package translate

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/usage"
)

const hi = `"messages":[{"role":"user","content":"hi"}]`

// request builds a request body with the given fields and a user message.
func request(fields string) []byte {
	if fields != "" {
		fields += ","
	}
	return []byte(`{"model":"m",` + fields + hi + `}`)
}

func TestParseRequest(t *testing.T) {
	for _, tc := range []struct {
		name, fields, param string // param is empty when the request is valid
	}{
		{"unknown option", `"best_of":2`, "best_of"},
		{"logit bias", `"logit_bias":{"50256":-100}`, "logit_bias"},
		{"log probabilities", `"logprobs":true`, "logprobs"},
		{"top log probabilities", `"top_logprobs":3`, "top_logprobs"},
		{"stored", `"store":true`, "store"},
		{"no choices", `"n":0`, "n"},
		{"no tokens", `"max_tokens":0`, "max_tokens"},
		{"flex tier", `"service_tier":"flex"`, "service_tier"},
		{"audio output", `"modalities":["text","audio"]`, "modalities"},
		{"prediction", `"prediction":{"type":"content","content":"x"}`, "prediction"},
		{"web search", `"web_search_options":{}`, "web_search_options"},
		{"verbosity", `"verbosity":"low"`, "verbosity"},
		{"custom tool", `"tools":[{"type":"custom","custom":{"name":"x"}}]`, "tools[0]"},
		{"bad tool choice", `"tool_choice":"any"`, "tool_choice"},
		{"text format", `"response_format":{"type":"text"}`, ""},
		{"accepted no-ops", `"metadata":{"a":"b"},"prompt_cache_key":"k","prompt_cache_retention":"24h",` +
			`"service_tier":"auto","logprobs":false,"logit_bias":{},"modalities":["text"],"store":false,` +
			`"stream_options":{"include_usage":true,"include_obfuscation":false},"user":null`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequest(request(tc.fields))
			var e *Error
			switch {
			case tc.param == "" && err != nil:
				t.Errorf("ParseRequest() = %v, want no error", err)
			case tc.param != "" && (!errors.As(err, &e) || e.Param != tc.param):
				t.Errorf("ParseRequest() = %v, want an error about %s", err, tc.param)
			}
		})
	}
}

func TestParseMessages(t *testing.T) {
	for _, tc := range []struct{ name, messages, param string }{
		{"no messages", `[]`, "messages"},
		{"author name", `[{"role":"user","name":"ann","content":"hi"}]`, "messages[0].name"},
		{"function role", `[{"role":"function","name":"f","content":"x"}]`, "messages[0].role"},
		{"audio input", `[{"role":"user","content":[{"type":"input_audio","input_audio":{}}]}]`, "messages[0].content[0]"},
		{"file", `[{"role":"user","content":[{"type":"file","file":{}}]}]`, "messages[0].content[0]"},
		{"image from the assistant", `[{"role":"assistant","content":[{"type":"image_url","image_url":{"url":"x"}}]}]`,
			"messages[0].content[0]"},
		{"unknown call", `[{"role":"tool","tool_call_id":"c9","content":"x"}]`, "messages[0].tool_call_id"},
		{"function_call", `[{"role":"assistant","function_call":{"name":"f","arguments":"{}"}}]`,
			"messages[0].function_call"},
		{"custom tool call", `[{"role":"assistant","tool_calls":[{"id":"c","type":"custom","custom":{}}]}]`,
			"messages[0].tool_calls[0].type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequest([]byte(`{"model":"m","messages":` + tc.messages + `}`))
			var e *Error
			if !errors.As(err, &e) || e.Param != tc.param {
				t.Errorf("ParseRequest() = %v, want an error about %s", err, tc.param)
			}
		})
	}
	r, err := ParseRequest([]byte(`{"model":"m","messages":[{"role":"assistant","content":null,"refusal":"No."}]}`))
	if err != nil || len(r.Messages) != 1 || r.Messages[0].Content[0].Text != "No." {
		t.Errorf("a refusal = %+v, %v; want its text as content", r, err)
	}
}

// translateError runs to on the request with fields and returns the option
// it rejects, or "" when it translates.
func translateError(t *testing.T, fields string, to func(*Request) error) string {
	t.Helper()
	r, err := ParseRequest(request(fields))
	if err != nil {
		t.Fatalf("ParseRequest(%s): %v", fields, err)
	}
	var e *Error
	if err := to(r); errors.As(err, &e) {
		return e.Param
	} else if err != nil {
		t.Fatalf("translate %s: %v", fields, err)
	}
	return ""
}

