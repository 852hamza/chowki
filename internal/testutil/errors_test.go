package testutil

import (
	"io"
	"strings"
	"testing"
)

func TestErrors(t *testing.T) {
	const (
		openAIBody    = `{"model":"m","messages":[{}]}`
		anthropicBody = `{"model":"m","max_tokens":10,"messages":[{}]}`
		geminiBody    = `{"contents":[{}]}`
		generate      = "/v1beta/models/gemini-test:generateContent"
	)
	version := map[string]string{"anthropic-version": anthropicVersion}
	tests := []struct {
		name       string
		start      func(testing.TB, Config) *Server
		cfg        Config
		path       string
		header     map[string]string
		body       string
		wantStatus int
		wantBody   []string
	}{
		{"openai wrong key", NewOpenAI, Config{APIKey: "sk-right"}, "/v1/chat/completions",
			map[string]string{"Authorization": "Bearer sk-wrong"}, openAIBody, 401,
			[]string{`"type":"invalid_request_error"`, `"param":null`, `"code":null`}},
		{"openai missing key", NewOpenAI, Config{APIKey: "sk-right"}, "/v1/chat/completions", nil, openAIBody, 401,
			[]string{`"message":"Incorrect API key provided."`}},
		{"openai missing model", NewOpenAI, Config{}, "/v1/chat/completions", nil, `{"messages":[{}]}`, 400,
			[]string{`"param":"model"`}},
		{"openai missing messages", NewOpenAI, Config{}, "/v1/chat/completions", nil, `{"model":"m"}`, 400,
			[]string{`"param":"messages"`}},
		{"openai invalid JSON", NewOpenAI, Config{}, "/v1/chat/completions", nil, `{`, 400,
			[]string{`invalid JSON body`}},
		{"openai failure", NewOpenAI, Config{FailStatus: 503}, "/v1/chat/completions", nil, openAIBody, 503,
			[]string{`"type":"server_error"`}},
		{"openai unknown path", NewOpenAI, Config{}, "/v1/unknown", nil, `{}`, 404,
			[]string{`unknown path /v1/unknown`}},

		{"anthropic wrong key", NewAnthropic, Config{APIKey: "sk-ant-right"}, "/v1/messages",
			map[string]string{"x-api-key": "sk-ant-wrong", "anthropic-version": anthropicVersion}, anthropicBody, 401,
			[]string{`"type":"error"`, `"type":"authentication_error"`, `"request_id":"req_fake`}},
		{"anthropic missing version", NewAnthropic, Config{}, "/v1/messages", nil, anthropicBody, 400,
			[]string{`"type":"invalid_request_error"`, `anthropic-version: header is required`}},
		{"anthropic missing max_tokens", NewAnthropic, Config{}, "/v1/messages", version,
			`{"model":"m","messages":[{}]}`, 400, []string{`max_tokens: Field required`}},
		{"anthropic missing model", NewAnthropic, Config{}, "/v1/messages", version,
			`{"max_tokens":10,"messages":[{}]}`, 400, []string{`model: Field required`}},
		{"anthropic overloaded", NewAnthropic, Config{FailStatus: 529}, "/v1/messages", version, anthropicBody, 529,
			[]string{`"type":"overloaded_error"`}},
		{"anthropic unlisted 4xx", NewAnthropic, Config{FailStatus: 422}, "/v1/messages", version, anthropicBody, 422,
			[]string{`"type":"invalid_request_error"`}},

		{"gemini wrong key", NewGemini, Config{APIKey: "right"}, generate,
			map[string]string{"x-goog-api-key": "wrong"}, geminiBody, 401,
			[]string{`"code":401`, `"status":"UNAUTHENTICATED"`}},
		{"gemini key in URL", NewGemini, Config{APIKey: "right"}, generate + "?key=right", nil, geminiBody, 400,
			[]string{`"status":"INVALID_ARGUMENT"`, `x-goog-api-key`}},
		{"gemini stream without alt=sse", NewGemini, Config{}, "/v1beta/models/gemini-test:streamGenerateContent",
			nil, geminiBody, 400, []string{`alt=sse`}},
		{"gemini unknown method", NewGemini, Config{}, "/v1beta/models/gemini-test:countTokens", nil, geminiBody, 404,
			[]string{`"status":"NOT_FOUND"`}},
		{"gemini missing contents", NewGemini, Config{}, generate, nil, `{}`, 400,
			[]string{`contents is not specified`}},
		{"gemini unavailable", NewGemini, Config{FailStatus: 503}, generate, nil, geminiBody, 503,
			[]string{`"code":503`, `"status":"UNAVAILABLE"`}},
		{"gemini unmapped status", NewGemini, Config{FailStatus: 418}, generate, nil, geminiBody, 418,
			[]string{`"status":"UNKNOWN"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.start(t, tt.cfg)
			resp := post(t.Context(), t, s.URL+tt.path, tt.header, tt.body)
			checkResponse(t, resp, tt.wantStatus, "application/json")
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.wantBody {
				if !strings.Contains(string(body), w) {
					t.Errorf("body doesn't contain %s:\n%s", w, body)
				}
			}
		})
	}
}
