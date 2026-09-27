package testutil

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// NewGemini starts a fake Gemini API. It serves
// POST /v1beta/models/{model}:generateContent as JSON and
// POST /v1beta/models/{model}:streamGenerateContent?alt=sse as an SSE stream,
// and expects the key in the x-goog-api-key header. Unlike the real API it
// refuses a key in the URL (?key=), which the gateway must never send
// upstream. It fails the test if cfg.Usage.CacheWrite is set, because Gemini
// reports no cache write tokens.
//
// Every stream chunk carries usageMetadata; the output count grows and the
// last chunk has the configured values, so a parser must keep the last one.
//
// Format: the discovery document at
// https://generativelanguage.googleapis.com/$discovery/rest?version=v1beta
// (GenerateContentResponse, Candidate, UsageMetadata), the API reference at
// https://ai.google.dev/api/generate-content, and the Google API error model
// at https://google.aip.dev/193.
func NewGemini(t testing.TB, cfg Config) *Server {
	t.Helper()
	if cfg.Usage.CacheWrite != 0 {
		t.Fatalf("testutil: Gemini reports no cache write tokens; Usage.CacheWrite must be 0")
	}
	return start(t, cfg, func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("POST /v1beta/models/{action}", s.geminiModels)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			geminiError(w, http.StatusNotFound, "unknown path "+r.URL.Path)
		})
	})
}

type geminiRequest struct {
	Contents []json.RawMessage `json:"contents"`
}

func (s *Server) geminiModels(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	i := strings.LastIndexByte(action, ':')
	if i <= 0 {
		geminiError(w, http.StatusNotFound, "unknown path "+r.URL.Path)
		return
	}
	model, method := action[:i], action[i+1:]
	stream := method == "streamGenerateContent"
	switch {
	case method != "generateContent" && !stream:
		geminiError(w, http.StatusNotFound, "unknown method "+method)
		return
	case r.URL.Query().Has("key"):
		geminiError(w, http.StatusBadRequest, "the fake refuses API keys in the URL; send x-goog-api-key")
		return
	case !s.authorized(r.Header.Get("x-goog-api-key")):
		geminiError(w, http.StatusUnauthorized, "API key not valid. Please pass a valid API key.")
		return
	case s.cfg.FailStatus != 0:
		geminiError(w, s.cfg.FailStatus, "fake provider failure")
		return
	case stream && r.URL.Query().Get("alt") != "sse":
		geminiError(w, http.StatusBadRequest, "the fake streams only with alt=sse")
		return
	}
	var req geminiRequest
	switch err := json.NewDecoder(r.Body).Decode(&req); {
	case err != nil:
		geminiError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	case len(req.Contents) == 0:
		geminiError(w, http.StatusBadRequest, "contents is not specified")
		return
	}

	id := s.id("fake-response-")
	response := func(text string, finish bool, u Usage) map[string]any {
		candidate := map[string]any{
			"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": text}}},
			"index":   0,
		}
		if finish {
			candidate["finishReason"] = "STOP"
		}
		return map[string]any{
			"candidates":    []any{candidate},
			"usageMetadata": geminiUsage(u),
			"modelVersion":  model,
			"responseId":    id,
		}
	}
	if !stream {
		writeJSON(w, http.StatusOK, response(s.cfg.text(), true, s.cfg.Usage))
		return
	}

	sse := newSSE(w, r, s.cfg.ChunkDelay)
	pieces := s.cfg.pieces()
	for i, piece := range pieces {
		u := s.cfg.Usage
		u.Output = u.Output * (i + 1) / len(pieces)
		if i > 0 && !sse.pause() || !sse.send("", response(piece, i == len(pieces)-1, u)) {
			return
		}
	}
}

// geminiUsage returns usageMetadata. Like the real API, which omits zero
// values, it leaves out the optional counts when they are zero.
func geminiUsage(u Usage) map[string]any {
	m := map[string]any{
		"promptTokenCount":     u.Input,
		"candidatesTokenCount": u.Output,
		"totalTokenCount":      u.Input + u.Output + u.Reasoning, // prompt + thoughts + candidates
	}
	if u.CacheRead != 0 {
		m["cachedContentTokenCount"] = u.CacheRead
	}
	if u.Reasoning != 0 {
		m["thoughtsTokenCount"] = u.Reasoning
	}
	return m
}

// grpcStatus maps HTTP statuses to google.rpc.Code names, following the HTTP
// mappings in google/rpc/code.proto.
var grpcStatus = map[int]string{
	400: "INVALID_ARGUMENT",
	401: "UNAUTHENTICATED",
	403: "PERMISSION_DENIED",
	404: "NOT_FOUND",
	409: "ABORTED",
	429: "RESOURCE_EXHAUSTED",
	499: "CANCELLED", //nolint:misspell // the google.rpc.Code name
	500: "INTERNAL",
	501: "UNIMPLEMENTED",
	503: "UNAVAILABLE",
	504: "DEADLINE_EXCEEDED",
}

func geminiError(w http.ResponseWriter, status int, message string) {
	name, ok := grpcStatus[status]
	if !ok {
		name = "UNKNOWN"
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{"code": status, "message": message, "status": name},
	})
}
