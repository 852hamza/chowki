package pipeline

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/catalog"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/providers"
	"github.com/852hamza/chowki/internal/sse"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/usage"
)

// Response headers that the gateway adds.
const (
	RequestIDHeader = "x-chowki-request-id"
	// CostHeader carries the cost of a priced, non-streaming request in USD.
	CostHeader = "x-chowki-cost-usd"
)

// maxResponse limits a non-streaming response that the gateway reads.
const maxResponse = 64 << 20

// Gateway serves the API endpoints: it authenticates each request with a
// virtual key, routes it to a provider, relays the response and records
// the request's metadata, usage and cost.
type Gateway struct {
	Store     store.Store
	Requests  *store.RequestLog
	Providers map[string]*providers.Provider
	Catalog   *catalog.Catalog
	Logger    *slog.Logger
	// MaxBody is the largest request body, in bytes.
	MaxBody int64
	// Timeout limits each upstream call, streams included.
	Timeout time.Duration
}

// Handler returns the handler of the chat endpoint of API family f:
// OpenAI chat completions or Anthropic messages.
func (g *Gateway) Handler(f usage.Family) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { g.serve(w, r, f) })
}

// NotFound returns a handler that answers unknown paths in the error
// format of family f.
func (g *Gateway) NotFound(f usage.Family) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set(RequestIDHeader, id)
		writeError(w, f, id, apiError{http.StatusNotFound, codeNotFound,
			fmt.Sprintf("%s %s isn't an endpoint of this gateway.", r.Method, r.URL.Path)})
	})
}

// call is one request on its way through the gateway.
type call struct {
	family   usage.Family
	id       string
	start    time.Time
	w        *trackingWriter
	key      store.Key
	provider *providers.Provider
	model    string // the model sent upstream
	stream   bool
	errType  string
	report   usage.Report
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request, f usage.Family) {
	c := &call{family: f, id: newRequestID(), start: time.Now(), w: &trackingWriter{ResponseWriter: w}}
	c.w.Header().Set(RequestIDHeader, c.id)
	if e := g.handle(r.Context(), c, r); e != nil {
		c.errType = e.Code
		writeError(c.w, f, c.id, *e)
	}
	g.finish(c)
}

// handle runs the stages. It returns an error to send to the client, or
// nil once the response has started or the client has gone.
func (g *Gateway) handle(ctx context.Context, c *call, r *http.Request) *apiError {
	// Authenticate before reading the body, so that clients without a key
	// can't make the gateway read large bodies.
	if e := g.authenticate(ctx, c, r); e != nil {
		return e
	}

	body, err := io.ReadAll(http.MaxBytesReader(c.w, r.Body, g.MaxBody))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &apiError{http.StatusRequestEntityTooLarge, codeTooLarge,
			fmt.Sprintf("The request body is larger than the gateway's limit of %d MiB.", g.MaxBody>>20)}
	}
	if err != nil {
		return &apiError{http.StatusBadRequest, codeInvalidRequest, "The request body couldn't be read."}
	}
	obj, err := parseObject(body)
	if err != nil {
		return &apiError{http.StatusBadRequest, codeInvalidRequest, "Invalid request body: " + err.Error() + "."}
	}
	req, e := readRequest(c.family, obj)
	if e != nil {
		return e
	}

	p, model, e := g.route(c.family, req.model)
	if e != nil {
		return e
	}
	c.provider, c.model, c.stream = p, model, req.stream
	if env := p.MissingKey(); env != "" {
		g.Logger.Error("provider key isn't set", "request_id", c.id, "provider", p.Name, "variable", env)
		return &apiError{http.StatusInternalServerError, codeProviderKeyMissing,
			fmt.Sprintf("The provider %q has no key; the gateway's admin must set %s.", p.Name, env)}
	}

	// Change the body only where needed; everything else reaches the
	// provider byte for byte.
	edits := map[string][]byte{}
	if model != req.model {
		edits["model"], _ = json.Marshal(model) // a string always marshals
	}
	// OpenAI streams report usage only on request. The extra chunk that
	// carries it is hidden from clients that didn't ask for it.
	dropUsage := c.family == usage.OpenAI && req.stream && !req.includeUsage
	if dropUsage {
		req.streamOptions["include_usage"] = json.RawMessage("true")
		edits["stream_options"], _ = json.Marshal(req.streamOptions) // raw JSON values always marshal
	}
	if len(edits) > 0 {
		body = obj.with(edits)
	}

	upstreamCtx, cancel := context.WithTimeout(ctx, g.Timeout)
	defer cancel()
	endpoint := providers.ChatCompletions
	if c.family == usage.Anthropic {
		endpoint = providers.Messages
	}
	resp, err := p.Do(upstreamCtx, endpoint, body, r.Header)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			c.errType = "client_closed"
			return nil
		case errors.Is(err, context.DeadlineExceeded):
			return &apiError{http.StatusGatewayTimeout, codeUpstreamTimeout,
				fmt.Sprintf("The provider %q didn't answer within %s.", p.Name, g.Timeout)}
		}
		g.Logger.Warn("upstream call failed", "request_id", c.id, "provider", p.Name, "error", err)
		return &apiError{http.StatusBadGateway, codeUpstreamFailed, fmt.Sprintf("The provider %q couldn't be reached.", p.Name)}
	}
	defer func() { _ = resp.Body.Close() }()

	if c.stream && resp.StatusCode == http.StatusOK && isEventStream(resp.Header) {
		g.relayStream(upstreamCtx, c, resp, dropUsage)
		return nil
	}
	return g.relayBody(c, resp)
}