func TestAnthropicOptions(t *testing.T) {
	to := func(r *Request) error { _, err := ToAnthropic(r, "claude", 4096, nil); return err }
	for fields, param := range map[string]string{
		`"n":2`:                  "n",
		`"n":1`:                  "",
		`"seed":7`:               "seed",
		`"presence_penalty":0.5`: "presence_penalty",
		`"presence_penalty":0`:   "",
		`"frequency_penalty":1`:  "frequency_penalty",
		`"temperature":1.5`:      "temperature",
		`"temperature":1`:        "",
		`"response_format":{"type":"json_object"}`: "response_format",
		`"reasoning_effort":"minimal"`:             "reasoning_effort",
		`"reasoning_effort":"xhigh"`:               "",
		`"reasoning_effort":"none"`:                "",
	} {
		if got := translateError(t, fields, to); got != param {
			t.Errorf("ToAnthropic(%s) rejects %q, want %q", fields, got, param)
		}
	}
	for messages, param := range map[string]string{
		`[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/bmp;base64,AA=="}}]}]`:          "messages[0].content[0]",
		`[{"role":"user","content":[{"type":"image_url","image_url":{"url":"ftp://x/y.png"}}]}]`:                       "messages[0].content[0]",
		`[{"role":"assistant","tool_calls":[{"id":"c","type":"function","function":{"name":"f","arguments":"[1]"}}]}]`: "messages[0].tool_calls[0].function.arguments",
	} {
		r, err := ParseRequest([]byte(`{"model":"m","messages":` + messages + `}`))
		if err != nil {
			t.Fatal(err)
		}
		var e *Error
		if _, err := ToAnthropic(r, "claude", 4096, nil); !errors.As(err, &e) || e.Param != param {
			t.Errorf("ToAnthropic(%s) = %v, want an error about %s", messages, err, param)
		}
	}

	// The default of max_tokens; reasoning none turns thinking off; one call
	// at a time; an image by URL.
	r, _ := ParseRequest([]byte(`{"model":"m","reasoning_effort":"none","parallel_tool_calls":false,` +
		`"tools":[{"type":"function","function":{"name":"f"}}],"tool_choice":{"type":"function","function":{"name":"f"}},` +
		`"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`))
	out, err := ToAnthropic(r, "claude", 4096, nil)
	for _, want := range []string{`"max_tokens":4096`, `"thinking":{"type":"disabled"}`,
		`"tool_choice":{"disable_parallel_tool_use":true,"name":"f","type":"tool"}`, `"input_schema":{"type":"object"}`,
		`"source":{"type":"url","url":"https://example.com/a.png"}`} {
		if err != nil || !strings.Contains(string(out), want) {
			t.Errorf("ToAnthropic() = %s, %v; want %s", out, err, want)
		}
	}
}

func TestGeminiOptions(t *testing.T) {
	to := func(model string) func(*Request) error {
		return func(r *Request) error { _, err := ToGemini(r, model, nil); return err }
	}
	for _, tc := range []struct{ model, fields, param string }{
		{"gemini-3.7-flash", `"stop":["a","b","c","d","e","f"]`, "stop"},
		{"gemini-3.7-flash", `"parallel_tool_calls":false,"tools":[{"type":"function","function":{"name":"f"}}]`,
			"parallel_tool_calls"},
		{"gemini-3.7-flash", `"reasoning_effort":"none"`, "reasoning_effort"},
		{"gemini-3.7-flash", `"reasoning_effort":"xhigh"`, "reasoning_effort"},
		{"gemini-2.5-flash", `"reasoning_effort":"max"`, "reasoning_effort"},
		{"gemini-2.5-flash", `"reasoning_effort":"none"`, ""},
		{"gemini-3.7-flash", `"tool_choice":"required","tools":[{"type":"function","function":{"name":"f",` +
			`"strict":true}}]`, "tool_choice"},
		{"gemini-3.7-flash", `"n":3,"seed":7,"presence_penalty":0.5,"frequency_penalty":0.5,` +
			`"response_format":{"type":"json_object"}`, ""},
	} {
		if got := translateError(t, tc.fields, to(tc.model)); got != tc.param {
			t.Errorf("ToGemini(%s, %s) rejects %q, want %q", tc.model, tc.fields, got, tc.param)
		}
	}

	for model, want := range map[string]string{
		"gemini-2.5-pro":         `"thinkingConfig":{"thinkingBudget":1024}`,
		"gemini-3.1-pro-preview": `"thinkingConfig":{"thinkingLevel":"LOW"}`,
		"gemini-3-flash-preview": `"thinkingConfig":{"thinkingLevel":"MINIMAL"}`,
	} {
		r, _ := ParseRequest(request(`"reasoning_effort":"minimal"`))
		if out, err := ToGemini(r, model, nil); err != nil || !strings.Contains(string(out), want) {
			t.Errorf("ToGemini(%s) = %s, %v; want %s", model, out, err, want)
		}
	}
	r, _ := ParseRequest(request(`"n":3,"response_format":{"type":"json_object"}`))
	out, _ := ToGemini(r, "gemini-3.7-flash", nil)
	if !strings.Contains(string(out), `"candidateCount":3`) || !strings.Contains(string(out), `"responseMimeType":"application/json"`) {
		t.Errorf("ToGemini() = %s", out)
	}
	r, _ = ParseRequest([]byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url",` +
		`"image_url":{"url":"https://example.com/a.png"}}]}]}`))
	var e *Error
	if _, err := ToGemini(r, "gemini-3.7-flash", nil); !errors.As(err, &e) || e.Param != "messages[0].content[0]" {
		t.Errorf("an image by URL: %v", err)
	}
}

