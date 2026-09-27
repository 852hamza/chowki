package testutil

import (
	"net/http"
	"strings"
	"testing"
)

type geminiResponseJSON struct {
	Candidates []struct {
		Content struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
		TotalTokenCount         int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
	ResponseID   string `json:"responseId"`
}

// geminiTestUsage is testUsage without the cache write tokens that Gemini
// doesn't report.
var geminiTestUsage = Usage{Input: testUsage.Input, Output: testUsage.Output, CacheRead: testUsage.CacheRead,
	Reasoning: testUsage.Reasoning}

func checkGeminiUsage(t *testing.T, got geminiResponseJSON, output int) {
	t.Helper()
	u := got.UsageMetadata
	want := geminiTestUsage
	if u.PromptTokenCount != want.Input || u.CandidatesTokenCount != output || u.CachedContentTokenCount != want.CacheRead ||
		u.ThoughtsTokenCount != want.Reasoning || u.TotalTokenCount != want.Input+output+want.Reasoning {
		t.Errorf("usageMetadata = %+v, want %+v with output %d", u, want, output)
	}
}

func TestGeminiGenerateContent(t *testing.T) {
	s := NewGemini(t, Config{APIKey: "EXAMPLE-key", Text: "Hello, world!", Usage: geminiTestUsage})
	resp := post(t.Context(), t, s.URL+"/v1beta/models/gemini-test:generateContent",
		map[string]string{"x-goog-api-key": "EXAMPLE-key"}, `{"contents":[{"parts":[{"text":"hi"}]}]}`)
	checkResponse(t, resp, http.StatusOK, "application/json")
	var got geminiResponseJSON
	decode(t, resp.Body, &got)
	if len(got.Candidates) != 1 || got.ModelVersion != "gemini-test" || got.ResponseID == "" {
		t.Fatalf("response = %+v", got)
	}
	c := got.Candidates[0]
	if c.Content.Role != "model" || len(c.Content.Parts) != 1 || c.Content.Parts[0].Text != "Hello, world!" ||
		c.FinishReason != "STOP" {
		t.Errorf("candidate = %+v", c)
	}
	checkGeminiUsage(t, got, geminiTestUsage.Output)
}

func TestGeminiStream(t *testing.T) {
	s := NewGemini(t, Config{Text: "Hello, world!", Chunks: 4, Usage: geminiTestUsage})
	resp := post(t.Context(), t, s.URL+"/v1beta/models/gemini-test:streamGenerateContent?alt=sse", nil,
		`{"contents":[{"parts":[{"text":"hi"}]}]}`)
	checkResponse(t, resp, http.StatusOK, "text/event-stream")
	events := readEvents(t, resp.Body)
	if len(events) != 4 {
		t.Fatalf("got %d chunks, want 4", len(events))
	}

	var text strings.Builder
	prevOutput := 0
	for i, ev := range events {
		if ev.name != "" {
			t.Errorf("chunk %d has event name %q; Gemini streams use data lines only", i, ev.name)
		}
		var r geminiResponseJSON
		decode(t, strings.NewReader(ev.data), &r)
		c := r.Candidates[0]
		text.WriteString(c.Content.Parts[0].Text)
		last := i == len(events)-1
		if (c.FinishReason == "STOP") != last {
			t.Errorf("chunk %d finishReason = %q", i, c.FinishReason)
		}
		if out := r.UsageMetadata.CandidatesTokenCount; out < prevOutput {
			t.Errorf("chunk %d output tokens went down from %d to %d", i, prevOutput, out)
		} else {
			prevOutput = out
		}
		if last {
			checkGeminiUsage(t, r, geminiTestUsage.Output)
		}
	}
	if text.String() != "Hello, world!" {
		t.Errorf("streamed text = %q, want %q", text.String(), "Hello, world!")
	}
}
