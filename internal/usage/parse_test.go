package usage

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/testutil"
)

var fakeUsage = testutil.Usage{Input: 1200, Output: 340, CacheRead: 1000, CacheWrite: 150, Reasoning: 120}

// fetch sends body to a fake provider and returns the response body.
func fetch(t *testing.T, url string, header map[string]string, body string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("fake provider: %d %v\n%s", resp.StatusCode, err, data)
	}
	return string(data)
}

// feed passes each event of an SSE stream to s.
func feed(s *Stream, stream string) {
	for block := range strings.SplitSeq(stream, "\n\n") {
		var name, data string
		for line := range strings.SplitSeq(block, "\n") {
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				name = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				data = v
			}
		}
		if data != "" {
			s.Event(name, []byte(data))
		}
	}
}

func TestParseFromFakeProviders(t *testing.T) {
	oa := testutil.NewOpenAI(t, testutil.Config{Usage: fakeUsage})
	an := testutil.NewAnthropic(t, testutil.Config{Usage: fakeUsage})
	openAIWant := Usage{Input: 1200, Output: 340, CacheRead: 1000, CacheWrite: 150, Reasoning: 120}
	// Anthropic's input_tokens excludes cached tokens, so the total adds them.
	anthropicWant := Usage{Input: 1200 + 1000 + 150, Output: 340, CacheRead: 1000, CacheWrite: 150, Reasoning: 120}
	version := map[string]string{"anthropic-version": "2023-06-01"}

	tests := []struct {
		name   string
		family Family
		stream bool
		url    string
		header map[string]string
		body   string
		want   Usage
	}{
		{"openai json", OpenAI, false, oa.URL + "/v1/chat/completions", nil,
			`{"model":"gpt-test","messages":[{}]}`, openAIWant},
		{"openai stream", OpenAI, true, oa.URL + "/v1/chat/completions", nil,
			`{"model":"gpt-test","messages":[{}],"stream":true,"stream_options":{"include_usage":true}}`, openAIWant},
		{"anthropic json", Anthropic, false, an.URL + "/v1/messages", version,
			`{"model":"claude-test","max_tokens":10,"messages":[{}]}`, anthropicWant},
		{"anthropic stream", Anthropic, true, an.URL + "/v1/messages", version,
			`{"model":"claude-test","max_tokens":10,"messages":[{}],"stream":true}`, anthropicWant},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fetch(t, tt.url, tt.header, tt.body)
			var r Report
			if tt.stream {
				s := NewStream(tt.family)
				feed(s, body)
				r = s.Report()
			} else {
				var err error
				if r, err = ParseResponse(tt.family, []byte(body)); err != nil {
					t.Fatal(err)
				}
			}
			if r.Usage == nil || *r.Usage != tt.want || r.Model == "" || r.Modifier != "" {
				t.Errorf("report = %+v (usage %+v), want usage %+v", r, r.Usage, tt.want)
			}
		})
	}
}

func TestOpenAIStreamWithoutUsage(t *testing.T) {
	oa := testutil.NewOpenAI(t, testutil.Config{Usage: fakeUsage})
	body := fetch(t, oa.URL+"/v1/chat/completions", nil, `{"model":"gpt-test","messages":[{}],"stream":true}`)
	s := NewStream(OpenAI)
	feed(s, body)
	if r := s.Report(); r.Usage != nil || r.Model != "gpt-test" {
		t.Errorf("report = %+v, want the model and no usage", r)
	}
}

