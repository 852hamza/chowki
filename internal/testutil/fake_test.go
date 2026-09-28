package testutil

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testUsage has a different value in every field, so a mixed-up field shows.
var testUsage = Usage{Input: 1200, Output: 340, CacheRead: 1000, CacheWrite: 150, Reasoning: 120}

const anthropicVersion = "2023-06-01"

// post sends a JSON POST request and returns the response. The body is closed
// when the test ends.
func post(ctx context.Context, t *testing.T, url string, header map[string]string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func checkResponse(t *testing.T, resp *http.Response, status int, contentType string) {
	t.Helper()
	if resp.StatusCode != status {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, status, body)
	}
	if got := resp.Header.Get("Content-Type"); got != contentType {
		t.Errorf("Content-Type = %q, want %q", got, contentType)
	}
}

func decode(t *testing.T, r io.Reader, v any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

// event is one server-sent event.
type event struct{ name, data string }

// readEvent reads the next event. It returns io.EOF at the end of the stream.
func readEvent(br *bufio.Reader) (event, error) {
	var ev event
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && (ev != event{}) {
				return ev, io.ErrUnexpectedEOF
			}
			return ev, err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if ev != (event{}) {
				return ev, nil
			}
		case strings.HasPrefix(line, "event: "):
			ev.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			ev.data = strings.TrimPrefix(line, "data: ")
		default:
			return ev, fmt.Errorf("unexpected SSE line %q", line)
		}
	}
}

func readEvents(t *testing.T, r io.Reader) []event {
	t.Helper()
	br := bufio.NewReader(r)
	var events []event
	for {
		ev, err := readEvent(br)
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatalf("read SSE stream: %v", err)
		}
		events = append(events, ev)
	}
}

func TestPieces(t *testing.T) {
	tests := []struct {
		text   string
		chunks int
		want   []string
	}{
		{"abcdefg", 3, []string{"ab", "cd", "efg"}},
		{"abc", 5, []string{"a", "b", "c"}},
		{"héllo", 2, []string{"hé", "llo"}},
		{"", 0, []string{"Hello fro", "m the fake", " provider."}},
	}
	for _, tt := range tests {
		got := Config{Text: tt.text, Chunks: tt.chunks}.pieces()
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("pieces(%q, %d) = %q, want %q", tt.text, tt.chunks, got, tt.want)
		}
	}
}

// fatalRecorder records a Fatalf call instead of failing the test.
type fatalRecorder struct {
	testing.TB
	msg string
}

func (f *fatalRecorder) Helper() {}

func (f *fatalRecorder) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func TestInvalidConfigFailsTheTest(t *testing.T) {
	tests := []struct {
		name  string
		start func(testing.TB, Config) *Server
		cfg   Config
		want  string
	}{
		{"negative usage", NewOpenAI, Config{Usage: Usage{Output: -1}}, "must not be negative"},
		{"negative chunks", NewAnthropic, Config{Chunks: -1}, "chunks must not be negative"},
		{"fail status not an error", NewOpenAI, Config{FailStatus: 302}, "not an HTTP error status"},
		{"cache write on Gemini", NewGemini, Config{Usage: Usage{CacheWrite: 1}}, "CacheWrite must be 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &fatalRecorder{TB: t}
			done := make(chan struct{})
			go func() {
				defer close(done)
				tt.start(rec, tt.cfg)
			}()
			<-done
			if !strings.Contains(rec.msg, tt.want) {
				t.Errorf("Fatalf message = %q, want it to contain %q", rec.msg, tt.want)
			}
		})
	}
}