func (g *Gateway) authenticate(ctx context.Context, c *call, r *http.Request) *apiError {
	key := clientKey(r)
	if key == "" {
		return &apiError{http.StatusUnauthorized, codeMissingKey,
			"Send your Chowki virtual key in the Authorization: Bearer, x-api-key or x-goog-api-key header."}
	}
	k, err := auth.Verify(ctx, g.Store, key)
	switch {
	case errors.Is(err, auth.ErrInvalid):
		return &apiError{http.StatusUnauthorized, codeInvalidKey, "The virtual key is invalid."}
	case errors.Is(err, auth.ErrRevoked):
		return &apiError{http.StatusUnauthorized, codeRevokedKey, "The virtual key has been revoked."}
	case err != nil:
		g.Logger.Error("verify key", "request_id", c.id, "error", err)
		return &apiError{http.StatusInternalServerError, codeInternal, "The gateway couldn't check the key; try again."}
	}
	c.key = k
	return nil
}

// clientKey returns the virtual key from the header that the client's SDK
// uses. It never reads a key from the URL, where it would leak into logs.
func clientKey(r *http.Request) string {
	if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("x-api-key"); v != "" {
		return v
	}
	return r.Header.Get("x-goog-api-key")
}

// request holds the fields the gateway reads from a request body.
type request struct {
	model         string
	stream        bool
	includeUsage  bool
	streamOptions map[string]json.RawMessage
}

func readRequest(f usage.Family, o *object) (request, *apiError) {
	bad := func(msg string) (request, *apiError) {
		return request{}, &apiError{http.StatusBadRequest, codeInvalidRequest, msg}
	}
	var req request
	raw, ok := o.raw("model")
	if !ok || json.Unmarshal(raw, &req.model) != nil || req.model == "" {
		return bad(`The "model" field must be a model name, such as <provider>/<model>.`)
	}
	if raw, ok := o.raw("stream"); ok && json.Unmarshal(raw, &req.stream) != nil {
		return bad(`The "stream" field must be true or false.`)
	}
	if f == usage.OpenAI {
		req.streamOptions = map[string]json.RawMessage{}
		if raw, ok := o.raw("stream_options"); ok && string(raw) != "null" {
			if json.Unmarshal(raw, &req.streamOptions) != nil {
				return bad(`The "stream_options" field must be an object.`)
			}
			if v, ok := req.streamOptions["include_usage"]; ok && json.Unmarshal(v, &req.includeUsage) != nil {
				return bad(`"stream_options.include_usage" must be true or false.`)
			}
		}
	}
	return req, nil
}

