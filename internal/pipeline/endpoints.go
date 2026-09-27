package pipeline

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/852hamza/chowki/internal/providers"
	"github.com/852hamza/chowki/internal/usage"
)

// kind is what an endpoint does, which decides the stages that apply.
type kind int

const (
	// kindChat generates text: every stage applies.
	kindChat kind = iota
	// kindEmbeddings computes embeddings: no streams and no prompt-cache
	// optimizer.
	kindEmbeddings
	// kindCountTokens counts tokens, which providers don't bill: no cache,
	// token limit or budget.
	kindCountTokens
)

// Endpoint is an API endpoint that the gateway relays to providers.
type Endpoint struct {
	Family   usage.Family
	Path     string // the gateway's path, such as /v1/chat/completions
	upstream string // the provider's path
	kind     kind
}

// The endpoints that the gateway relays.
var (
	OpenAIChat           = Endpoint{usage.OpenAI, "/v1/chat/completions", providers.ChatCompletions, kindChat}
	OpenAIEmbeddings     = Endpoint{usage.OpenAI, "/v1/embeddings", providers.Embeddings, kindEmbeddings}
	AnthropicMessages    = Endpoint{usage.Anthropic, "/anthropic/v1/messages", providers.Messages, kindChat}
	AnthropicCountTokens = Endpoint{usage.Anthropic, "/anthropic/v1/messages/count_tokens", providers.CountTokens,
		kindCountTokens}
)

// Endpoints are the endpoints with fixed paths that the gateway relays.
var Endpoints = []Endpoint{OpenAIChat, OpenAIEmbeddings, AnthropicMessages, AnthropicCountTokens}

// geminiPrefix is where the gateway serves Gemini's model methods, whose
// paths go on with the model and the method.
const geminiPrefix = "/gemini/v1beta/models/"

// Gemini's model methods. Records show their paths with {model}, and each
// target of a request gets a path with its own model.
var (
	GeminiGenerate = Endpoint{usage.Gemini, geminiPrefix + "{model}:generateContent", ":generateContent", kindChat}
	GeminiStream   = Endpoint{usage.Gemini, geminiPrefix + "{model}:streamGenerateContent",
		":streamGenerateContent?alt=sse", kindChat}
	GeminiCountTokens = Endpoint{usage.Gemini, geminiPrefix + "{model}:countTokens", ":countTokens", kindCountTokens}
	GeminiEmbed       = Endpoint{usage.Gemini, geminiPrefix + "{model}:embedContent", ":embedContent", kindEmbeddings}
	GeminiBatchEmbed  = Endpoint{usage.Gemini, geminiPrefix + "{model}:batchEmbedContents", ":batchEmbedContents",
		kindEmbeddings}
)

// GeminiEndpoints are the Gemini model methods that the gateway relays.
var GeminiEndpoints = []Endpoint{GeminiGenerate, GeminiStream, GeminiCountTokens, GeminiEmbed, GeminiBatchEmbed}

// target returns the provider's path for a request to model.
func (ep Endpoint) target(model string) string {
	if ep.Family == usage.Gemini {
		return providers.GeminiModels + model + ep.upstream
	}
	return ep.upstream
}

// redactionKind is the request shape that redaction reads.
func (ep Endpoint) redactionKind() string {
	switch {
	case ep.kind == kindEmbeddings && ep.Family == usage.Gemini:
		return "gemini-embeddings"
	case ep.kind == kindEmbeddings:
		return "embeddings"
	}
	return string(ep.Family)
}

// Gemini returns the handler of Gemini's model methods, whose paths name the
// model and the method, as in
// /gemini/v1beta/models/gemini-2.5-flash:generateContent. The model may be
// <provider>/<model> or an alias, as on the other endpoints.
func (g *Gateway) Gemini() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, geminiPrefix)
		if i := strings.LastIndexByte(rest, ':'); i > 0 {
			for _, ep := range GeminiEndpoints {
				if ep.Path == geminiPrefix+"{model}"+rest[i:] {
					g.serve(w, r, ep, rest[:i])
					return
				}
			}
		}
		g.NotFound(usage.Gemini).ServeHTTP(w, r)
	})
}

// Models returns the handler of GET /v1/models: the aliases and catalog
// models that the key may use through the OpenAI-format endpoints, in the
// OpenAI format.
func (g *Gateway) Models() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := &call{ep: OpenAIChat, family: usage.OpenAI, id: newRequestID(), start: time.Now(),
			w: &trackingWriter{ResponseWriter: w}}
		c.w.Header().Set(RequestIDHeader, c.id)
		if e := g.authenticate(r.Context(), c, r); e != nil {
			writeError(c.w, usage.OpenAI, c.id, *e)
			g.Logger.Info("request rejected", "request_id", c.id, "path", r.URL.Path, "status", e.Status)
			return
		}
		type model struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		}
		list := struct {
			Object string  `json:"object"`
			Data   []model `json:"data"`
		}{Object: "list", Data: []model{}}
		for _, m := range g.Router.Models(providerType(usage.OpenAI)) {
			if allowed(c.key.AllowedModels, m.ID) {
				list.Data = append(list.Data, model{ID: m.ID, Object: "model", OwnedBy: m.Owner})
			}
		}
		c.w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(c.w).Encode(list) // the client may be gone; nothing to do then
		g.Logger.Info("models listed", "request_id", c.id, "key_id", c.key.ID, "models", len(list.Data))
	})
}
