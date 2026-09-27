package pipeline

import (
	"encoding/json"
	"net/http"
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

// Endpoints are all the endpoints that the gateway relays.
var Endpoints = []Endpoint{OpenAIChat, OpenAIEmbeddings, AnthropicMessages, AnthropicCountTokens}

// redactionKind is the request shape that redaction reads.
func (ep Endpoint) redactionKind() string {
	if ep.kind == kindEmbeddings {
		return "embeddings"
	}
	return string(ep.Family)
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