func TestRequestsAreRecorded(t *testing.T) {
	s := NewOpenAI(t, Config{})
	body := `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`
	resp := post(t.Context(), t, s.URL+"/v1/chat/completions?trace=1", map[string]string{
		"Authorization": "Bearer sk-EXAMPLE",
		"X-Test":        "yes",
	}, body)
	checkResponse(t, resp, http.StatusOK, "application/json")

	reqs := s.Requests()
	if len(reqs) != 1 {
		t.Fatalf("Requests() returned %d requests, want 1", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodPost || r.Path != "/v1/chat/completions" || r.Query.Get("trace") != "1" ||
		r.Header.Get("X-Test") != "yes" || r.Header.Get("Authorization") != "Bearer sk-EXAMPLE" || string(r.Body) != body {
		t.Errorf("recorded request = %+v", r)
	}
}

// TestStreamsFlushEachChunk sets an hour-long delay between chunks: the first
// text piece can only arrive in time if the fake flushes it on its own.
func TestStreamsFlushEachChunk(t *testing.T) {
	cfg := Config{Text: "first second", Chunks: 2, ChunkDelay: time.Hour}
	tests := []struct {
		name   string
		start  func(testing.TB, Config) *Server
		path   string
		header map[string]string
		body   string
		text   func(t *testing.T, ev event) string // text piece in ev, if any
	}{
		{
			name:  "openai",
			start: NewOpenAI,
			path:  "/v1/chat/completions",
			body:  `{"model":"m","messages":[{}],"stream":true}`,
			text: func(t *testing.T, ev event) string {
				var c openAIChunk
				decode(t, strings.NewReader(ev.data), &c)
				return c.Choices[0].Delta.Content
			},
		},
		{
			name:   "anthropic",
			start:  NewAnthropic,
			path:   "/v1/messages",
			header: map[string]string{"anthropic-version": anthropicVersion},
			body:   `{"model":"m","max_tokens":10,"messages":[{}],"stream":true}`,
			text: func(t *testing.T, ev event) string {
				var d anthropicDelta
				if ev.name == "content_block_delta" {
					decode(t, strings.NewReader(ev.data), &d)
				}
				return d.Delta.Text
			},
		},
		{
			name:  "gemini",
			start: NewGemini,
			path:  "/v1beta/models/gemini-test:streamGenerateContent?alt=sse",
			body:  `{"contents":[{}]}`,
			text: func(t *testing.T, ev event) string {
				var r geminiResponseJSON
				decode(t, strings.NewReader(ev.data), &r)
				return r.Candidates[0].Content.Parts[0].Text
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.start(t, cfg)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			resp := post(ctx, t, s.URL+tt.path, tt.header, tt.body)
			checkResponse(t, resp, http.StatusOK, "text/event-stream")
			br := bufio.NewReader(resp.Body)
			for {
				ev, err := readEvent(br)
				if err != nil {
					t.Fatalf("first piece didn't arrive: %v", err)
				}
				if got := tt.text(t, ev); got != "" {
					if got != "first " {
						t.Errorf("first piece = %q, want %q", got, "first ")
					}
					return // the deferred cancel ends the stream
				}
			}
		})
	}
}

// Each fake answers with a tool call in its format, as JSON and as a
// stream whose pieces add up to the arguments.
func TestToolCalls(t *testing.T) {
	cfg := Config{ToolCall: &ToolCall{Name: "get_weather", Arguments: `{"city":"Paris"}`}, Chunks: 3}
	oa, an, gm := NewOpenAI(t, cfg), NewAnthropic(t, cfg), NewGemini(t, cfg)
	version := map[string]string{"anthropic-version": "2023-06-01"}
	for _, tc := range []struct {
		name, url string
		header    map[string]string
		body      string
		want      []string
	}{
		{"openai", oa.URL + "/v1/chat/completions", nil, `{"model":"m","messages":[{}]}`,
			[]string{`"finish_reason":"tool_calls"`, `"arguments":"{\"city\":\"Paris\"}"`, `"content":null`}},
		{"openai stream", oa.URL + "/v1/chat/completions", nil, `{"model":"m","messages":[{}],"stream":true}`,
			[]string{`"arguments":"","name":"get_weather"`, `"arguments":"{\"cit"`, `"finish_reason":"tool_calls"`}},
		{"anthropic", an.URL + "/v1/messages", version, `{"model":"m","max_tokens":9,"messages":[{}]}`,
			[]string{`"type":"tool_use"`, `"input":{"city":"Paris"}`, `"stop_reason":"tool_use"`}},
		{"anthropic stream", an.URL + "/v1/messages", version,
			`{"model":"m","max_tokens":9,"messages":[{}],"stream":true}`,
			[]string{`"type":"tool_use"`, `"partial_json":"{\"cit"`, `"stop_reason":"tool_use"`}},
		{"gemini", gm.URL + "/v1beta/models/g:generateContent", nil, `{"contents":[{}]}`,
			[]string{`"functionCall":{"args":{"city":"Paris"},"name":"get_weather"}`,
				`"thoughtSignature":"` + FakeSignature + `"`}},
		{"gemini stream", gm.URL + "/v1beta/models/g:streamGenerateContent?alt=sse", nil, `{"contents":[{}]}`,
			[]string{`"functionCall":{"args":{"city":"Paris"},"name":"get_weather"}`, `"finishReason":"STOP"`}},
	} {
		resp := post(t.Context(), t, tc.url, tc.header, tc.body)
		b, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %v", tc.name, resp.StatusCode, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(string(b), want) {
				t.Errorf("%s: the reply lacks %s:\n%s", tc.name, want, b)
			}
		}
	}
	if err := (Config{ToolCall: &ToolCall{Arguments: "{"}}).validate(); err == nil {
		t.Error("validate() accepted arguments that aren't JSON")
	}
}
