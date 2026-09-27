package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

// NewAnthropic starts a fake Anthropic Messages API. It serves POST
// /v1/messages as JSON, or as an SSE stream when the request sets
// "stream": true. It accepts the key in "x-api-key" or "Authorization:
// Bearer", requires the anthropic-version header, and sends a request-id
// header with every response.
//
// A stream follows the documented event flow: message_start (with the input
// and cache token counts and output_tokens 1), content_block_start, ping,
// content_block_delta events, content_block_stop, message_delta (with the
// cumulative output_tokens) and message_stop.
//
// Format: https://platform.claude.com/docs/en/api/messages,
// https://platform.claude.com/docs/en/build-with-claude/streaming,
// https://platform.claude.com/docs/en/api/overview (headers) and
// https://platform.claude.com/docs/en/api/errors.
func NewAnthropic(t testing.TB, cfg Config) *Server {
	t.Helper()
	return start(t, cfg, func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("POST /v1/messages", s.anthropicMessages)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			s.anthropicError(w, http.StatusNotFound, "unknown path "+r.URL.Path)
		})
	})
}

type anthropicRequest struct {
	Model     string            `json:"model"`
	MaxTokens *int              `json:"max_tokens"`
	Messages  []json.RawMessage `json:"messages"`
	Stream    bool              `json:"stream"`
}

func (s *Server) anthropicMessages(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("x-api-key")
	if key == "" {
		key = bearer(r)
	}
	switch {
	case !s.authorized(key):
		s.anthropicError(w, http.StatusUnauthorized, "invalid x-api-key")
		return
	case r.Header.Get("anthropic-version") == "":
		s.anthropicError(w, http.StatusBadRequest, "anthropic-version: header is required")
		return
	case s.cfg.FailStatus != 0:
		s.anthropicError(w, s.cfg.FailStatus, "fake provider failure")
		return
	}
	var req anthropicRequest
	switch err := json.NewDecoder(r.Body).Decode(&req); {
	case err != nil:
		s.anthropicError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	case req.Model == "":
		s.anthropicError(w, http.StatusBadRequest, "model: Field required")
		return
	case req.MaxTokens == nil:
		s.anthropicError(w, http.StatusBadRequest, "max_tokens: Field required")
		return
	case len(req.Messages) == 0:
		s.anthropicError(w, http.StatusBadRequest, "messages: Field required")
		return
	}

	w.Header().Set("request-id", s.id("req_fake"))
	id := s.id("msg_fake")
	u := s.cfg.Usage
	if !req.Stream {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":            id,
			"type":          "message",
			"role":          "assistant",
			"model":         req.Model,
			"content":       []any{map[string]any{"type": "text", "text": s.cfg.text()}},
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
			"usage":         anthropicUsage(u),
		})
		return
	}

	startUsage := anthropicUsage(u)
	startUsage["output_tokens"] = min(1, u.Output)
	delete(startUsage, "output_tokens_details")
	deltaUsage := map[string]any{"output_tokens": u.Output}
	if u.Reasoning != 0 {
		deltaUsage["output_tokens_details"] = map[string]any{"thinking_tokens": u.Reasoning}
	}

	sse := newSSE(w, r, s.cfg.ChunkDelay)
	events := []struct {
		name string
		data map[string]any
	}{
		{"message_start", map[string]any{"type": "message_start", "message": map[string]any{
			"id": id, "type": "message", "role": "assistant", "content": []any{}, "model": req.Model,
			"stop_reason": nil, "stop_sequence": nil, "usage": startUsage,
		}}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 0,
			"content_block": map[string]any{"type": "text", "text": ""}}},
		{"ping", map[string]any{"type": "ping"}},
	}
	for _, ev := range events {
		if !sse.send(ev.name, ev.data) {
			return
		}
	}
	for i, piece := range s.cfg.pieces() {
		delta := map[string]any{"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": piece}}
		if i > 0 && !sse.pause() || !sse.send("content_block_delta", delta) {
			return
		}
	}
	events = []struct {
		name string
		data map[string]any
	}{
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}},
		{"message_delta", map[string]any{"type": "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": deltaUsage}},
		{"message_stop", map[string]any{"type": "message_stop"}},
	}
	for _, ev := range events {
		if !sse.send(ev.name, ev.data) {
			return
		}
	}
}

func anthropicUsage(u Usage) map[string]any {
	m := map[string]any{
		"input_tokens":                u.Input,
		"output_tokens":               u.Output,
		"cache_creation_input_tokens": u.CacheWrite,
		"cache_read_input_tokens":     u.CacheRead,
	}
	if u.Reasoning != 0 {
		m["output_tokens_details"] = map[string]any{"thinking_tokens": u.Reasoning}
	}
	return m
}

// anthropicErrorTypes maps HTTP statuses to documented error types.
var anthropicErrorTypes = map[int]string{
	400: "invalid_request_error",
	401: "authentication_error",
	402: "billing_error",
	403: "permission_error",
	404: "not_found_error",
	409: "conflict_error",
	413: "request_too_large",
	429: "rate_limit_error",
	500: "api_error",
	504: "timeout_error",
	529: "overloaded_error",
}

func (s *Server) anthropicError(w http.ResponseWriter, status int, message string) {
	typ, ok := anthropicErrorTypes[status]
	if !ok {
		// The docs use invalid_request_error for unlisted 4xx statuses.
		typ = "invalid_request_error"
		if status >= 500 {
			typ = "api_error"
		}
	}
	requestID := w.Header().Get("request-id")
	if requestID == "" {
		requestID = s.id("req_fake")
		w.Header().Set("request-id", requestID)
	}
	writeJSON(w, status, map[string]any{
		"type":       "error",
		"error":      map[string]any{"type": typ, "message": message},
		"request_id": requestID,
	})
}
