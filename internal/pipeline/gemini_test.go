package pipeline_test

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

const geminiBody = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`

// Gemini usage: the fake reports thoughts apart from the candidates.
var geminiUsage = testutil.Usage{Input: 1000, Output: 200, CacheRead: 400, Reasoning: 100}

func newGeminiHarness(t *testing.T, cfg testutil.Config, opts ...option) *harness {
	t.Helper()
	return newHarness(t, testutil.Config{}, testutil.Config{}, append([]option{withGemini(cfg)}, opts...)...)
}

func TestGeminiGenerateContent(t *testing.T) {
	// 600 uncached × 1 + 400 cached × 0.1 + (200 + 100 thoughts) × 8, per million.
	const cost = (600 + 40 + 2400) / 1e6
	tokens := store.Tokens{Input: 1000, Output: 300, CacheRead: 400, Reasoning: 100}
	for _, tt := range []struct{ name, path string }{
		{"json", "/gemini/v1beta/models/gemini-test:generateContent"},
		{"stream", "/gemini/v1beta/models/gemini-test:streamGenerateContent?alt=sse"},
		{"provider in the model", "/gemini/v1beta/models/gemini/gemini-test:generateContent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newGeminiHarness(t, testutil.Config{Usage: geminiUsage})
			resp := h.post(t.Context(), tt.path, geminiBody)
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"modelVersion":"gemini-test"`) {
				t.Fatalf("status %d\n%s", resp.StatusCode, body)
			}
			sent := h.gemini.Requests()[0]
			if !strings.HasPrefix(sent.Path, "/v1beta/models/gemini-test:") || sent.Query.Has("key") ||
				sent.Header.Get("x-goog-api-key") != geminiKey || string(sent.Body) != geminiBody {
				t.Errorf("the provider got %s %v %v\n%s", sent.Path, sent.Query, sent.Header, sent.Body)
			}
			recs := h.records()
			if len(recs) != 1 {
				t.Fatalf("saved %d records, want 1", len(recs))
			}
			r := recs[0]
			if r.Provider != "gemini" || r.Model != "gemini-test" || r.APIFamily != "gemini" || r.Tokens == nil ||
				*r.Tokens != tokens || r.CostUSD == nil || math.Abs(*r.CostUSD-cost) > 1e-12 ||
				math.Abs(r.SavingsUSD-400*0.9/1e6) > 1e-12 || r.SavingsMethod != "prompt_cache" {
				t.Errorf("record = %+v, tokens %+v, cost %v", r, r.Tokens, r.CostUSD)
			}
		})
	}
}

// Above 200k prompt tokens, the long-context price applies, cached tokens
// included.
func TestGeminiLongContext(t *testing.T) {
	h := newGeminiHarness(t, testutil.Config{Usage: testutil.Usage{Input: 250000, Output: 1000, CacheRead: 50000}})
	resp := h.post(t.Context(), "/gemini/v1beta/models/gemini-test:generateContent", geminiBody)
	readBody(t, resp)
	const cost = (200000*2 + 50000*0.2 + 1000*12) / 1e6
	if recs := h.records(); len(recs) != 1 || recs[0].CostUSD == nil || math.Abs(*recs[0].CostUSD-cost) > 1e-12 ||
		resp.Header.Get(pipeline.CostHeader) != "0.42200000" {
		t.Errorf("records = %+v, cost header %q; want %v", recs, resp.Header.Get(pipeline.CostHeader), cost)
	}
}

// geminiError decodes an error in the Google API error format.
func geminiError(t *testing.T, body string) (code int, status, reason string) {
	t.Helper()
	var e struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
			Details []struct {
				Type   string `json:"@type"`
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &e); err != nil || e.Error.Message == "" {
		t.Fatalf("body isn't a Google API error: %v\n%s", err, body)
	}
	if len(e.Error.Details) > 0 {
		reason = e.Error.Details[0].Reason
	}
	return e.Error.Code, e.Error.Status, reason
}

func TestGeminiErrors(t *testing.T) {
	h := newGeminiHarness(t, testutil.Config{})
	for _, tt := range []struct {
		name, path, key, body string
		status                int
		grpc, reason          string
	}{
		{"no key", "/gemini/v1beta/models/gemini-test:generateContent", "", geminiBody, 401, "UNAUTHENTICATED",
			"MISSING_API_KEY"},
		{"stream without alt=sse", "/gemini/v1beta/models/gemini-test:streamGenerateContent", h.key, geminiBody, 400,
			"INVALID_ARGUMENT", "INVALID_REQUEST"},
		{"unknown method", "/gemini/v1beta/models/gemini-test:predict", h.key, geminiBody, 404, "NOT_FOUND", "NOT_FOUND"},
		{"unknown path", "/gemini/v1/files", h.key, "", 404, "NOT_FOUND", "NOT_FOUND"},
		{"not JSON", "/gemini/v1beta/models/gemini-test:generateContent", h.key, "{", 400, "INVALID_ARGUMENT",
			"INVALID_REQUEST"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			header := map[string]string{}
			if tt.key != "" {
				header["x-goog-api-key"] = tt.key
			}
			resp := h.send(t.Context(), http.MethodPost, h.url+tt.path, header, tt.body)
			code, grpc, reason := geminiError(t, readBody(t, resp))
			if resp.StatusCode != tt.status || code != tt.status || grpc != tt.grpc || reason != tt.reason {
				t.Errorf("got %d (%d %s %s), want %d %s %s", resp.StatusCode, code, grpc, reason, tt.status, tt.grpc,
					tt.reason)
			}
		})
	}

	// The provider's own errors reach the client as they are.
	h = newGeminiHarness(t, testutil.Config{FailStatus: http.StatusServiceUnavailable})
	resp := h.post(t.Context(), "/gemini/v1beta/models/gemini-test:generateContent", geminiBody)
	if code, grpc, _ := geminiError(t, readBody(t, resp)); resp.StatusCode != http.StatusServiceUnavailable ||
		code != http.StatusServiceUnavailable || grpc != "UNAVAILABLE" {
		t.Errorf("provider failure = %d %d %s", resp.StatusCode, code, grpc)
	}
}

