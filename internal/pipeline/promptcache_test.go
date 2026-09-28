package pipeline_test

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/promptcache"
	"github.com/852hamza/chowki/internal/testutil"
)

func withPromptCache(h *harness, _ *[]config.Provider) { h.gw.PromptCache = promptcache.New() }

// longSystem is a system prompt of about 1000 tokens, above the test
// model's minimum of 512.
var longSystem = strings.Repeat("Answer as a careful assistant. ", 130)

func TestPromptCacheBreakpoint(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{}, withPromptCache)
	system, _ := json.Marshal(longSystem)
	body := `{"model":"claude-test","max_tokens":64,"system":` + string(system) +
		`,"messages":[{"role":"user","content":"hi"}]}`
	for range 2 {
		if resp := h.post(t.Context(), "/anthropic/v1/messages", body); resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	}
	reqs := h.anthropic.Requests()
	if string(reqs[0].Body) != body {
		t.Errorf("the first request changed; a new prefix gets no breakpoint:\n%s", reqs[0].Body)
	}
	var sent struct {
		System []struct {
			Type, Text   string
			CacheControl json.RawMessage `json:"cache_control"`
		} `json:"system"`
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(reqs[1].Body, &sent); err != nil {
		t.Fatalf("the second request isn't JSON: %v\n%s", err, reqs[1].Body)
	}
	if len(sent.System) != 1 || sent.System[0].Text != longSystem || sent.System[0].Type != "text" ||
		string(sent.System[0].CacheControl) != `{"type":"ephemeral"}` ||
		string(sent.Messages) != `[{"role":"user","content":"hi"}]` {
		t.Errorf("the repeated request didn't get one breakpoint at the end of its unchanged system prompt:\n%s",
			reqs[1].Body)
	}
	if !strings.Contains(h.logs.String(), `"prompt_cache_breakpoint":true`) {
		t.Error("the log doesn't show the breakpoint")
	}
}

// AC: the optimizer never adds a breakpoint when cache_control already
// exists, so it never exceeds the provider's limit of breakpoints.
func TestPromptCacheRespectsTheClient(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{}, withPromptCache)
	system, _ := json.Marshal(longSystem)
	body := `{"model":"claude-test","max_tokens":64,"system":` + string(system) + `,"messages":[{"role":"user",` +
		`"content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}]}`
	for range 3 {
		readBody(t, h.post(t.Context(), "/anthropic/v1/messages", body))
	}
	for i, r := range h.anthropic.Requests() {
		if string(r.Body) != body {
			t.Errorf("request %d changed:\n%s", i+1, r.Body)
		}
	}
}

// AC: net savings can be negative, and are stored as such.
func TestNegativeSavingsAreStored(t *testing.T) {
	// A cache write of 5000 tokens at $5 instead of $4 per million, and no
	// reads, loses $0.005.
	h := newHarness(t, testutil.Config{}, testutil.Config{Usage: testutil.Usage{Input: 100, Output: 10, CacheWrite: 5000}})
	readBody(t, h.post(t.Context(), "/anthropic/v1/messages", anthropicBody))
	recs := h.records()
	if len(recs) != 1 || math.Abs(recs[0].SavingsUSD-(-0.005)) > 1e-12 || recs[0].SavingsMethod != "prompt_cache" {
		t.Errorf("records = %+v; want savings of -$0.005 by prompt_cache", recs)
	}
}
