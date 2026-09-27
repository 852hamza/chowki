package pipeline_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/testutil"
	"github.com/852hamza/chowki/internal/translate"
)

// weatherArgs are the arguments of the fakes' calls of get_weather.
const weatherArgs = `{"city":"Paris"}`

// AC: an option that the target can't honor gets a 400 that names it.
func TestUnsupportedOption(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{}, withGemini(testutil.Config{}))
	for _, tc := range []struct{ model, option, param string }{
		{"anthropic/claude-test", `"seed":7`, "seed"},
		{"anthropic/claude-test", `"temperature":1.8`, "temperature"},
		{"gemini/gemini-test", `"stop":["a","b","c","d","e","f"]`, "stop"},
		{"anthropic/claude-test", `"logit_bias":{"1":5}`, "logit_bias"},
		{"gemini/gemini-test", `"audio":{"voice":"alloy","format":"mp3"}`, "audio"},
	} {
		body := `{"model":"` + tc.model + `",` + tc.option + `,"messages":[{"role":"user","content":"hi"}]}`
		resp := h.post(t.Context(), "/v1/chat/completions", body)
		var e struct {
			Error struct{ Message, Type, Code, Param string } `json:"error"`
		}
		if err := json.Unmarshal([]byte(readBody(t, resp)), &e); err != nil || resp.StatusCode != http.StatusBadRequest ||
			e.Error.Code != "unsupported_option" || e.Error.Param != tc.param || e.Error.Type != "invalid_request_error" {
			t.Errorf("%s with %s: %d %+v; want 400 unsupported_option naming %s", tc.model, tc.option,
				resp.StatusCode, e.Error, tc.param)
		}
	}
	if n := len(h.anthropic.Requests()) + len(h.gemini.Requests()); n != 0 {
		t.Errorf("the providers got %d requests; want none", n)
	}
	if recs := h.records(); len(recs) != 5 || recs[0].ErrorType != "unsupported_option" || recs[0].Provider != "anthropic" {
		t.Errorf("records = %+v", recs)
	}
}

// Anthropic requires max_tokens: a translated request without it gets the
// model's maximum from the catalog, or a default.
func TestTranslatedMaxTokens(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{})
	readBody(t, h.post(t.Context(), "/v1/chat/completions",
		`{"model":"anthropic/claude-test","messages":[{"role":"user","content":"hi"}]}`))
	sent := h.anthropic.Requests()[0]
	if !strings.Contains(string(sent.Body), `"max_tokens":4096`) ||
		sent.Header.Get("anthropic-version") != translate.AnthropicVersion || sent.Header.Get("x-api-key") != anthropicKey {
		t.Errorf("the provider got %v\n%s", sent.Header, sent.Body)
	}
}

// Gemini's thought signature reaches the client in extra_content, and when
// the client drops it, Memory puts it back into the next request.
func TestTranslatedThoughtSignature(t *testing.T) {
	call := &testutil.ToolCall{Name: "get_weather", Arguments: weatherArgs}
	h := newHarness(t, testutil.Config{}, testutil.Config{}, withGemini(testutil.Config{ToolCall: call}))
	resp := h.post(t.Context(), "/v1/chat/completions", `{"model":"gemini/gemini-test","messages":[`+
		`{"role":"user","content":"Is Paris warm?"}],"tools":[{"type":"function","function":{"name":"get_weather"}}]}`)
	var first struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID           string `json:"id"`
					ExtraContent struct {
						Google struct {
							ThoughtSignature string `json:"thought_signature"`
						} `json:"google"`
					} `json:"extra_content"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(readBody(t, resp)), &first); err != nil || len(first.Choices) != 1 ||
		len(first.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("first answer: %v", err)
	}
	tc := first.Choices[0].Message.ToolCalls[0]
	if tc.ExtraContent.Google.ThoughtSignature != testutil.FakeSignature {
		t.Errorf("thought signature = %q", tc.ExtraContent.Google.ThoughtSignature)
	}
	// The next request of the loop, from a client that kept only the ID.
	readBody(t, h.post(t.Context(), "/v1/chat/completions", `{"model":"gemini/gemini-test","messages":[`+
		`{"role":"user","content":"Is Paris warm?"},{"role":"assistant","tool_calls":[{"id":"`+tc.ID+`",`+
		`"type":"function","function":{"name":"get_weather","arguments":"{}"}}]},`+
		`{"role":"tool","tool_call_id":"`+tc.ID+`","content":"21C"}]}`))
	if sent := string(h.gemini.Requests()[1].Body); !strings.Contains(sent, `"thoughtSignature":"`+testutil.FakeSignature) ||
		!strings.Contains(sent, `"functionResponse":{"name":"get_weather","response":{"output":"21C"}}`) {
		t.Errorf("the provider got %s", sent)
	}
}

// An alias can span APIs: a failing Anthropic target falls back to OpenAI.
func TestFallbackAcrossAPIs(t *testing.T) {
	h := newHarness(t, testutil.Config{Text: "from OpenAI"}, testutil.Config{FailStatus: http.StatusServiceUnavailable},
		func(h *harness, _ *[]config.Provider) {
			h.aliases = map[string][]string{"any": {"anthropic/claude-test", "openai/gpt-test"}}
		})
	for _, stream := range []string{"false", "true"} {
		resp := h.post(t.Context(), "/v1/chat/completions",
			`{"model":"any","stream":`+stream+`,"messages":[{"role":"user","content":"hi"}]}`)
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK || !strings.Contains(body, `"model":"gpt-test"`) {
			t.Errorf("stream %s: %d\n%s", stream, resp.StatusCode, body)
		}
	}
	if recs := h.records(); len(recs) != 2 || recs[0].Provider != "openai" || recs[0].APIFamily != "openai" {
		t.Errorf("records = %+v", recs)
	}
}
