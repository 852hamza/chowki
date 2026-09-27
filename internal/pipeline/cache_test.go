package pipeline_test

import (
	"bytes"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

func setKeyCache(t *testing.T, h *harness, mode string) {
	t.Helper()
	if _, err := h.st.UpdateKey(t.Context(), h.key[:auth.PrefixLen], store.KeyUpdate{CacheMode: &mode}); err != nil {
		t.Fatal(err)
	}
}

// waitCached waits until the cache holds entries: it stores them in the
// background.
func waitCached(t *testing.T, h *harness) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if size, err := h.st.CacheSize(t.Context()); err == nil && size > 0 {
			return
		}
	}
	t.Fatal("the response wasn't cached")
}

func cacheStatus(resp *http.Response) string { return resp.Header.Get(pipeline.CacheHeader) }

// AC: cache hit and miss headers are correct.
func TestCacheHitAndMiss(t *testing.T) {
	tests := []struct {
		name, path, first, second string
		cost                      float64 // what the first request costs
	}{
		{"openai", "/v1/chat/completions", openAIBody,
			"{ \"messages\": [{\"content\": \"hi\", \"role\": \"user\"}],\n \"model\": \"gpt-test\", \"user\": \"u2\" }", 3},
		{"anthropic", "/anthropic/v1/messages", anthropicBody,
			`{"messages":[{"role":"user","content":"hi"}],"max_tokens":64,"model":"claude-test","metadata":{"user_id":"u2"}}`, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testutil.Config{Usage: bigUsage}
			h := newHarness(t, cfg, cfg)
			setKeyCache(t, h, "exact")
			first := h.post(t.Context(), tt.path, tt.first)
			firstBody := readBody(t, first)
			if first.StatusCode != http.StatusOK || cacheStatus(first) != "miss" {
				t.Fatalf("first request: status %d, cache %q; want 200 and miss", first.StatusCode, cacheStatus(first))
			}
			waitCached(t, h)

			// The same request, formatted differently and from another user.
			second := h.post(t.Context(), tt.path, tt.second)
			if body := readBody(t, second); second.StatusCode != http.StatusOK || cacheStatus(second) != "hit" ||
				body != firstBody || second.Header.Get(pipeline.CostHeader) != "0.00000000" ||
				second.Header.Get("Content-Type") != first.Header.Get("Content-Type") {
				t.Errorf("second request: status %d, cache %q, cost %q; want 200, hit, 0 and the first body\n%s",
					second.StatusCode, cacheStatus(second), second.Header.Get(pipeline.CostHeader), body)
			}
			if n := len(h.openai.Requests()) + len(h.anthropic.Requests()); n != 1 {
				t.Errorf("the providers got %d requests, want 1", n)
			}
			recs := h.records()
			if len(recs) != 2 {
				t.Fatalf("saved %d records, want 2", len(recs))
			}
			if r := recs[0]; r.CacheStatus != "miss" || r.CostUSD == nil || *r.CostUSD != tt.cost {
				t.Errorf("first record = %+v; want a miss that cost $%v", r, tt.cost)
			}
			if r := recs[1]; r.CacheStatus != "hit" || r.CostUSD == nil || *r.CostUSD != 0 ||
				math.Abs(r.SavingsUSD-tt.cost) > 1e-9 || r.SavingsMethod != "exact_cache" || r.Tokens != nil {
				t.Errorf("second record = %+v; want a free hit that saved $%v", r, tt.cost)
			}
		})
	}
}

func TestCacheBypass(t *testing.T) {
	tests := []struct {
		name, keyMode, header, body, want string
	}{
		{"off by default", "", "", openAIBody, "bypass"},
		{"on for the request", "", "on", openAIBody, "miss"},
		{"off for the request", "exact", "off", openAIBody, "bypass"},
		{"streams", "exact", "", `{"model":"gpt-test","messages":[{}],"stream":true}`, "bypass"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, testutil.Config{}, testutil.Config{})
			setKeyCache(t, h, tt.keyMode)
			header := map[string]string{"Authorization": "Bearer " + h.key, "x-chowki-cache": tt.header}
			resp := h.send(t.Context(), http.MethodPost, h.url+"/v1/chat/completions", header, tt.body)
			if readBody(t, resp); resp.StatusCode != http.StatusOK || cacheStatus(resp) != tt.want {
				t.Errorf("status %d, cache %q; want 200 and %s", resp.StatusCode, cacheStatus(resp), tt.want)
			}
		})
	}

	h := newHarness(t, testutil.Config{}, testutil.Config{})
	header := map[string]string{"Authorization": "Bearer " + h.key, "x-chowki-cache": "maybe"}
	if resp := h.send(t.Context(), http.MethodPost, h.url+"/v1/chat/completions", header, openAIBody); resp.StatusCode !=
		http.StatusBadRequest {
		t.Errorf("x-chowki-cache: maybe got status %d, want 400", resp.StatusCode)
	}
}

// Projects never share answers.
func TestCacheIsPerProject(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{})
	setKeyCache(t, h, "exact")
	readBody(t, h.post(t.Context(), "/v1/chat/completions", openAIBody))
	waitCached(t, h)
	other, _, err := auth.Create(t.Context(), h.st, "other", store.Key{Name: "b", CacheMode: "exact"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	resp := h.send(t.Context(), http.MethodPost, h.url+"/v1/chat/completions",
		map[string]string{"Authorization": "Bearer " + other}, openAIBody)
	if readBody(t, resp); cacheStatus(resp) != "miss" {
		t.Errorf("another project's request: cache %q, want miss", cacheStatus(resp))
	}
}

// A hit costs nothing, so a key whose budget is used up still gets it.
func TestCacheHitsSkipTheBudget(t *testing.T) {
	h := newHarness(t, testutil.Config{Usage: bigUsage}, testutil.Config{})
	setKeyCache(t, h, "exact")
	setKeyBudget(t, h, 2.5)
	readBody(t, h.post(t.Context(), "/v1/chat/completions", openAIBody)) // costs $3
	waitCached(t, h)
	if resp := h.post(t.Context(), "/v1/chat/completions", openAIBody); resp.StatusCode != http.StatusOK ||
		cacheStatus(resp) != "hit" {
		t.Errorf("repeated request: status %d, cache %q; want a hit", resp.StatusCode, cacheStatus(resp))
	}
	other := `{"model":"gpt-test","messages":[{"role":"user","content":"something new"}]}`
	if resp := h.post(t.Context(), "/v1/chat/completions", other); resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("new request: status %d, want 429 budget_exceeded", resp.StatusCode)
	}
}

// AC: cache entries are encrypted at rest.
func TestCacheIsEncryptedAtRest(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{})
	setKeyCache(t, h, "exact")
	body := readBody(t, h.post(t.Context(), "/v1/chat/completions", openAIBody))
	if !bytes.Contains([]byte(body), []byte(testutil.DefaultText)) {
		t.Fatalf("the reply doesn't contain %q:\n%s", testutil.DefaultText, body)
	}
	waitCached(t, h)
	h.flush()
	files, err := filepath.Glob(h.dbPath + "*") // the database and its write-ahead log
	if err != nil || len(files) == 0 {
		t.Fatalf("database files = %v, %v", files, err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(testutil.DefaultText)) {
			t.Errorf("%s holds the reply in plain text", filepath.Base(f))
		}
	}
}
