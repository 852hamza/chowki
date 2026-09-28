package pipeline_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/sse"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

// conformanceCase is a scenario that every adapter and every translation
// path must handle alike.
type conformanceCase struct {
	name    string
	stream  bool
	tools   bool // the request offers the get_weather tool
	history bool // the request carries an earlier tool call and its result
	image   bool // the request has an image
	call    bool // the provider answers with a call of get_weather
	fail    int  // the provider fails with this status
}

var conformanceCases = []conformanceCase{
	{name: "text"},
	{name: "text stream", stream: true},
	{name: "tool call", tools: true, call: true},
	{name: "tool call stream", tools: true, call: true, stream: true},
	{name: "tool result", tools: true, history: true},
	{name: "tool result stream", tools: true, history: true, stream: true},
	{name: "image", image: true},
	{name: "provider error", fail: http.StatusServiceUnavailable},
	{name: "provider error stream", fail: http.StatusServiceUnavailable, stream: true},
}

const (
	conformanceText = "Paris is warm."
	pixel           = "iVBORw0KGgo="
)

var conformanceUsage = testutil.Usage{Input: 100, Output: 20}

// answer is what a client got, read in its format.
type answer struct {
	status int
	text   string
	call   string // the name and the arguments of a tool call, as name(arguments)
	err    string // the error message
}

// conformancePath is a way through the gateway: a client's API format and
// the provider it reaches.
type conformancePath struct {
	name     string
	path     func(c conformanceCase) string
	request  func(c conformanceCase) string
	read     func(t *testing.T, c conformanceCase, body string) answer
	upstream func(h *harness) []testutil.Request
	// sent lists what the provider must find in the request, by case.
	sent func(c conformanceCase) []string
}

var conformancePaths = []conformancePath{
	{"openai adapter", openAIPath, openAIRequest("gpt-test"), readOpenAI,
		func(h *harness) []testutil.Request { return h.openai.Requests() }, sentOpenAI},
	{"anthropic adapter", func(conformanceCase) string { return "/anthropic/v1/messages" }, anthropicRequest,
		readAnthropic, func(h *harness) []testutil.Request { return h.anthropic.Requests() }, sentAnthropic},
	{"gemini adapter", geminiPath, geminiRequest, readGemini,
		func(h *harness) []testutil.Request { return h.gemini.Requests() }, sentGemini},
	{"openai to anthropic", openAIPath, openAIRequest("anthropic/claude-test"), readOpenAI,
		func(h *harness) []testutil.Request { return h.anthropic.Requests() }, sentAnthropic},
	{"openai to gemini", openAIPath, openAIRequest("gemini/gemini-test"), readOpenAI,
		func(h *harness) []testutil.Request { return h.gemini.Requests() }, sentGemini},
}

// AC: the same table passes for the openai, anthropic and gemini adapters
// and for both translation paths.
func TestConformance(t *testing.T) {
	for _, p := range conformancePaths {
		for _, c := range conformanceCases {
			t.Run(p.name+"/"+c.name, func(t *testing.T) {
				cfg := testutil.Config{Text: conformanceText, Usage: conformanceUsage, FailStatus: c.fail}
				if c.call {
					cfg.ToolCall = &testutil.ToolCall{Name: "get_weather", Arguments: weatherArgs}
				}
				h := newHarness(t, cfg, cfg, withGemini(cfg))
				resp := h.post(t.Context(), p.path(c), p.request(c))
				got := p.read(t, c, readBody(t, resp))
				got.status = resp.StatusCode

				want := answer{status: http.StatusOK, text: conformanceText}
				switch {
				case c.fail != 0:
					want = answer{status: c.fail, err: "fake provider failure"}
				case c.call:
					want = answer{status: http.StatusOK, call: "get_weather(" + weatherArgs + ")"}
				}
				if got != want {
					t.Errorf("the client got %+v, want %+v", got, want)
				}
				sent := p.upstream(h)
				if len(sent) != 1 {
					t.Fatalf("the provider got %d requests, want 1", len(sent))
				}
				for _, part := range p.sent(c) {
					if !strings.Contains(string(sent[0].Body), part) {
						t.Errorf("the provider's request lacks %s:\n%s", part, sent[0].Body)
					}
				}
				recs := h.records()
				if len(recs) != 1 {
					t.Fatalf("saved %d records, want 1", len(recs))
				}
				r := recs[0]
				if c.fail != 0 {
					if r.ErrorType != fmt.Sprintf("upstream_%d", c.fail) || r.Tokens != nil {
						t.Errorf("record = %+v, want the provider's failure", r)
					}
					return
				}
				if r.Tokens == nil || *r.Tokens != (store.Tokens{Input: 100, Output: 20}) || r.CostUSD == nil ||
					*r.CostUSD <= 0 || r.ErrorType != "" {
					t.Errorf("record = %+v, tokens %+v; want the usage priced", r, r.Tokens)
				}
			})
		}
	}
}