// A call without its thought signature gets it from Memory, or else the
// placeholder that Gemini 3 accepts; Gemini 2.5 needs none.
func TestGeminiSignatures(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","tool_calls":[` +
		`{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}},` +
		`{"id":"c2","type":"function","function":{"name":"f","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"c1","content":"a"},{"role":"tool","tool_call_id":"c2","content":"b"}]}`)
	r, err := ParseRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	signatures := func(model string, mem *Memory) int {
		out, err := ToGemini(r, model, mem)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(out), `"thoughtSignature":"`+SkipSignature+`"`) +
			10*strings.Count(string(out), `"thoughtSignature":"remembered"`)
	}
	if n := signatures("gemini-3.7-flash", nil); n != 1 {
		t.Errorf("Gemini 3 got %d placeholders, want 1 on the first call", n)
	}
	if n := signatures("gemini-2.5-flash", nil); n != 0 {
		t.Errorf("Gemini 2.5 got %d placeholders, want none", n)
	}
	mem := NewMemory()
	mem.put(&memo{ids: []string{"c1"}, signature: "remembered"})
	if n := signatures("gemini-3.7-flash", mem); n != 10 {
		t.Errorf("with Memory: %d; want the remembered signature and no placeholder", n)
	}
}

func TestMemoryLimits(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	mem := NewMemory()
	mem.now = func() time.Time { return now }
	mem.put(&memo{ids: []string{"a"}, signature: "s"})
	if mem.get("a") == nil || mem.get("b") != nil || mem.get("") != nil {
		t.Fatal("get() doesn't find what put() kept")
	}
	now = now.Add(memoryTTL + time.Second)
	if mem.get("a") != nil {
		t.Error("an expired memo is still there")
	}
	big := strings.Repeat("x", memoryMaxBytes/2)
	mem.put(&memo{ids: []string{"b"}, signature: big})
	mem.put(&memo{ids: []string{"c"}, signature: big})
	mem.put(&memo{ids: []string{"d"}, signature: big})
	if mem.get("b") != nil || mem.get("d") == nil || mem.bytes > memoryMaxBytes || mem.byID["a"] != nil {
		t.Errorf("Memory holds %d bytes and %d IDs; want the oldest dropped", mem.bytes, len(mem.byID))
	}
	var none *Memory
	none.put(&memo{ids: []string{"a"}})
	if none.get("a") != nil {
		t.Error("a nil Memory kept something")
	}
}

func TestGeminiBlockedPrompt(t *testing.T) {
	body := []byte(`{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":8},` +
		`"responseId":"r1"}`)
	out, err := FromGemini(body, "gemini-3.7-flash", created, nil)
	if err != nil || !strings.Contains(string(out), `"finish_reason":"content_filter"`) ||
		!strings.Contains(string(out), `"content":null`) {
		t.Errorf("FromGemini() = %s, %v", out, err)
	}
	chunks := NewGeminiStream("gemini-3.7-flash", created, false, nil).Event("", body)
	if len(chunks) != 1 || !strings.Contains(string(chunks[0]), `"finish_reason":"content_filter"`) {
		t.Errorf("stream chunks = %s", chunks)
	}
}

