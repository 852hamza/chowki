package pipeline_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/budget"
	"github.com/852hamza/chowki/internal/cache"
	"github.com/852hamza/chowki/internal/catalog"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/netguard"
	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/providers"
	"github.com/852hamza/chowki/internal/ratelimit"
	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/server"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

// Provider keys of the fakes. The fakes reject any other key, which proves
// that the gateway swaps the client's virtual key for the provider key.
const (
	openAIKey    = "sk-EXAMPLE-upstream-openai"
	anthropicKey = "sk-ant-EXAMPLE-upstream"
)

// testCatalog prices the fakes' models with round numbers.
const testCatalog = `{"models":[
 {"provider":"openai","model":"gpt-test","price":{"input":2,"output":10,"cache_read":0.2,"cache_write":2.5},
  "source":"https://example.com/pricing","updated":"2026-09-27"},
 {"provider":"anthropic","model":"claude-test","price":{"input":4,"output":20,"cache_read":0.2,"cache_write":5,
  "cache_write_1h":8},"source":"https://example.com/pricing","updated":"2026-09-27"}]}`

// recordingStore captures the request records that the gateway saves.
type recordingStore struct {
	*store.SQLite
	mu      sync.Mutex
	records []store.Request
}

func (r *recordingStore) InsertRequests(ctx context.Context, rs []store.Request) error {
	r.mu.Lock()
	r.records = append(r.records, rs...)
	r.mu.Unlock()
	return r.SQLite.InsertRequests(ctx, rs)
}

// syncBuffer is a bytes.Buffer that the logger and the test can share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// harness is a gateway with a real database in front of fake providers.
type harness struct {
	t         testing.TB
	gw        *pipeline.Gateway
	url       string
	key       string // a valid virtual key
	dbPath    string
	st        *recordingStore
	logs      *syncBuffer
	openai    *testutil.Server
	anthropic *testutil.Server
	closed    bool
}

type option func(*pipeline.Gateway, *[]config.Provider)

func newHarness(t testing.TB, oa, an testutil.Config, opts ...option) *harness {
	t.Helper()
	oa.APIKey, an.APIKey = openAIKey, anthropicKey
	h := &harness{t: t, openai: testutil.NewOpenAI(t, oa), anthropic: testutil.NewAnthropic(t, an), logs: &syncBuffer{}}

	h.dbPath = filepath.Join(t.TempDir(), "chowki.db")
	sq, err := store.OpenSQLite(t.Context(), "file:"+h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	h.st = &recordingStore{SQLite: sq}
	if h.key, _, err = auth.Create(t.Context(), sq, "default", store.Key{Name: "test"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load([]byte(testCatalog))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(h.logs, nil))
	budgets, err := budget.Load(t.Context(), sq, time.Now(), logger)
	if err != nil {
		t.Fatal(err)
	}
	h.gw = &pipeline.Gateway{Store: h.st, Cache: h.openCache(logger), Limits: ratelimit.New(), Budgets: budgets,
		Catalog: cat, Logger: logger, MaxBody: 1 << 20, Timeout: 10 * time.Second}
	cfgs := []config.Provider{
		{Name: "openai", Type: config.TypeOpenAI, BaseURL: h.openai.URL + "/v1", APIKeyEnv: "OPENAI_API_KEY", APIKey: openAIKey},
		{Name: "anthropic", Type: config.TypeAnthropic, BaseURL: h.anthropic.URL, APIKeyEnv: "ANTHROPIC_API_KEY",
			APIKey: anthropicKey},
	}
	for _, o := range opts {
		o(h.gw, &cfgs)
	}
	if h.gw.Providers, err = providers.New(cfgs, netguard.Policy{AllowPrivate: true}); err != nil {
		t.Fatal(err)
	}
	h.gw.Requests = store.NewRequestLog(h.st, logger)
	srv := httptest.NewServer(server.Routes(h.gw))
	h.url = srv.URL
	t.Cleanup(func() {
		srv.Close()
		h.flush()
		_ = sq.Close()
	})
	return h
}

// openCache returns an exact cache on the harness's database, off unless a
// key or a request turns it on.
func (h *harness) openCache(logger *slog.Logger) *cache.Cache {
	h.t.Helper()
	box, err := secretbox.New(bytes.Repeat([]byte{7}, secretbox.KeySize), "cache")
	if err != nil {
		h.t.Fatal(err)
	}
	c, err := cache.New(context.Background(), h.st, box, cache.Options{Default: cache.ModeOff, TTL: time.Hour,
		MaxBytes: 1 << 20}, logger)
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

// flush saves the queued request records and cache entries. The harness
// takes no more requests after it.
func (h *harness) flush() {
	if !h.closed {
		h.closed = true
		h.gw.Requests.Close()
		h.gw.Cache.Close()
	}
}

// restart replaces the gateway with a new one on the same database, as a
// restart of the process would.
func (h *harness) restart() {
	h.t.Helper()
	h.flush()
	gw := *h.gw
	gw.Limits = ratelimit.New()
	gw.Cache = h.openCache(gw.Logger)
	var err error
	if gw.Budgets, err = budget.Load(context.Background(), h.st, time.Now(), gw.Logger); err != nil {
		h.t.Fatal(err)
	}
	gw.Requests = store.NewRequestLog(h.st, gw.Logger)
	h.gw, h.closed = &gw, false
	srv := httptest.NewServer(server.Routes(h.gw))
	h.t.Cleanup(srv.Close)
	h.url = srv.URL
}

// records returns the saved request records.
func (h *harness) records() []store.Request {
	h.flush()
	h.st.mu.Lock()
	defer h.st.mu.Unlock()
	return append([]store.Request(nil), h.st.records...)
}

// post sends a request to the gateway with the valid virtual key in the
// header that the family's SDK uses.
func (h *harness) post(ctx context.Context, path, body string) *http.Response {
	h.t.Helper()
	header := map[string]string{"Authorization": "Bearer " + h.key}
	if strings.HasPrefix(path, "/anthropic/") {
		header = map[string]string{"x-api-key": h.key, "anthropic-version": "2023-06-01"}
	}
	return h.send(ctx, http.MethodPost, h.url+path, header, body)
}

func (h *harness) send(ctx context.Context, method, url string, header map[string]string, body string) *http.Response {
	h.t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func readBody(t testing.TB, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
