package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

// NewOpenAI starts a fake OpenAI-compatible API. It serves
// POST /v1/chat/completions as JSON, or as an SSE stream when the request
// sets "stream": true, and expects the key in "Authorization: Bearer".
//
// As in the real API, a stream carries usage only when the request sets
// stream_options.include_usage: every chunk then has "usage": null, and an
// extra chunk with empty choices and the usage comes before "data: [DONE]".
//
// Format: https://github.com/openai/openai-openapi (CreateChatCompletionResponse,
// CreateChatCompletionStreamResponse, ChatCompletionStreamOptions,
// CompletionUsage, ErrorResponse).
func NewOpenAI(t testing.TB, cfg Config) *Server {
	t.Helper()
	return start(t, cfg, func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("POST /v1/chat/completions", s.openAIChat)
		mux.HandleFunc("POST /v1/embeddings", s.openAIEmbeddings)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			openAIError(w, http.StatusNotFound, "", "unknown path "+r.URL.Path)
		})
	})
}

type openAIRequest struct {
	Model         string            `json:"model"`
	Messages      []json.RawMessage `json:"messages"`
	Stream        bool              `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

// openAIEmbeddings answers with one small embedding per request, and
// reports Usage.Input as its prompt tokens.
func (s *Server) openAIEmbeddings(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(bearer(r)) {
		openAIError(w, http.StatusUnauthorized, "", "Incorrect API key provided.")
		return
	}
	if s.cfg.FailStatus != 0 {
		openAIError(w, s.cfg.FailStatus, "", "fake provider failure")
		return
	}
	var req struct {
		Model string          `json:"model"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" || len(req.Input) == 0 {
		openAIError(w, http.StatusBadRequest, "input", "model and input are required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{0.25, -0.5, 0.75}}},
		"model":  req.Model,
		"usage":  map[string]int{"prompt_tokens": s.cfg.Usage.Input, "total_tokens": s.cfg.Usage.Input},
	})
}

func (s *Server) openAIChat(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(bearer(r)) {
		openAIError(w, http.StatusUnauthorized, "", "Incorrect API key provided.")
		return
	}
	if s.cfg.FailStatus != 0 {
		openAIError(w, s.cfg.FailStatus, "", "fake provider failure")
		return
	}
	var req openAIRequest
	switch err := json.NewDecoder(r.Body).Decode(&req); {
	case err != nil:
		openAIError(w, http.StatusBadRequest, "", "invalid JSON body: "+err.Error())
		return
	case req.Model == "":
		openAIError(w, http.StatusBadRequest, "model", "you must provide a model parameter")
		return
	case len(req.Messages) == 0:
		openAIError(w, http.StatusBadRequest, "messages", "messages must not be empty")
		return
	}

	id := s.id("chatcmpl-fake")
	if !req.Stream {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      id,
			"object":  "chat.completion",
			"created": fakeCreated,
			"model":   req.Model,
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": s.cfg.text(), "refusal": nil},
				"logprobs":      nil,
				"finish_reason": "stop",
			}},
			"usage": openAIUsage(s.cfg.Usage),
		})
		return
	}

	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	chunk := func(choices []any) map[string]any {
		c := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": fakeCreated,
			"model":   req.Model,
			"choices": choices,
		}
		if includeUsage {
			c["usage"] = nil
		}
		return c
	}
	choice := func(delta map[string]any, finish any) []any {
		return []any{map[string]any{"index": 0, "delta": delta, "logprobs": nil, "finish_reason": finish}}
	}

	sse := newSSE(w, r, s.cfg.ChunkDelay)
	if !sse.send("", chunk(choice(map[string]any{"role": "assistant", "content": ""}, nil))) {
		return
	}
	for i, piece := range s.cfg.pieces() {
		if i > 0 && !sse.pause() || !sse.send("", chunk(choice(map[string]any{"content": piece}, nil))) {
			return
		}
	}
	if !sse.send("", chunk(choice(map[string]any{}, "stop"))) {
		return
	}
	if includeUsage {
		final := chunk([]any{})
		final["usage"] = openAIUsage(s.cfg.Usage)
		if !sse.send("", final) {
			return
		}
	}
	sse.write("", "[DONE]")
}

func openAIUsage(u Usage) map[string]any {
	prompt := map[string]any{"cached_tokens": u.CacheRead}
	if u.CacheWrite != 0 {
		prompt["cache_write_tokens"] = u.CacheWrite
	}
	return map[string]any{
		"prompt_tokens":             u.Input,
		"completion_tokens":         u.Output,
		"total_tokens":              u.Input + u.Output,
		"prompt_tokens_details":     prompt,
		"completion_tokens_details": map[string]any{"reasoning_tokens": u.Reasoning},
	}
}

// openAIError writes an ErrorResponse. The spec documents the shape but not
// the type and code values; invalid_request_error appears in OpenAI's error
// guide, and server_error for 5xx is an assumption.
func openAIError(w http.ResponseWriter, status int, param, message string) {
	typ := "invalid_request_error"
	if status >= 500 {
		typ = "server_error"
	}
	var p any
	if param != "" {
		p = param
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{"message": message, "type": typ, "param": p, "code": nil},
	})
}