func TestFinishReasons(t *testing.T) {
	for reason, want := range map[string]string{"end_turn": "stop", "stop_sequence": "stop", "max_tokens": "length",
		"model_context_window_exceeded": "length", "tool_use": "tool_calls", "refusal": "content_filter",
		"pause_turn": "stop"} {
		if got := anthropicFinish(reason); got != want {
			t.Errorf("anthropicFinish(%s) = %s, want %s", reason, got, want)
		}
	}
	for _, tc := range []struct {
		reason string
		calls  bool
		want   string
	}{{"STOP", false, "stop"}, {"STOP", true, "tool_calls"}, {"MAX_TOKENS", true, "length"},
		{"SAFETY", false, "content_filter"}, {"MALFORMED_FUNCTION_CALL", false, "stop"}} {
		if got := geminiFinish(tc.reason, tc.calls); got == nil || *got != tc.want {
			t.Errorf("geminiFinish(%s, %v) = %v, want %s", tc.reason, tc.calls, got, tc.want)
		}
	}
	if geminiFinish("", true) != nil {
		t.Error("a chunk without a finish reason finished")
	}
}

func TestOpenAIError(t *testing.T) {
	decode := func(b []byte) (msg, typ string, code any) {
		var e struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    any    `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(b, &e); err != nil {
			t.Fatal(err)
		}
		return e.Error.Message, e.Error.Type, e.Error.Code
	}
	for _, tc := range []struct {
		family   usage.Family
		status   int
		body     string
		msg, typ string
		code     any
	}{
		{usage.Anthropic, 529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
			"Overloaded", "server_error", "overloaded_error"},
		{usage.Gemini, 429, `{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`,
			"Quota exceeded", "rate_limit_error", "RESOURCE_EXHAUSTED"},
		{usage.Gemini, 401, `not JSON`, "The provider answered with status 401.", "authentication_error", nil},
		{usage.Anthropic, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`,
			"bad", "invalid_request_error", "invalid_request_error"},
	} {
		msg, typ, code := decode(OpenAIError(tc.family, tc.status, []byte(tc.body)))
		if msg != tc.msg || typ != tc.typ || code != tc.code {
			t.Errorf("OpenAIError(%s, %d) = %q %q %v", tc.family, tc.status, msg, typ, code)
		}
	}
	if !strings.Contains(string(errorChunk("down", "server_error")), `"error":{`) {
		t.Error("errorChunk() isn't an error")
	}
}

func TestAnthropicStreamError(t *testing.T) {
	s := NewAnthropicStream(created, true, nil)
	chunks := s.Event("error", []byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	if len(chunks) != 1 || !strings.Contains(string(chunks[0]), `"message":"Overloaded","param":null,"type":"server_error"`) {
		t.Errorf("chunks = %s", chunks)
	}
	if s.End() != nil {
		t.Error("a stream that never started reported usage")
	}
}

func FuzzTranslate(f *testing.F) {
	f.Add(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	f.Add(`{"model":"m","messages":[{"role":"assistant","tool_calls":[{"id":"c","type":"function",` +
		`"function":{"name":"f","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c","content":"x"}],` +
		`"tools":[{"type":"function","function":{"name":"f","strict":true}}],"reasoning_effort":"low"}`)
	f.Add(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"f"}}]},"finishReason":"STOP"}]}`)
	f.Add(`{"id":"m","content":[{"type":"tool_use","id":"t","name":"f","input":{}}],"stop_reason":"tool_use"}`)
	mem := NewMemory()
	f.Fuzz(func(t *testing.T, body string) {
		if r, err := ParseRequest([]byte(body)); err == nil {
			for _, out := range [][]byte{mustTranslate(ToAnthropic(r, "claude", 1024, mem)),
				mustTranslate(ToGemini(r, "gemini-3.7-flash", mem))} {
				if out != nil && !json.Valid(out) {
					t.Fatalf("a translation isn't JSON: %s", out)
				}
			}
		}
		for _, out := range [][]byte{mustTranslate(FromAnthropic([]byte(body), created, mem)),
			mustTranslate(FromGemini([]byte(body), "g", created, mem))} {
			if out != nil && !json.Valid(out) {
				t.Fatalf("a translation isn't JSON: %s", out)
			}
		}
		for _, s := range []stream{NewAnthropicStream(created, true, mem), NewGeminiStream("g", created, true, mem)} {
			for _, name := range []string{"message_start", "content_block_start", "content_block_delta", "message_delta",
				"message_stop", "error", ""} {
				for _, chunk := range s.Event(name, []byte(body)) {
					if !json.Valid(chunk) {
						t.Fatalf("a chunk isn't JSON: %s", chunk)
					}
				}
			}
			s.End()
		}
	})
}

func mustTranslate(out []byte, err error) []byte {
	if err != nil {
		return nil
	}
	return out
}
