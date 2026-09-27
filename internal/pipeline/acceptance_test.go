package pipeline_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

const (
	openAIBody    = `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`
	anthropicBody = `{"model":"claude-test","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
)

var fakeUsage = testutil.Usage{Input: 1200, Output: 340, CacheRead: 1000, CacheWrite: 150, Reasoning: 120}

// AC: responses through Chowki match the fake provider's body byte for
// byte for non-streaming calls, and requests reach it unchanged.
func TestNonStreamingIsByteForByte(t *testing.T) {
	cfg := testutil.Config{Text: "Grüße, \"world\" <3", Usage: fakeUsage}
	tests := []struct {
		name, path, body string
		direct           func(t *testing.T) *testutil.Server
		upstream         func(h *harness) *testutil.Server
		upstreamPath     string
		header           map[string]string
	}{
		{"openai", "/v1/chat/completions", openAIBody,
			func(t *testing.T) *testutil.Server { c := cfg; c.APIKey = openAIKey; return testutil.NewOpenAI(t, c) },
			func(h *harness) *testutil.Server { return h.openai }, "/v1/chat/completions",
			map[string]string{"Authorization": "Bearer " + openAIKey}},
		{"anthropic", "/anthropic/v1/messages", anthropicBody,
			func(t *testing.T) *testutil.Server {
				c := cfg
				c.APIKey = anthropicKey
				return testutil.NewAnthropic(t, c)
			},
			func(h *harness) *testutil.Server { return h.anthropic }, "/v1/messages",
			map[string]string{"x-api-key": anthropicKey, "anthropic-version": "2023-06-01"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Two fresh fakes answer their first request identically.
			direct := tt.direct(t)
			h := newHarness(t, cfg, cfg)
			want := readBody(t, h.send(t.Context(), http.MethodPost, direct.URL+tt.upstreamPath, tt.header, tt.body))

			resp := h.post(t.Context(), tt.path, tt.body)
			if got := readBody(t, resp); resp.StatusCode != http.StatusOK || got != want {
				t.Fatalf("through Chowki: %d\n%s\nwant the provider's body\n%s", resp.StatusCode, got, want)
			}
			if !strings.HasPrefix(resp.Header.Get(pipeline.RequestIDHeader), "req_") {
				t.Errorf("request ID header = %q", resp.Header.Get(pipeline.RequestIDHeader))
			}
			reqs := tt.upstream(h).Requests()
			if len(reqs) != 1 || string(reqs[0].Body) != tt.body {
				t.Fatalf("the provider received %d requests; body %s", len(reqs), reqs[0].Body)
			}
			for _, name := range []string{"Authorization", "X-Api-Key"} {
				if v := reqs[0].Header.Get(name); strings.Contains(v, h.key) {
					t.Errorf("the virtual key reached the provider in %s", name)
				}
			}
		})
	}
}

// AC: streams arrive chunk by chunk. The fake waits an hour between
// chunks, so the first one arrives in time only if nothing buffers it.
func TestStreamsArriveChunkByChunk(t *testing.T) {
	cfg := testutil.Config{Text: "first second", Chunks: 2, ChunkDelay: time.Hour}
	h := newHarness(t, cfg, cfg)
	for _, tc := range []struct{ path, body, want string }{
		{"/v1/chat/completions", `{"model":"gpt-test","messages":[{}],"stream":true}`, `"content":"first "`},
		{"/anthropic/v1/messages", `{"model":"claude-test","max_tokens":5,"messages":[{}],"stream":true}`, `"text":"first "`},
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		resp := h.post(ctx, tc.path, tc.body)
		if ct := resp.Header.Get("Content-Type"); resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "text/event-stream") {
			t.Fatalf("%s: status %d, Content-Type %q", tc.path, resp.StatusCode, ct)
		}
		sc := bufio.NewScanner(resp.Body)
		found := false
		for sc.Scan() {
			if strings.Contains(sc.Text(), tc.want) {
				found = true
				break
			}
		}
		cancel()
		if !found {
			t.Errorf("%s: the first chunk didn't arrive: %v", tc.path, sc.Err())
		}
	}
}

// AC: usage and cost are correct for both API families, including cached
// tokens: the fakes report reads and writes, and each formula applies.
func TestUsageAndCost(t *testing.T) {
	const (
		// OpenAI: 50 uncached × 2 + 1000 read × 0.2 + 150 written × 2.5 + 340 out × 10, per million.
		openAICost    = 4075 / 1e6
		openAISavings = (1000*(2-0.2) - 150*(2.5-2)) / 1e6
		// Anthropic: 1200 input × 4 + 1000 read × 0.2 + 150 written × 5 + 340 out × 20, per million.
		anthropicCost    = 12550 / 1e6
		anthropicSavings = (1000*(4-0.2) - 150*(5-4)) / 1e6
	)
	openAITokens := store.Tokens{Input: 1200, Output: 340, CacheRead: 1000, CacheWrite: 150, Reasoning: 120}
	anthropicTokens := store.Tokens{Input: 1200 + 1000 + 150, Output: 340, CacheRead: 1000, CacheWrite: 150, Reasoning: 120}
	tests := []struct {
		name, path, body string
		tokens           store.Tokens
		cost, savings    float64
		header           string // the cost header of non-streaming requests
	}{
		{"openai", "/v1/chat/completions", openAIBody, openAITokens, openAICost, openAISavings, "0.00407500"},
		{"openai stream", "/v1/chat/completions", `{"model":"gpt-test","messages":[{}],"stream":true}`,
			openAITokens, openAICost, openAISavings, ""},
		{"anthropic", "/anthropic/v1/messages", anthropicBody, anthropicTokens, anthropicCost, anthropicSavings, "0.01255000"},
		{"anthropic stream", "/anthropic/v1/messages",
			`{"model":"claude-test","max_tokens":64,"messages":[{}],"stream":true}`,
			anthropicTokens, anthropicCost, anthropicSavings, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testutil.Config{Usage: fakeUsage}
			h := newHarness(t, cfg, cfg)
			resp := h.post(t.Context(), tt.path, tt.body)
			readBody(t, resp)
			if resp.StatusCode != http.StatusOK || resp.Header.Get(pipeline.CostHeader) != tt.header {
				t.Errorf("status %d, cost header %q; want 200 and %q", resp.StatusCode,
					resp.Header.Get(pipeline.CostHeader), tt.header)
			}
			recs := h.records()
			if len(recs) != 1 {
				t.Fatalf("saved %d records, want 1", len(recs))
			}
			r := recs[0]
			if r.Tokens == nil || *r.Tokens != tt.tokens {
				t.Errorf("tokens = %+v, want %+v", r.Tokens, tt.tokens)
			}
			if r.CostUSD == nil || math.Abs(*r.CostUSD-tt.cost) > 1e-12 || math.Abs(r.SavingsUSD-tt.savings) > 1e-12 ||
				r.SavingsMethod != "prompt_cache" {
				t.Errorf("cost %v, savings %v (%s); want %v, %v", r.CostUSD, r.SavingsUSD, r.SavingsMethod, tt.cost, tt.savings)
			}
			if r.Status != http.StatusOK || r.ErrorType != "" || r.KeyID == 0 || r.Latency <= 0 {
				t.Errorf("record = %+v", r)
			}
		})
	}
}

func TestOpenAIUsageChunk(t *testing.T) {
	for _, clientAsks := range []bool{false, true} {
		h := newHarness(t, testutil.Config{Usage: fakeUsage}, testutil.Config{})
		body := `{"model":"gpt-test","messages":[{}],"stream":true}`
		if clientAsks {
			body = `{"model":"gpt-test","messages":[{}],"stream":true,"stream_options":{"include_usage":true}}`
		}
		out := readBody(t, h.post(t.Context(), "/v1/chat/completions", body))
		if got := strings.Contains(out, `"prompt_tokens":1200`); got != clientAsks {
			t.Errorf("client asked for usage: %v; the usage chunk reached it: %v\n%s", clientAsks, got, out)
		}
		if !strings.HasSuffix(out, "data: [DONE]\n\n") {
			t.Errorf("stream doesn't end with [DONE]:\n%s", out)
		}
		upstream := string(h.openai.Requests()[0].Body)
		if !strings.Contains(upstream, `"include_usage":true`) || !strings.HasPrefix(upstream, `{"model":"gpt-test","messages":[{}]`) {
			t.Errorf("upstream body = %s", upstream)
		}
		if recs := h.records(); len(recs) != 1 || recs[0].Tokens == nil || recs[0].Tokens.Input != 1200 {
			t.Errorf("records = %+v", recs)
		}
	}
}

// AC: invalid or revoked keys get the error JSON of their API family.
func TestKeyErrors(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{})
	revoked := h.key
	if _, err := h.st.RevokeKey(t.Context(), revoked[:12], time.Now()); err != nil {
		t.Fatal(err)
	}
	unknown := "chowki_" + strings.Repeat("0", 49)
	tests := []struct {
		name, path string
		header     map[string]string
		code       string
	}{
		{"openai missing key", "/v1/chat/completions", nil, "missing_api_key"},
		{"openai malformed key", "/v1/chat/completions", map[string]string{"Authorization": "Bearer sk-not-chowki"}, "invalid_api_key"},
		{"openai unknown key", "/v1/chat/completions", map[string]string{"Authorization": "Bearer " + unknown}, "invalid_api_key"},
		{"openai revoked key", "/v1/chat/completions", map[string]string{"Authorization": "Bearer " + revoked}, "revoked_api_key"},
		{"anthropic missing key", "/anthropic/v1/messages", nil, "missing_api_key"},
		{"anthropic revoked key", "/anthropic/v1/messages", map[string]string{"x-api-key": revoked}, "revoked_api_key"},
		{"key in the Gemini header", "/v1/chat/completions", map[string]string{"x-goog-api-key": unknown}, "invalid_api_key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.send(t.Context(), http.MethodPost, h.url+tt.path, tt.header, openAIBody)
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401\n%s", resp.StatusCode, body)
			}
			if strings.HasPrefix(tt.path, "/anthropic/") {
				var e struct {
					Type  string `json:"type"`
					Error struct {
						Type    string `json:"type"`
						Message string `json:"message"`
					} `json:"error"`
					RequestID string `json:"request_id"`
				}
				if err := json.Unmarshal([]byte(body), &e); err != nil || e.Type != "error" ||
					e.Error.Type != "authentication_error" || e.Error.Message == "" ||
					e.RequestID != resp.Header.Get(pipeline.RequestIDHeader) {
					t.Errorf("Anthropic error = %s", body)
				}
				return
			}
			var e struct {
				Error struct {
					Message, Type, Code string
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(body), &e); err != nil || e.Error.Code != tt.code ||
				e.Error.Type != "authentication_error" || e.Error.Message == "" {
				t.Errorf("OpenAI error = %s, want code %s", body, tt.code)
			}
		})
	}
	if n := len(h.openai.Requests()) + len(h.anthropic.Requests()); n != 0 {
		t.Errorf("rejected requests reached the providers %d times", n)
	}
}

// AC: no key material appears in logs.
func TestNoKeyMaterialInLogs(t *testing.T) {
	h := newHarness(t, testutil.Config{Usage: fakeUsage}, testutil.Config{Usage: fakeUsage})
	good := h.key
	readBody(t, h.post(t.Context(), "/v1/chat/completions", openAIBody))
	readBody(t, h.post(t.Context(), "/anthropic/v1/messages", anthropicBody))
	readBody(t, h.post(t.Context(), "/v1/chat/completions", `{"model":"gpt-test","messages":[{}],"stream":true}`))
	bad := "chowki_" + strings.Repeat("A", 49)
	readBody(t, h.send(t.Context(), http.MethodPost, h.url+"/v1/chat/completions",
		map[string]string{"Authorization": "Bearer " + bad}, openAIBody))
	if _, err := h.st.RevokeKey(t.Context(), good[:12], time.Now()); err != nil {
		t.Fatal(err)
	}
	readBody(t, h.post(t.Context(), "/v1/chat/completions", openAIBody))
	h.flush()

	logs := h.logs.String()
	if !strings.Contains(logs, `"msg":"request"`) || !strings.Contains(logs, `"msg":"request rejected"`) {
		t.Fatalf("the logs don't record the requests:\n%s", logs)
	}
	for _, secret := range []string{good, good[12:], bad, openAIKey, anthropicKey, "Bearer", "hi\""} {
		if strings.Contains(logs, secret) {
			t.Errorf("the logs contain %q:\n%s", secret, logs)
		}
	}
}

func TestRouting(t *testing.T) {
	twoOpenAI := func(_ *harness, cfgs *[]config.Provider) {
		*cfgs = append(*cfgs, config.Provider{Name: "local", Type: config.TypeOpenAI, BaseURL: "http://127.0.0.1:1/v1"})
	}
	t.Run("provider prefix is removed", func(t *testing.T) {
		h := newHarness(t, testutil.Config{}, testutil.Config{}, twoOpenAI)
		body := `{"model":"openai/gpt-x",  "messages":[{"role":"user","content":"hi"}]}`
		resp := h.post(t.Context(), "/v1/chat/completions", body)
		readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if got := string(h.openai.Requests()[0].Body); got != `{"model":"gpt-x",  "messages":[{"role":"user","content":"hi"}]}` {
			t.Errorf("upstream body = %s", got)
		}
	})
	t.Run("catalog picks among several providers", func(t *testing.T) {
		h := newHarness(t, testutil.Config{}, testutil.Config{}, twoOpenAI)
		if resp := h.post(t.Context(), "/v1/chat/completions", openAIBody); resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d: %s", resp.StatusCode, readBody(t, resp))
		}
	})
	for _, tc := range []struct {
		name, path, body, code string
		opts                   []option
	}{
		{"ambiguous model", "/v1/chat/completions", `{"model":"other","messages":[{}]}`, "unknown_provider",
			[]option{twoOpenAI}},
		{"provider of another API", "/v1/chat/completions", `{"model":"anthropic/claude-test","messages":[{}]}`,
			"wrong_endpoint", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, testutil.Config{}, testutil.Config{}, tc.opts...)
			resp := h.post(t.Context(), tc.path, tc.body)
			if body := readBody(t, resp); resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, tc.code) {
				t.Errorf("status %d, body %s; want 400 %s", resp.StatusCode, body, tc.code)
			}
		})
	}
}

func TestRequestErrors(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{}, func(h *harness, _ *[]config.Provider) {
		h.gw.MaxBody = 256
	})
	tests := []struct {
		name, path, body string
		status           int
		want             string
	}{
		{"not JSON", "/v1/chat/completions", `{"model":`, 400, "Invalid request body"},
		{"no model", "/v1/chat/completions", `{"messages":[]}`, 400, `The \"model\" field`},
		{"model twice", "/v1/chat/completions", `{"model":"gpt-test","model":"x"}`, 400, "appears twice"},
		{"bad stream", "/anthropic/v1/messages", `{"model":"claude-test","stream":"yes"}`, 400, `The \"stream\" field`},
		{"bad stream options", "/v1/chat/completions", `{"model":"gpt-test","stream_options":3}`, 400, "stream_options"},
		{"too large", "/v1/chat/completions", `{"model":"gpt-test","x":"` + strings.Repeat("a", 300) + `"}`, 413,
			"request_too_large"},
		{"unknown path", "/v1/completions", `{}`, 404, "not_found"},
		{"unknown Anthropic path", "/anthropic/v1/complete", `{}`, 404, "not_found_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.post(t.Context(), tt.path, tt.body)
			if body := readBody(t, resp); resp.StatusCode != tt.status || !strings.Contains(body, tt.want) {
				t.Errorf("status %d, body %s; want %d and %q", resp.StatusCode, body, tt.status, tt.want)
			}
		})
	}
}