// route picks the provider for a model: "<provider>/<model>" names it;
// otherwise the provider that the catalog lists for the model, or the only
// provider of the API family.
func (g *Gateway) route(f usage.Family, requested string) (*providers.Provider, string, *apiError) {
	wantType := config.TypeOpenAI
	if f == usage.Anthropic {
		wantType = config.TypeAnthropic
	}
	check := func(p *providers.Provider, model string) (*providers.Provider, string, *apiError) {
		if p.Type != wantType {
			return nil, "", &apiError{http.StatusBadRequest, codeWrongEndpoint, fmt.Sprintf(
				"The provider %q speaks the %s API; send requests for it to that API's endpoint.", p.Name, p.Type)}
		}
		return p, model, nil
	}
	if name, model, ok := strings.Cut(requested, "/"); ok && model != "" {
		if p, ok := g.Providers[name]; ok {
			return check(p, model)
		}
	}
	var listed []*providers.Provider
	for _, name := range g.Catalog.Providers(requested) {
		if p, ok := g.Providers[name]; ok && p.Type == wantType {
			listed = append(listed, p)
		}
	}
	if len(listed) == 1 {
		return listed[0], requested, nil
	}
	var family []string
	for name, p := range g.Providers {
		if p.Type == wantType {
			family = append(family, name)
		}
	}
	if len(family) == 1 {
		return g.Providers[family[0]], requested, nil
	}
	slices.Sort(family)
	hint := "Add a provider of this API to chowki.yaml."
	if len(family) > 1 {
		hint = fmt.Sprintf("Name it as <provider>/<model>, with a provider from: %s.", strings.Join(family, ", "))
	}
	return nil, "", &apiError{http.StatusBadRequest, codeUnknownProvider,
		fmt.Sprintf("The gateway can't tell which provider serves the model %q. %s", requested, hint)}
}

// relayBody forwards a complete response: a non-streaming one, or an error.
func (g *Gateway) relayBody(c *call, resp *http.Response) *apiError {
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err == nil && len(data) > maxResponse {
		err = errors.New("response too large")
	}
	if err != nil {
		g.Logger.Warn("read upstream response", "request_id", c.id, "provider", c.provider.Name, "error", err)
		if errors.Is(err, context.DeadlineExceeded) {
			return &apiError{http.StatusGatewayTimeout, codeUpstreamTimeout,
				fmt.Sprintf("The provider %q didn't answer within %s.", c.provider.Name, g.Timeout)}
		}
		return &apiError{http.StatusBadGateway, codeUpstreamFailed,
			fmt.Sprintf("The response of the provider %q couldn't be read.", c.provider.Name)}
	}
	copyHeaders(c.w.Header(), resp.Header)
	if resp.StatusCode == http.StatusOK {
		if rep, err := usage.ParseResponse(c.family, data); err == nil {
			c.report = rep
		}
		if cost := g.cost(c); cost.USD != nil {
			c.w.Header().Set(CostHeader, fmt.Sprintf("%.8f", *cost.USD))
		}
	} else {
		c.errType = fmt.Sprintf("upstream_%d", resp.StatusCode)
	}
	c.w.WriteHeader(resp.StatusCode)
	if _, err := c.w.Write(data); err != nil {
		c.errType = "client_closed"
	}
	return nil
}

// relayStream forwards a streamed response event by event.
func (g *Gateway) relayStream(ctx context.Context, c *call, resp *http.Response, dropUsage bool) {
	copyHeaders(c.w.Header(), resp.Header)
	c.w.Header().Set("Cache-Control", "no-cache")
	c.w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(c.w)
	stream := usage.NewStream(c.family)
	err := rc.Flush()
	if err == nil {
		err = sse.Relay(c.w, rc.Flush, resp.Body, func(ev sse.Event) bool {
			stream.Event(ev.Name, ev.Data)
			return !dropUsage || !isUsageChunk(ev.Data)
		})
	}
	c.report = stream.Report()
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			c.errType = codeUpstreamTimeout
		case ctx.Err() != nil:
			c.errType = "client_closed"
		default:
			c.errType = "upstream_stream_error"
		}
		g.Logger.Warn("stream ended early", "request_id", c.id, "provider", c.provider.Name, "error", err)
	}
}

