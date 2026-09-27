package testutil

import (
	"net/http"
	"strings"
	"testing"
)

type anthropicUsageJSON struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokensDetails      *struct {
		ThinkingTokens int `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

type anthropicDelta struct {
	Type  string `json:"type"`
	Delta struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage *anthropicUsageJSON `json:"usage"`
}

func TestAnthropicMessages(t *testing.T) {
	s := NewAnthropic(t, Config{APIKey: "sk-ant-EXAMPLE", Text: "Hello, world!", Chunks: 4, Usage: testUsage})
	url := s.URL + "/v1/messages"
	const request = `{"model":"claude-test","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]`

	for _, auth := range []map[string]string{
		{"x-api-key": "sk-ant-EXAMPLE", "anthropic-version": anthropicVersion},
		{"Authorization": "Bearer sk-ant-EXAMPLE", "anthropic-version": anthropicVersion},
	} {
		resp := post(t.Context(), t, url, auth, request+`}`)
		checkResponse(t, resp, http.StatusOK, "application/json")
		if resp.Header.Get("request-id") == "" {
			t.Error("response has no request-id header")
		}
		var got struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Model   string `json:"model"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StopReason string             `json:"stop_reason"`
			Usage      anthropicUsageJSON `json:"usage"`
		}
		decode(t, resp.Body, &got)
		if got.Type != "message" || got.Role != "assistant" || got.Model != "claude-test" || got.StopReason != "end_turn" ||
			len(got.Content) != 1 || got.Content[0].Type != "text" || got.Content[0].Text != "Hello, world!" {
			t.Errorf("message = %+v", got)
		}
		u := got.Usage
		if u.InputTokens != testUsage.Input || u.OutputTokens != testUsage.Output ||
			u.CacheCreationInputTokens != testUsage.CacheWrite || u.CacheReadInputTokens != testUsage.CacheRead ||
			u.OutputTokensDetails == nil || u.OutputTokensDetails.ThinkingTokens != testUsage.Reasoning {
			t.Errorf("usage = %+v, want %+v", u, testUsage)
		}
	}
}

func TestAnthropicStream(t *testing.T) {
	s := NewAnthropic(t, Config{Text: "Hello, world!", Chunks: 4, Usage: testUsage})
	resp := post(t.Context(), t, s.URL+"/v1/messages", map[string]string{"anthropic-version": anthropicVersion},
		`{"model":"claude-test","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"stream":true}`)
	checkResponse(t, resp, http.StatusOK, "text/event-stream")
	events := readEvents(t, resp.Body)

	var names []string
	for _, ev := range events {
		names = append(names, ev.name)
		var d anthropicDelta
		decode(t, strings.NewReader(ev.data), &d)
		if d.Type != ev.name {
			t.Errorf("event %q has data type %q", ev.name, d.Type)
		}
	}
	want := "message_start content_block_start ping content_block_delta content_block_delta content_block_delta " +
		"content_block_delta content_block_stop message_delta message_stop"
	if got := strings.Join(names, " "); got != want {
		t.Fatalf("events = %s\nwant     %s", got, want)
	}

	var start struct {
		Message struct {
			Model string             `json:"model"`
			Usage anthropicUsageJSON `json:"usage"`
		} `json:"message"`
	}
	decode(t, strings.NewReader(events[0].data), &start)
	su := start.Message.Usage
	if start.Message.Model != "claude-test" || su.InputTokens != testUsage.Input || su.OutputTokens != 1 ||
		su.CacheCreationInputTokens != testUsage.CacheWrite || su.CacheReadInputTokens != testUsage.CacheRead {
		t.Errorf("message_start = %s", events[0].data)
	}

	var text strings.Builder
	for _, ev := range events[3:7] {
		var d anthropicDelta
		decode(t, strings.NewReader(ev.data), &d)
		if d.Delta.Type != "text_delta" {
			t.Errorf("delta type = %q, want text_delta", d.Delta.Type)
		}
		text.WriteString(d.Delta.Text)
	}
	if text.String() != "Hello, world!" {
		t.Errorf("streamed text = %q, want %q", text.String(), "Hello, world!")
	}

	var delta anthropicDelta
	decode(t, strings.NewReader(events[8].data), &delta)
	if delta.Delta.StopReason != "end_turn" || delta.Usage == nil || delta.Usage.OutputTokens != testUsage.Output ||
		delta.Usage.OutputTokensDetails == nil || delta.Usage.OutputTokensDetails.ThinkingTokens != testUsage.Reasoning {
		t.Errorf("message_delta = %s", events[8].data)
	}
}