func TestUpstreamErrors(t *testing.T) {
	t.Run("provider error is passed through", func(t *testing.T) {
		h := newHarness(t, testutil.Config{FailStatus: 429}, testutil.Config{})
		resp := h.post(t.Context(), "/v1/chat/completions", openAIBody)
		if body := readBody(t, resp); resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(body, "fake provider failure") {
			t.Errorf("status %d, body %s", resp.StatusCode, body)
		}
		if recs := h.records(); len(recs) != 1 || recs[0].ErrorType != "upstream_429" || recs[0].CostUSD != nil {
			t.Errorf("records = %+v", recs)
		}
	})
	t.Run("unreachable provider", func(t *testing.T) {
		gone := httptest.NewServer(http.NotFoundHandler())
		gone.Close()
		h := newHarness(t, testutil.Config{}, testutil.Config{}, func(_ *harness, cfgs *[]config.Provider) {
			(*cfgs)[1].BaseURL = gone.URL
		})
		resp := h.post(t.Context(), "/anthropic/v1/messages", anthropicBody)
		if body := readBody(t, resp); resp.StatusCode != http.StatusBadGateway || !strings.Contains(body, `"type":"api_error"`) {
			t.Errorf("status %d, body %s", resp.StatusCode, body)
		}
	})
	t.Run("slow provider times out", func(t *testing.T) {
		slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			// A server notices a closed connection only after reading the body.
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
		}))
		t.Cleanup(slow.Close)
		h := newHarness(t, testutil.Config{}, testutil.Config{}, func(h *harness, cfgs *[]config.Provider) {
			h.gw.Timeout = 100 * time.Millisecond
			(*cfgs)[0].BaseURL = slow.URL + "/v1"
		})
		resp := h.post(t.Context(), "/v1/chat/completions", openAIBody)
		if body := readBody(t, resp); resp.StatusCode != http.StatusGatewayTimeout || !strings.Contains(body, "upstream_timeout") {
			t.Errorf("status %d, body %s", resp.StatusCode, body)
		}
	})
	t.Run("missing provider key", func(t *testing.T) {
		h := newHarness(t, testutil.Config{}, testutil.Config{}, func(_ *harness, cfgs *[]config.Provider) {
			(*cfgs)[0].APIKey = ""
		})
		resp := h.post(t.Context(), "/v1/chat/completions", openAIBody)
		if body := readBody(t, resp); resp.StatusCode != http.StatusInternalServerError || !strings.Contains(body, "OPENAI_API_KEY") {
			t.Errorf("status %d, body %s", resp.StatusCode, body)
		}
		if len(h.openai.Requests()) != 0 {
			t.Error("the request reached the provider without a key")
		}
	})
}