// Requests in each format. Tool results answer an earlier call c1 of
// get_weather with {"temp_c":21}.

func openAIPath(conformanceCase) string { return "/v1/chat/completions" }

func openAIRequest(model string) func(c conformanceCase) string {
	return func(c conformanceCase) string {
		content := `"Is Paris warm?"`
		if c.image {
			content = `[{"type":"text","text":"Is Paris warm?"},` +
				`{"type":"image_url","image_url":{"url":"data:image/png;base64,` + pixel + `"}}]`
		}
		messages := `{"role":"user","content":` + content + `}`
		if c.history {
			messages += `,{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":` +
				`{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},` +
				`{"role":"tool","tool_call_id":"c1","content":"{\"temp_c\":21}"}`
		}
		body := `{"model":"` + model + `","messages":[` + messages + `]`
		if c.tools {
			body += `,"tools":[{"type":"function","function":{"name":"get_weather","parameters":` +
				`{"type":"object","properties":{"city":{"type":"string"}}}}}]`
		}
		if c.stream {
			body += `,"stream":true,"stream_options":{"include_usage":true}`
		}
		return body + "}"
	}
}

func anthropicRequest(c conformanceCase) string {
	content := `"Is Paris warm?"`
	if c.image {
		content = `[{"type":"text","text":"Is Paris warm?"},{"type":"image","source":{"type":"base64",` +
			`"media_type":"image/png","data":"` + pixel + `"}}]`
	}
	messages := `{"role":"user","content":` + content + `}`
	if c.history {
		messages += `,{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"get_weather",` +
			`"input":{"city":"Paris"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1",` +
			`"content":"{\"temp_c\":21}"}]}`
	}
	body := `{"model":"claude-test","max_tokens":64,"messages":[` + messages + `]`
	if c.tools {
		body += `,"tools":[{"name":"get_weather","input_schema":{"type":"object","properties":` +
			`{"city":{"type":"string"}}}}]`
	}
	if c.stream {
		body += `,"stream":true`
	}
	return body + "}"
}

func geminiPath(c conformanceCase) string {
	if c.stream {
		return "/gemini/v1beta/models/gemini-test:streamGenerateContent?alt=sse"
	}
	return "/gemini/v1beta/models/gemini-test:generateContent"
}

func geminiRequest(c conformanceCase) string {
	parts := `{"text":"Is Paris warm?"}`
	if c.image {
		parts += `,{"inlineData":{"mimeType":"image/png","data":"` + pixel + `"}}`
	}
	contents := `{"role":"user","parts":[` + parts + `]}`
	if c.history {
		contents += `,{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"Paris"}},` +
			`"thoughtSignature":"` + testutil.FakeSignature + `"}]},{"role":"user","parts":[{"functionResponse":` +
			`{"name":"get_weather","response":{"temp_c":21}}}]}`
	}
	body := `{"contents":[` + contents + `]`
	if c.tools {
		body += `,"tools":[{"functionDeclarations":[{"name":"get_weather","parametersJsonSchema":` +
			`{"type":"object","properties":{"city":{"type":"string"}}}}]}]`
	}
	return body + "}"
}

// What each provider must receive: the image and the tool result in its
// format.

func sentOpenAI(c conformanceCase) []string {
	var out []string
	if c.image {
		out = append(out, `"url":"data:image/png;base64,`+pixel+`"`)
	}
	if c.history {
		out = append(out, `"tool_call_id":"c1"`)
	}
	return out
}

func sentAnthropic(c conformanceCase) []string {
	var out []string
	if c.image {
		out = append(out, `"data":"`+pixel+`"`, `"media_type":"image/png"`)
	}
	if c.history {
		out = append(out, `"tool_use_id":"c1"`, `"type":"tool_result"`)
	}
	return out
}

func sentGemini(c conformanceCase) []string {
	var out []string
	if c.image {
		out = append(out, `"inlineData":{"`, pixel)
	}
	if c.history {
		out = append(out, `"functionResponse":{"`, `"thoughtSignature":"`)
	}
	return out
}

