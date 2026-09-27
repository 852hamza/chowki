package pipeline_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

// bigUsage makes a request cost $3.00 through OpenAI (1M input tokens at
// $2 and 100k output tokens at $10 per million) and $6.00 through
// Anthropic ($4 and $20).
var bigUsage = testutil.Usage{Input: 1_000_000, Output: 100_000}

func setKeyBudget(t *testing.T, h *harness, usd float64) {
	t.Helper()
	if _, err := h.st.UpdateKey(t.Context(), h.key[:auth.PrefixLen], store.KeyUpdate{BudgetUSD: &usd}); err != nil {
		t.Fatal(err)
	}
}

// checkBudgetError checks a response that rejects a request for a used-up
// budget, in the error format of the family that path belongs to.
func checkBudgetError(t *testing.T, resp *http.Response, path, wantMessage string) {
	t.Helper()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("x-should-retry") != "false" {
		t.Fatalf("status %d, x-should-retry %q; want 429 and false\n%s", resp.StatusCode,
			resp.Header.Get("x-should-retry"), body)
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
		!anthropic && e.Error.Code != "budget_exceeded" {
		t.Errorf("error = %s; want the family's format, with the code budget_exceeded for OpenAI", body)
	}
}

// AC: an exceeded budget returns 429 budget_exceeded.
func TestBudgetExceeded(t *testing.T) {
	// One request costs more than each budget.
	tests := []struct {
		name, path, body string
		budget           float64
		message          string
	}{
		{"openai", "/v1/chat/completions", openAIBody, 2.5, "The monthly budget of this key, $2.50, is used up: $3.00 spent"},
		{"openai stream", "/v1/chat/completions", `{"model":"gpt-test","messages":[{}],"stream":true}`, 2.5,
			"The monthly budget of this key, $2.50, is used up: $3.00 spent"},
		{"anthropic", "/anthropic/v1/messages", anthropicBody, 5, "The monthly budget of this key, $5.00, is used up: $6.00 spent"},
		{"anthropic stream", "/anthropic/v1/messages",
			`{"model":"claude-test","max_tokens":64,"messages":[{}],"stream":true}`, 5,
			"The monthly budget of this key, $5.00, is used up: $6.00 spent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testutil.Config{Usage: bigUsage}
			h := newHarness(t, cfg, cfg)
			setKeyBudget(t, h, tt.budget)
			first := h.post(t.Context(), tt.path, tt.body)
			readBody(t, first)
			if first.StatusCode != http.StatusOK {
				t.Fatalf("first request: status %d, want 200 while the budget has room", first.StatusCode)
			}
			checkBudgetError(t, h.post(t.Context(), tt.path, tt.body), tt.path, tt.message)

			if n := len(h.openai.Requests()) + len(h.anthropic.Requests()); n != 1 {
				t.Errorf("the providers got %d requests, want 1: the rejected one must not reach them", n)
			}
			recs := h.records()
			if len(recs) != 2 || recs[1].Status != http.StatusTooManyRequests || recs[1].ErrorType != "budget_exceeded" {
				t.Errorf("records = %+v; want the rejection recorded as budget_exceeded", recs)
			}
		})
	}
}

func TestProjectBudget(t *testing.T) {
	h := newHarness(t, testutil.Config{Usage: bigUsage}, testutil.Config{})
	budget := 2.5
	if _, err := h.st.UpdateProject(t.Context(), "default", store.ProjectUpdate{BudgetUSD: &budget}); err != nil {
		t.Fatal(err)
	}
	readBody(t, h.post(t.Context(), "/v1/chat/completions", openAIBody))
	checkBudgetError(t, h.post(t.Context(), "/v1/chat/completions", openAIBody), "/v1/chat/completions",
		`The monthly budget of the project "default", $2.50, is used up: $3.00 spent`)
}

// AC: spend survives a restart.
func TestSpendSurvivesRestart(t *testing.T) {
	h := newHarness(t, testutil.Config{Usage: bigUsage}, testutil.Config{})
	setKeyBudget(t, h, 2.5)
	if resp := h.post(t.Context(), "/v1/chat/completions", openAIBody); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	h.restart()
	checkBudgetError(t, h.post(t.Context(), "/v1/chat/completions", openAIBody), "/v1/chat/completions",
		"$3.00 spent")

	spend, err := h.st.SpendByKey(t.Context(), store.Period(time.Now()))
	if err != nil || len(spend) != 1 || spend[0].USD != 3 {
		t.Errorf("saved spend = %+v, %v; want $3", spend, err)
	}
}

// Requests to models without a price count as free: their cost is unknown.
func TestUnpricedModelsCountAsFree(t *testing.T) {
	h := newHarness(t, testutil.Config{Usage: fakeUsage}, testutil.Config{})
	setKeyBudget(t, h, 0.000001)
	for i := range 3 {
		resp := h.post(t.Context(), "/v1/chat/completions", `{"model":"local-model","messages":[{}]}`)
		if readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i+1, resp.StatusCode)
		}
	}
}
