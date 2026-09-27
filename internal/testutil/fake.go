package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// DefaultText is the reply of a fake provider whose Config.Text is empty.
const DefaultText = "Hello from the fake provider."

// fakeCreated is the fixed Unix time in replies, so streams are reproducible.
const fakeCreated = 1767225600 // 2026-01-01T00:00:00Z

// Usage is the token usage that a fake provider reports. Each field is written
// unchanged to one field of the provider's format, so a test decides exactly
// what the gateway has to parse. The formats count differently: OpenAI and
// Gemini include cached tokens in the prompt count, while Anthropic's
// input_tokens exclude tokens read from or written to the cache.
type Usage struct {
	// Input is OpenAI prompt_tokens, Anthropic input_tokens and Gemini
	// promptTokenCount.
	Input int
	// Output is OpenAI completion_tokens, Anthropic output_tokens and Gemini
	// candidatesTokenCount.
	Output int
	// CacheRead is OpenAI prompt_tokens_details.cached_tokens, Anthropic
	// cache_read_input_tokens and Gemini cachedContentTokenCount.
	CacheRead int
	// CacheWrite is OpenAI prompt_tokens_details.cache_write_tokens and
	// Anthropic cache_creation_input_tokens. Gemini has no such field, so
	// NewGemini rejects a non-zero value.
	CacheWrite int
	// Reasoning is OpenAI completion_tokens_details.reasoning_tokens,
	// Anthropic output_tokens_details.thinking_tokens and Gemini
	// thoughtsTokenCount.
	Reasoning int
}

// Config configures a fake provider. The zero value accepts any key and
// replies with DefaultText and zero usage.
type Config struct {
	// APIKey, when set, is the only key the fake accepts. A request with
	// another key or none gets a 401 error.
	APIKey string
	// Text is the reply. Empty means DefaultText.
	Text string
	// Chunks is the number of pieces a streamed reply is split into. Zero
	// means 3; the count never exceeds the number of characters in Text.
	Chunks int
	// ChunkDelay is the pause between streamed pieces. Tests use a long delay
	// to check that the gateway relays each chunk without waiting for the
	// rest of the stream.
	ChunkDelay time.Duration
	// Usage is reported with every successful reply.
	Usage Usage
	// FailStatus, when set, makes every authenticated request fail with this
	// HTTP status (400-599) and an error body in the provider's format.
	FailStatus int
	// Delay is a pause before the fake answers at all, headers included.
	// Tests use it to make a provider slower than the gateway's timeout.
	Delay time.Duration
	// ToolCall, when set, makes the reply a call of a tool instead of text.
	// Streams split its arguments into Chunks pieces where the format
	// streams them, as OpenAI's and Anthropic's do; Gemini's sends a call
	// whole, with a thought signature.
	ToolCall *ToolCall
}

// ToolCall is a call of a tool that a fake provider makes.
type ToolCall struct {
	Name string
	// Arguments is a JSON object.
	Arguments string
}

// FakeSignature is the thought signature of the fake Gemini's calls.
const FakeSignature = "ZmFrZS1zaWduYXR1cmU="

func (c Config) text() string {
	if c.Text == "" {
		return DefaultText
	}
	return c.Text
}

// pieces splits the reply into the configured number of streamed pieces.
func (c Config) pieces() []string { return c.split(c.text()) }

// argumentPieces splits a tool call's arguments like the reply.
func (c Config) argumentPieces() []string { return c.split(c.ToolCall.Arguments) }

func (c Config) split(s string) []string {
	runes := []rune(s)
	n := c.Chunks
	if n == 0 {
		n = 3
	}
	n = min(n, len(runes))
	pieces := make([]string, n)
	for i := range n {
		pieces[i] = string(runes[i*len(runes)/n : (i+1)*len(runes)/n])
	}
	return pieces
}

func (c Config) validate() error {
	u := c.Usage
	switch {
	case u.Input < 0 || u.Output < 0 || u.CacheRead < 0 || u.CacheWrite < 0 || u.Reasoning < 0:
		return errors.New("usage numbers must not be negative")
	case c.Chunks < 0:
		return errors.New("chunks must not be negative")
	case c.FailStatus != 0 && (c.FailStatus < 400 || c.FailStatus > 599):
		return fmt.Errorf("fail status %d is not an HTTP error status", c.FailStatus)
	case c.ToolCall != nil && !json.Valid([]byte(c.ToolCall.Arguments)):
		return errors.New("tool call arguments must be JSON")
	}
	return nil
}

// Request is a request that a fake provider received.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// Server is a running fake provider. It stops when the test ends.
type Server struct {
	// URL is the base URL of the server, such as http://127.0.0.1:41234,
	// without a path.
	URL string

	cfg Config

	mu       sync.Mutex
	requests []Request
	nextID   int
}

// Requests returns the requests the server received, oldest first.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// start runs a fake provider whose routes register adds to mux.
func start(t testing.TB, cfg Config, register func(s *Server, mux *http.ServeMux)) *Server {
	t.Helper()
	if err := cfg.validate(); err != nil {
		t.Fatalf("testutil: invalid fake provider config: %v", err)
	}
	s := &Server{cfg: cfg}
	mux := http.NewServeMux()
	register(s, mux)
	srv := httptest.NewServer(s.record(mux))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// record saves a copy of every request before next handles it.
func (s *Server) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		s.mu.Lock()
		s.requests = append(s.requests, Request{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Header: r.Header.Clone(),
			Body:   body,
		})
		s.mu.Unlock()
		select {
		case <-time.After(s.cfg.Delay):
		case <-r.Context().Done():
			return
		}
		next.ServeHTTP(w, r)
	})
}

// id returns a new identifier with the given prefix.
func (s *Server) id(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return fmt.Sprintf("%s%d", prefix, s.nextID)
}

func (s *Server) authorized(key string) bool {
	return s.cfg.APIKey == "" || key == s.cfg.APIKey
}

// bearer returns the token from an "Authorization: Bearer" header.
func bearer(r *http.Request) string {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return token
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // a client that went away is the test's concern
}

// sseStream writes server-sent events, flushing each one.
type sseStream struct {
	w     http.ResponseWriter
	rc    *http.ResponseController
	ctx   context.Context
	delay time.Duration
}

func newSSE(w http.ResponseWriter, r *http.Request, delay time.Duration) *sseStream {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	return &sseStream{w: w, rc: http.NewResponseController(w), ctx: r.Context(), delay: delay}
}

// send writes an event with an optional name and JSON data. It reports
// whether the client is still there.
func (s *sseStream) send(event string, data any) bool {
	b, err := json.Marshal(data)
	if err != nil {
		return false
	}
	return s.write(event, string(b))
}

func (s *sseStream) write(event, data string) bool {
	var b strings.Builder
	if event != "" {
		b.WriteString("event: " + event + "\n")
	}
	b.WriteString("data: " + data + "\n\n")
	if _, err := io.WriteString(s.w, b.String()); err != nil {
		return false
	}
	return s.rc.Flush() == nil
}

// pause waits for the chunk delay. It reports whether the client is still
// there.
func (s *sseStream) pause() bool {
	if s.delay <= 0 {
		return s.ctx.Err() == nil
	}
	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.ctx.Done():
		return false
	}
}
