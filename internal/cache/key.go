package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
)

// Request is what identifies a request in the cache: everything that can
// change the provider's answer.
type Request struct {
	Family   string // the API family, openai or anthropic
	Endpoint string // such as /v1/chat/completions
	// Provider and Model serve the request, after routing.
	Provider, Model string
	// Project keeps entries apart: projects never share answers.
	Project int64
	// Headers are the client headers that reach the provider.
	Headers http.Header
	// Body is the request body, a JSON object.
	Body []byte
}

// volatile are the top-level fields that don't change the answer: who
// asked, and how to stream it. model is left out because Request.Model is
// the model that serves the request, after routing.
var volatile = []string{"model", "user", "metadata", "stream", "stream_options"}

// Key returns the cache key of r: a SHA-256 hash of its fields and of its
// body in canonical JSON, without the volatile fields. Whitespace and the
// order of fields don't change the key; numbers keep their text.
func Key(r Request) ([32]byte, error) {
	var fields map[string]any
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	dec.UseNumber()
	if err := dec.Decode(&fields); err != nil {
		return [32]byte{}, fmt.Errorf("cache key: %w", err)
	}
	for _, name := range volatile {
		delete(fields, name)
	}
	// json.Marshal sorts the keys of maps, which makes the JSON canonical.
	data, err := json.Marshal([]any{"chowki-cache-v1", r.Family, r.Endpoint, r.Provider, r.Model, r.Project,
		r.Headers, fields})
	if err != nil {
		return [32]byte{}, fmt.Errorf("cache key: %w", err)
	}
	return sha256.Sum256(data), nil
}