// The gateway counts each request in its metrics, and serves health checks.
func TestMetricsAndHealth(t *testing.T) {
	h := newHarness(t, testutil.Config{Usage: fakeUsage}, testutil.Config{})
	readBody(t, h.post(t.Context(), "/v1/chat/completions", openAIBody))
	readBody(t, h.send(t.Context(), http.MethodPost, h.url+"/v1/chat/completions", nil, openAIBody)) // no key
	for path, want := range map[string]string{
		"/metrics": `chowki_requests_total{family="openai",provider="openai",model="gpt-test",status="200",cache="bypass"} 1` +
			"\n",
		"/healthz": `{"status":"ok"}`,
		"/readyz":  `{"status":"ready"}`,
	} {
		resp := h.send(t.Context(), http.MethodGet, h.url+path, nil, "")
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK || !strings.Contains(body, want) {
			t.Errorf("GET %s = %d\n%s\nwant %s", path, resp.StatusCode, body, want)
		}
	}
	resp := h.send(t.Context(), http.MethodGet, h.url+"/metrics", nil, "")
	body := readBody(t, resp)
	for _, want := range []string{`status="401"`, `chowki_tokens_total{type="input"} 1200`, "chowki_overhead_seconds_count 2"} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics lacks %s:\n%s", want, body)
		}
	}
}