// Answers in each format, JSON or streamed.

func eachEvent(body string, f func(name string, data []byte)) {
	r := sse.NewReader(strings.NewReader(body))
	for {
		ev, err := r.Next()
		if err != nil {
			return
		}
		f(ev.Name, ev.Data)
	}
}

func readOpenAI(t *testing.T, c conformanceCase, body string) answer {
	t.Helper()
	var a answer
	type call struct {
		Function struct{ Name, Arguments string } `json:"function"`
	}
	var e struct {
		Error *struct{ Message string } `json:"error"`
	}
	if json.Unmarshal([]byte(body), &e) == nil && e.Error != nil {
		return answer{err: e.Error.Message}
	}
	if !c.stream {
		var r struct {
			Choices []struct {
				Message struct {
					Content   *string `json:"content"`
					ToolCalls []call  `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
			Usage *json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(body), &r); err != nil || len(r.Choices) != 1 || r.Usage == nil {
			t.Fatalf("not a chat completion with usage: %v\n%s", err, body)
		}
		m := r.Choices[0].Message
		if m.Content != nil {
			a.text = *m.Content
		}
		for _, tc := range m.ToolCalls {
			a.call += tc.Function.Name + "(" + tc.Function.Arguments + ")"
		}
		return a
	}
	var name, args strings.Builder
	usage, done := false, false
	eachEvent(body, func(_ string, data []byte) {
		if string(data) == "[DONE]" {
			done = true
			return
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   *string `json:"content"`
					ToolCalls []call  `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			t.Errorf("a chunk isn't JSON: %s", data)
			return
		}
		usage = usage || chunk.Usage != nil && string(*chunk.Usage) != "null"
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != nil {
				a.text += *ch.Delta.Content
			}
			for _, tc := range ch.Delta.ToolCalls {
				name.WriteString(tc.Function.Name)
				args.WriteString(tc.Function.Arguments)
			}
		}
	})
	if name.Len() > 0 {
		a.call = name.String() + "(" + args.String() + ")"
	}
	if !done || !usage {
		t.Errorf("the stream ended without [DONE] or usage: %v, %v", done, usage)
	}
	return a
}

func readAnthropic(t *testing.T, c conformanceCase, body string) answer {
	t.Helper()
	var e struct {
		Type  string                    `json:"type"`
		Error *struct{ Message string } `json:"error"`
	}
	if json.Unmarshal([]byte(body), &e) == nil && e.Type == "error" && e.Error != nil {
		return answer{err: e.Error.Message}
	}
	var a answer
	if !c.stream {
		var r struct {
			Content []struct {
				Type, Text, Name string
				Input            json.RawMessage
			} `json:"content"`
		}
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("not a message: %v\n%s", err, body)
		}
		for _, b := range r.Content {
			switch b.Type {
			case "text":
				a.text += b.Text
			case "tool_use":
				a.call += b.Name + "(" + string(b.Input) + ")"
			}
		}
		return a
	}
	var name, args strings.Builder
	eachEvent(body, func(event string, data []byte) {
		var ev struct {
			ContentBlock struct{ Type, Name string } `json:"content_block"`
			Delta        struct {
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		_ = json.Unmarshal(data, &ev)
		switch event {
		case "content_block_start":
			name.WriteString(ev.ContentBlock.Name)
		case "content_block_delta":
			a.text += ev.Delta.Text
			args.WriteString(ev.Delta.PartialJSON)
		}
	})
	if name.Len() > 0 {
		a.call = name.String() + "(" + args.String() + ")"
	}
	return a
}

func readGemini(t *testing.T, c conformanceCase, body string) answer {
	t.Helper()
	var e struct {
		Error *struct{ Message string } `json:"error"`
	}
	if json.Unmarshal([]byte(body), &e) == nil && e.Error != nil {
		return answer{err: e.Error.Message}
	}
	var a answer
	read := func(data []byte) {
		var r struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text         string
						FunctionCall *struct {
							Name string
							Args json.RawMessage
						} `json:"functionCall"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			t.Errorf("not a Gemini response: %v\n%s", err, data)
			return
		}
		for _, cand := range r.Candidates {
			for _, p := range cand.Content.Parts {
				a.text += p.Text
				if f := p.FunctionCall; f != nil {
					a.call += f.Name + "(" + string(f.Args) + ")"
				}
			}
		}
	}
	if !c.stream {
		read([]byte(body))
		return a
	}
	eachEvent(body, func(_ string, data []byte) { read(data) })
	return a
}
