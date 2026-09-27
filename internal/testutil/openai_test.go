package testutil

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type openAIUsageJSON struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens     int `json:"cached_tokens"`
		CacheWriteTokens int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type openAIChunk struct {
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsageJSON `json:"usage"`
}

func wantOpenAIUsage(u Usage) openAIUsageJSON {
	var w openAIUsageJSON
	w.PromptTokens, w.CompletionTokens, w.TotalTokens = u.Input, u.Output, u.Input+u.Output
	w.PromptTokensDetails.CachedTokens, w.PromptTokensDetails.CacheWriteTokens = u.CacheRead, u.CacheWrite
	w.CompletionTokensDetails.ReasoningTokens = u.Reasoning
	return w
}

func TestOpenAIChat(t *testing.T) {
	s := NewOpenAI(t, Config{APIKey: "sk-EXAMPLE", Text: "Hello, world!", Chunks: 4, Usage: testUsage})
	url := s.URL + "/v1/chat/completions"
	auth := map[string]string{"Authorization": "Bearer sk-EXAMPLE"}
	const request = `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]`

	t.Run("json", func(t *testing.T) {
		resp := post(t.Context(), t, url, auth, request+`}`)
		checkResponse(t, resp, http.StatusOK, "application/json")
		var got struct {
			Object  string `json:"object"`
			Model   string `json:"model"`
			Choices []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage openAIUsageJSON `json:"usage"`
		}
		decode(t, resp.Body, &got)
		if got.Object != "chat.completion" || got.Model != "gpt-test" || len(got.Choices) != 1 {
			t.Fatalf("response = %+v", got)
		}
		c := got.Choices[0]
		if c.Message.Role != "assistant" || c.Message.Content != "Hello, world!" || c.FinishReason != "stop" {
			t.Errorf("choice = %+v", c)
		}
		if want := wantOpenAIUsage(testUsage); got.Usage != want {
			t.Errorf("usage = %+v, want %+v", got.Usage, want)
		}
	})

	for _, includeUsage := range []bool{false, true} {
		name := "stream without usage"
		body := request + `,"stream":true}`
		if includeUsage {
			name = "stream with usage"
			body = request + `,"stream":true,"stream_options":{"include_usage":true}}`
		}
		t.Run(name, func(t *testing.T) {
			resp := post(t.Context(), t, url, auth, body)
			checkResponse(t, resp, http.StatusOK, "text/event-stream")
			events := readEvents(t, resp.Body)
			if last := events[len(events)-1]; last != (event{data: "[DONE]"}) {
				t.Fatalf("last event = %+v, want data: [DONE]", last)
			}
			var text strings.Builder
			var pieces, stops int
			var usage *openAIUsageJSON
			for i, ev := range events[:len(events)-1] {
				if ev.name != "" {
					t.Errorf("event %d has name %q; OpenAI streams use data lines only", i, ev.name)
				}
				var fields map[string]json.RawMessage
				decode(t, strings.NewReader(ev.data), &fields)
				rawUsage, hasUsage := fields["usage"]
				var c openAIChunk
				decode(t, strings.NewReader(ev.data), &c)
				if c.Object != "chat.completion.chunk" || c.Model != "gpt-test" {
					t.Errorf("chunk %d = %s", i, ev.data)
				}
				switch {
				case !includeUsage && hasUsage:
					t.Errorf("chunk %d has usage, but the request didn't ask for it: %s", i, ev.data)
				case includeUsage && !hasUsage:
					t.Errorf("chunk %d has no usage field: %s", i, ev.data)
				case includeUsage && string(rawUsage) != "null":
					if len(c.Choices) != 0 {
						t.Errorf("usage chunk %d has choices: %s", i, ev.data)
					}
					usage = c.Usage
				}
				for _, ch := range c.Choices {
					if ch.FinishReason != nil && *ch.FinishReason == "stop" {
						stops++
					}
					if ch.Delta.Content != "" {
						pieces++
						text.WriteString(ch.Delta.Content)
					}
				}
			}
			if text.String() != "Hello, world!" || pieces != 4 || stops != 1 {
				t.Errorf("streamed text = %q in %d pieces with %d stops, want %q in 4 with 1",
					text.String(), pieces, stops, "Hello, world!")
			}
			if !includeUsage {
				return
			}
			if usage == nil {
				t.Fatal("stream has no usage chunk")
			}
			if want := wantOpenAIUsage(testUsage); *usage != want {
				t.Errorf("usage = %+v, want %+v", *usage, want)
			}
		})
	}
}

func TestOpenAIUsageOmitsZeroCacheWrite(t *testing.T) {
	s := NewOpenAI(t, Config{Usage: Usage{Input: 10, Output: 5}})
	resp := post(t.Context(), t, s.URL+"/v1/chat/completions", nil, `{"model":"m","messages":[{}]}`)
	checkResponse(t, resp, http.StatusOK, "application/json")
	var got struct {
		Usage struct {
			PromptTokensDetails map[string]int `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	decode(t, resp.Body, &got)
	if _, ok := got.Usage.PromptTokensDetails["cache_write_tokens"]; ok {
		t.Errorf("prompt_tokens_details = %v, want no cache_write_tokens", got.Usage.PromptTokensDetails)
	}
}