func TestGeminiCountTokensAndEmbeddings(t *testing.T) {
	h := newGeminiHarness(t, testutil.Config{Usage: testutil.Usage{Input: 5000}},
		func(h *harness, _ *[]config.Provider) {
			h.aliases = map[string][]string{"embed": {"gemini/gemini-embed-test"}}
		})
	resp := h.post(t.Context(), "/gemini/v1beta/models/gemini-test:countTokens", geminiBody)
	if got := strings.TrimSpace(readBody(t, resp)); resp.StatusCode != http.StatusOK || got != `{"totalTokens":5000}` {
		t.Fatalf("countTokens = %d %s", resp.StatusCode, got)
	}
	resp = h.post(t.Context(), "/gemini/v1beta/models/gemini-embed-test:embedContent",
		`{"content":{"parts":[{"text":"mail jane.doe@company.io"}]}}`)
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK || resp.Header.Get("x-chowki-redactions") != "1" {
		t.Fatalf("embedContent = %d %v\n%s", resp.StatusCode, resp.Header, body)
	}
	// Through an alias, each request of a batch names the provider's model,
	// as Gemini wants it to.
	resp = h.post(t.Context(), "/gemini/v1beta/models/embed:batchEmbedContents",
		`{"requests":[{"model":"models/embed","content":{"parts":[{"text":"a"}]}},`+
			`{"model":"models/embed","content":{"parts":[{"text":"b"}]}}]}`)
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("batchEmbedContents = %d\n%s", resp.StatusCode, body)
	}
	sent := h.gemini.Requests()
	if len(sent) != 3 || strings.Contains(string(sent[1].Body), "jane.doe") ||
		sent[2].Path != "/v1beta/models/gemini-embed-test:batchEmbedContents" ||
		strings.Count(string(sent[2].Body), `"model":"models/gemini-embed-test"`) != 2 {
		t.Errorf("the provider got %+v", sent)
	}
	recs := h.records()
	// countTokens is free; embeddings cost 5000 tokens × $0.20 per million.
	if len(recs) != 3 || recs[0].CostUSD == nil || *recs[0].CostUSD != 0 || recs[1].CostUSD == nil ||
		math.Abs(*recs[1].CostUSD-0.001) > 1e-12 || recs[2].Model != "gemini-embed-test" ||
		recs[2].Endpoint != "/gemini/v1beta/models/{model}:batchEmbedContents" {
		t.Errorf("records = %+v", recs)
	}
}

func TestGeminiRedactionAndCache(t *testing.T) {
	h := newGeminiHarness(t, testutil.Config{Usage: geminiUsage})
	setKeyCache(t, h, "exact")
	body := `{"systemInstruction":{"parts":[{"text":"Be brief."}]},` +
		`"contents":[{"role":"user","parts":[{"text":"Call +14155552671"}]}]}`
	first := h.post(t.Context(), "/gemini/v1beta/models/gemini-test:generateContent", body)
	got := readBody(t, first)
	if first.StatusCode != http.StatusOK || first.Header.Get("x-chowki-cache") != "miss" ||
		first.Header.Get("x-chowki-redactions") != "1" {
		t.Fatalf("status %d, headers %v", first.StatusCode, first.Header)
	}
	if sent := string(h.gemini.Requests()[0].Body); strings.Contains(sent, "4155552671") ||
		!strings.Contains(sent, `"Be brief."`) {
		t.Errorf("the provider got %s", sent)
	}
	waitCached(t, h)
	second := h.post(t.Context(), "/gemini/v1beta/models/gemini-test:generateContent", body)
	if second.Header.Get("x-chowki-cache") != "hit" || readBody(t, second) != got || len(h.gemini.Requests()) != 1 {
		t.Errorf("a repeated request wasn't answered from the cache")
	}
}

// A Gemini alias falls back to its next target, with the model in the path.
func TestGeminiFallback(t *testing.T) {
	backup := testutil.NewGemini(t, testutil.Config{APIKey: geminiKey, Usage: geminiUsage})
	h := newGeminiHarness(t, testutil.Config{FailStatus: http.StatusTooManyRequests},
		func(h *harness, cfgs *[]config.Provider) {
			*cfgs = append(*cfgs, config.Provider{Name: "backup", Type: config.TypeGemini, BaseURL: backup.URL,
				APIKeyEnv: "BACKUP_KEY", APIKey: geminiKey})
			h.aliases = map[string][]string{"smart": {"gemini/gemini-test", "backup/gemini-backup"}}
		})
	resp := h.post(t.Context(), "/gemini/v1beta/models/smart:streamGenerateContent?alt=sse", geminiBody)
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK || !strings.Contains(body, "data: ") {
		t.Fatalf("status %d\n%s", resp.StatusCode, body)
	}
	if sent := backup.Requests(); len(sent) != 1 || sent[0].Path != "/v1beta/models/gemini-backup:streamGenerateContent" ||
		sent[0].Query.Get("alt") != "sse" {
		t.Errorf("the backup got %+v", sent)
	}
	if recs := h.records(); len(recs) != 1 || recs[0].Provider != "backup" || recs[0].Model != "gemini-backup" {
		t.Errorf("records = %+v", recs)
	}
}