func TestParseResponseDetails(t *testing.T) {
	tests := []struct {
		name   string
		family Family
		body   string
		want   Report
	}{
		{"openai fast tier", OpenAI, `{"model":"m","service_tier":"fast","usage":{"prompt_tokens":5,"completion_tokens":1}}`,
			Report{Model: "m", Usage: &Usage{Input: 5, Output: 1}, Modifier: "service tier fast"}},
		{"openai default tier", OpenAI, `{"model":"m","service_tier":"default","usage":{"prompt_tokens":5,
			"completion_tokens":1,"prompt_tokens_details":null}}`, Report{Model: "m", Usage: &Usage{Input: 5, Output: 1}}},
		{"openai no usage", OpenAI, `{"model":"m"}`, Report{Model: "m"}},
		{"anthropic cache breakdown", Anthropic, `{"model":"c","usage":{"input_tokens":10,"output_tokens":3,
			"cache_creation_input_tokens":300,"cache_read_input_tokens":0,
			"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":200}}}`,
			Report{Model: "c", Usage: &Usage{Input: 310, Output: 3, CacheWrite: 300, CacheWrite1h: 200}}},
		{"anthropic priority", Anthropic, `{"model":"c","usage":{"input_tokens":1,"output_tokens":1,"service_tier":"priority"}}`,
			Report{Model: "c", Usage: &Usage{Input: 1, Output: 1}, Modifier: "service tier priority"}},
		{"anthropic fast", Anthropic, `{"model":"c","usage":{"input_tokens":1,"output_tokens":1,"speed":"fast",
			"service_tier":"standard"}}`, Report{Model: "c", Usage: &Usage{Input: 1, Output: 1}, Modifier: "fast mode"}},
		{"anthropic us inference", Anthropic, `{"model":"c","usage":{"input_tokens":1,"output_tokens":1,"inference_geo":"us"}}`,
			Report{Model: "c", Usage: &Usage{Input: 1, Output: 1}, Modifier: "US-only inference"}},
		{"anthropic global inference", Anthropic, `{"model":"c","usage":{"input_tokens":1,"output_tokens":1,
			"inference_geo":"global","speed":"standard"}}`, Report{Model: "c", Usage: &Usage{Input: 1, Output: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResponse(tt.family, []byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseResponse() = %+v (usage %+v), want %+v (usage %+v)", got, got.Usage, tt.want, tt.want.Usage)
			}
		})
	}
	for _, f := range []Family{OpenAI, Anthropic, "gemini"} {
		if _, err := ParseResponse(f, []byte("{")); err == nil {
			t.Errorf("ParseResponse(%s) of bad JSON succeeded", f)
		}
	}
}

func TestAnthropicStreamCumulativeCounts(t *testing.T) {
	s := NewStream(Anthropic)
	s.Event("message_start", []byte(`{"type":"message_start","message":{"model":"c","usage":{"input_tokens":10,
		"cache_read_input_tokens":5,"cache_creation_input_tokens":0,"output_tokens":1}}}`))
	s.Event("content_block_delta", []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`))
	s.Event("message_delta", []byte(`{"type":"message_delta","usage":{"output_tokens":7}}`))
	s.Event("message_delta", []byte(`{"type":"message_delta","usage":{"output_tokens":15,"input_tokens":12,
		"output_tokens_details":{"thinking_tokens":4}}}`))
	s.Event("message_delta", []byte(`not JSON`))
	want := Usage{Input: 12 + 5, Output: 15, CacheRead: 5, Reasoning: 4}
	if r := s.Report(); r.Usage == nil || *r.Usage != want || r.Model != "c" {
		t.Errorf("report = %+v (usage %+v), want %+v", r, r.Usage, want)
	}

	// A stream whose only usage is in message_delta still reports it.
	s = NewStream(Anthropic)
	s.Event("message_delta", []byte(`{"usage":{"output_tokens":2}}`))
	if r := s.Report(); r.Usage == nil || r.Usage.Output != 2 {
		t.Errorf("report = %+v", r)
	}
	if r := NewStream(Anthropic).Report(); r.Usage != nil {
		t.Errorf("empty stream report = %+v, want no usage", r)
	}
}

func FuzzParseResponse(f *testing.F) {
	f.Add(`{"model":"m","usage":{"prompt_tokens":1,"completion_tokens":2}}`)
	f.Add(`{"model":"c","usage":{"input_tokens":1,"cache_creation":{"ephemeral_1h_input_tokens":3}}}`)
	f.Fuzz(func(_ *testing.T, body string) {
		for _, fam := range []Family{OpenAI, Anthropic} {
			_, _ = ParseResponse(fam, []byte(body))
			s := NewStream(fam)
			for _, name := range []string{"", "message_start", "message_delta"} {
				s.Event(name, []byte(body))
			}
			_ = s.Report()
		}
	})
}
