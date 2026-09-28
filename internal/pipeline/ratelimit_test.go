package pipeline_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

func setKeyLimits(t *testing.T, h *harness, rpm, tpm int64) {
	t.Helper()
	if _, err := h.st.UpdateKey(t.Context(), h.key[:auth.PrefixLen], store.KeyUpdate{RPM: &rpm, TPM: &tpm}); err != nil {
		t.Fatal(err)
	}
}

// checkRateLimited checks a response that rejects a request over a rate
// limit, and returns its Retry-After in seconds.
func checkRateLimited(t *testing.T, resp *http.Response, path, wantMessage string) int {
	t.Helper()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429\n%s", resp.StatusCode, body)
	}
	after, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	ms, msErr := strconv.Atoi(resp.Header.Get("retry-after-ms"))
	if err != nil || msErr != nil || after < 1 || ms < 1 || ms > after*1000 {
		t.Errorf("Retry-After %q, retry-after-ms %q; want whole seconds and the milliseconds within them",
			resp.Header.Get("Retry-After"), resp.Header.Get("retry-after-ms"))
	}
	var e struct {
		Type  string `json:"type"`
		Error struct {
			Type, Code, Message string
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &e); err != nil || e.Error.Type != "rate_limit_error" ||
		!strings.Contains(e.Error.Message, wantMessage) {
		t.Errorf("error = %s; want a rate_limit_error that says %q", body, wantMessage)
	}
	if anthropic := strings.HasPrefix(path, "/anthropic/"); anthropic && e.Type != "error" ||
		!anthropic && e.Error.Code != "rate_limit_exceeded" {
		t.Errorf("error = %s; want the family's format, with the code rate_limit_exceeded for OpenAI", body)
	}
	return after
}

func TestRequestsPerMinute(t *testing.T) {
	for _, tt := range []struct{ name, path, body string }{
		{"openai", "/v1/chat/completions", openAIBody},
		{"anthropic", "/anthropic/v1/messages", anthropicBody},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, testutil.Config{}, testutil.Config{})
			setKeyLimits(t, h, 2, 0)
			for i := range 2 {
				if resp := h.post(t.Context(), tt.path, tt.body); resp.StatusCode != http.StatusOK {
					t.Fatalf("request %d: status %d, want 200 within the limit", i+1, resp.StatusCode)
				}
			}
			// 2 requests a minute refill one every 30 seconds. The limit
			// applies before the body is read, so a broken body doesn't
			// matter.
			after := checkRateLimited(t, h.post(t.Context(), tt.path, "not JSON"), tt.path,
				"This key has reached its limit of 2 requests per minute. Try again in ")
			if after != 30 && after != 29 { // 29 if the first requests took over a second
				t.Errorf("Retry-After = %d, want 30", after)
			}
			if n := len(h.openai.Requests()) + len(h.anthropic.Requests()); n != 2 {
				t.Errorf("the providers got %d requests, want 2", n)
			}
			if recs := h.records(); len(recs) != 3 || recs[2].ErrorType != "rate_limit_exceeded" {
				t.Errorf("records = %+v; want the rejection recorded as rate_limit_exceeded", recs)
			}
		})
	}
}

func TestTokensPerMinute(t *testing.T) {
	// A request uses 1.1 million tokens: far more than a minute's worth.
	h := newHarness(t, testutil.Config{Usage: bigUsage}, testutil.Config{})
	setKeyLimits(t, h, 0, 1000)
	if resp := h.post(t.Context(), "/v1/chat/completions", openAIBody); resp.StatusCode != http.StatusOK {
		t.Fatalf("first request: status %d, want 200 with a full bucket", resp.StatusCode)
	}
	// The bucket owes 1,099,000 tokens: 1099 minutes at 1000 a minute.
	after := checkRateLimited(t, h.post(t.Context(), "/v1/chat/completions", openAIBody), "/v1/chat/completions",
		"This key has reached its limit of 1000 tokens per minute; this request needs about 16 tokens.")
	if after < 1099*60 || after > 1100*60 {
		t.Errorf("Retry-After = %d, want about %d", after, 1099*60)
	}
	if n := len(h.openai.Requests()); n != 1 {
		t.Errorf("the provider got %d requests, want 1", n)
	}
}

// A request that fails upstream returns the tokens it held.
func TestFailedRequestsReturnTheirTokens(t *testing.T) {
	h := newHarness(t, testutil.Config{FailStatus: http.StatusInternalServerError}, testutil.Config{})
	setKeyLimits(t, h, 0, 1000)
	// About 750 tokens each: two don't fit in 1000 at once.
	body := `{"model":"gpt-test","messages":[{"role":"user","content":"` + strings.Repeat("x", 2950) + `"}]}`
	for i := range 3 {
		if resp := h.post(t.Context(), "/v1/chat/completions", body); resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("request %d: status %d, want the provider's 500", i+1, resp.StatusCode)
		}
	}
}
