package pipeline_test

import (
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

// The Responses API, as the Codex CLI uses it, relays to OpenAI-compatible
// providers with usage, cost and redaction.
func TestResponses(t *testing.T) {
	// OpenAI: 50 uncached × 2 + 1000 read × 0.2 + 150 written × 2.5 + 340 out × 10, per million.
	const cost = 4075 / 1e6
	tokens := store.Tokens{Input: 1200, Output: 340, CacheRead: 1000, CacheWrite: 150, Reasoning: 120}
	for _, stream := range []string{"false", "true"} {
		t.Run("stream "+stream, func(t *testing.T) {
			h := newHarness(t, testutil.Config{Usage: fakeUsage}, testutil.Config{})
			body := `{"model":"gpt-test","instructions":"Mail jane.doe@company.io.","stream":` + stream +
				`,"input":[{"role":"user","content":[{"type":"input_text","text":"Call +14155552671"}]}]}`
			resp := h.post(t.Context(), "/v1/responses", body)
			got := readBody(t, resp)
			if resp.StatusCode != http.StatusOK || !strings.Contains(got, testutil.DefaultText[:5]) ||
				resp.Header.Get("x-chowki-redactions") != "2" {
				t.Fatalf("status %d, headers %v\n%s", resp.StatusCode, resp.Header, got)
			}
			if stream == "false" && resp.Header.Get(pipeline.CostHeader) != "0.00407500" {
				t.Errorf("cost header %q", resp.Header.Get(pipeline.CostHeader))
			}
			sent := string(h.openai.Requests()[0].Body)
			if strings.Contains(sent, "jane.doe") || strings.Contains(sent, "4155552671") ||
				strings.Contains(sent, "stream_options") {
				t.Errorf("the provider got %s", sent)
			}
			recs := h.records()
			if len(recs) != 1 || recs[0].Endpoint != "/v1/responses" || recs[0].Tokens == nil || *recs[0].Tokens != tokens ||
				recs[0].CostUSD == nil || math.Abs(*recs[0].CostUSD-cost) > 1e-12 {
				t.Errorf("records = %+v", recs)
			}
		})
	}
}

// Translation covers chat completions only: the Responses API reaches
// OpenAI-compatible providers.
func TestResponsesOtherAPIs(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{})
	resp := h.post(t.Context(), "/v1/responses", `{"model":"anthropic/claude-test","input":"hi"}`)
	if body := readBody(t, resp); resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "wrong_endpoint") {
		t.Errorf("status %d\n%s", resp.StatusCode, body)
	}
}