// isUsageChunk reports whether data is the extra OpenAI chunk that carries
// usage and no choices.
func isUsageChunk(data []byte) bool {
	if !bytes.Contains(data, []byte(`"usage"`)) {
		return false
	}
	var chunk struct {
		Choices []json.RawMessage `json:"choices"`
		Usage   json.RawMessage   `json:"usage"`
	}
	return json.Unmarshal(data, &chunk) == nil && len(chunk.Choices) == 0 && len(chunk.Usage) > 0 &&
		string(chunk.Usage) != "null"
}

func isEventStream(h http.Header) bool {
	mt, _, err := mime.ParseMediaType(h.Get("Content-Type"))
	return err == nil && mt == "text/event-stream"
}

// copied are the provider response headers that reach the client: the
// content type, when to retry, and the provider's request ID for support.
var copied = []string{"Content-Type", "Retry-After", "Request-Id", "X-Request-Id"}

func copyHeaders(dst, src http.Header) {
	for _, h := range copied {
		if v := src.Values(h); len(v) > 0 {
			dst[h] = v
		}
	}
}

func (g *Gateway) cost(c *call) usage.Cost {
	var m *catalog.Model
	if c.provider != nil {
		var ok bool
		if m, ok = g.Catalog.Find(c.provider.Name, c.model); !ok {
			m, _ = g.Catalog.Find(c.provider.Name, c.report.Model)
		}
	}
	return usage.Compute(c.family, c.report, m)
}

// finish records the request's metadata: never its content or keys.
func (g *Gateway) finish(c *call) {
	latency := time.Since(c.start)
	status := c.w.status
	if status == 0 {
		status = 499 // the client left before the response
	}
	var ttfb time.Duration
	if !c.w.first.IsZero() {
		ttfb = c.w.first.Sub(c.start)
	}
	attrs := []any{"request_id", c.id, "family", c.family, "status", status, "latency_ms", latency.Milliseconds()}
	if c.errType != "" {
		attrs = append(attrs, "error", c.errType)
	}
	if c.key.ID == 0 {
		g.Logger.Info("request rejected", attrs...)
		return
	}
	rec := store.Request{
		ID: c.id, Time: c.start, KeyID: c.key.ID, ProjectID: c.key.ProjectID, APIFamily: string(c.family),
		Endpoint: endpointPath(c.family), Model: c.model, Stream: c.stream, Status: status, ErrorType: c.errType,
		Latency: latency, TTFB: ttfb,
	}
	if c.provider != nil {
		rec.Provider = c.provider.Name
	}
	if u := c.report.Usage; u != nil {
		rec.Tokens = &store.Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead,
			CacheWrite: u.CacheWrite, Reasoning: u.Reasoning}
	}
	cost := g.cost(c)
	rec.CostUSD, rec.SavingsUSD, rec.SavingsMethod = cost.USD, cost.SavingsUSD, cost.SavingsMethod
	g.Requests.Add(rec)

	attrs = append(attrs, "key_id", c.key.ID, "provider", rec.Provider, "model", rec.Model, "stream", c.stream,
		"ttfb_ms", ttfb.Milliseconds())
	if t := rec.Tokens; t != nil {
		attrs = append(attrs, "input_tokens", t.Input, "output_tokens", t.Output, "cache_read_tokens", t.CacheRead,
			"cache_write_tokens", t.CacheWrite)
	}
	if cost.USD != nil {
		attrs = append(attrs, "cost_usd", *cost.USD)
	} else if c.errType == "" {
		attrs = append(attrs, "unpriced", cost.Reason)
	}
	g.Logger.Info("request", attrs...)
}

func endpointPath(f usage.Family) string {
	if f == usage.Anthropic {
		return "/anthropic/v1/messages"
	}
	return "/v1/chat/completions"
}

func newRequestID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return "req_" + hex.EncodeToString(b)
}

// trackingWriter records the status and when the first byte went out.
type trackingWriter struct {
	http.ResponseWriter
	status int
	first  time.Time
}

func (t *trackingWriter) WriteHeader(code int) {
	if t.status == 0 {
		t.status = code
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackingWriter) Write(b []byte) (int, error) {
	if t.status == 0 {
		t.status = http.StatusOK
	}
	if t.first.IsZero() {
		t.first = time.Now()
	}
	return t.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (t *trackingWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }
