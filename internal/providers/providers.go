package providers

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/852hamza/chowki/internal/buildinfo"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/netguard"
)

// Provider is an upstream API that the gateway forwards requests to.
type Provider struct {
	// Name and Type come from the configuration.
	Name string
	Type string
	// Free means the provider doesn't bill the requests: its free tier.
	Free bool

	base   *url.URL
	key    config.Secret
	keyEnv string
	client *http.Client
}

// MissingKey returns the environment variable that should hold the
// provider key when the key is needed but empty, and "" otherwise.
func (p *Provider) MissingKey() string {
	if p.keyEnv != "" && p.key.Reveal() == "" {
		return p.keyEnv
	}
	return ""
}

// New returns the configured providers, by name. They share one HTTP client
// whose connections obey the network policy. It never follows redirects,
// which could lead around the policy.
func New(cfgs []config.Provider, guard netguard.Policy) (map[string]*Provider, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: guard.Control}
	client := &http.Client{
		Transport: &http.Transport{
			// Proxies from the environment would move the network policy
			// from the provider to the proxy, so there are none.
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          256,
			MaxIdleConnsPerHost:   64,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	ps := map[string]*Provider{}
	for _, c := range cfgs {
		base, err := url.Parse(strings.TrimSuffix(c.BaseURL, "/"))
		if err != nil {
			return nil, fmt.Errorf("provider %s: %w", c.Name, err)
		}
		ps[c.Name] = &Provider{Name: c.Name, Type: c.Type, Free: c.FreeTier, base: base, key: c.APIKey,
			keyEnv: c.APIKeyEnv, client: client}
	}
	return ps, nil
}

// Endpoints, relative to the provider's base URL. OpenAI base URLs end in
// /v1, as in the OpenAI SDKs; Anthropic and Gemini base URLs don't, as in
// their SDKs.
const (
	ChatCompletions = "/chat/completions"
	Responses       = "/responses"
	Embeddings      = "/embeddings"
	Messages        = "/v1/messages"
	CountTokens     = "/v1/messages/count_tokens" //nolint:gosec // G101: an API path, not a credential
	// GeminiModels precedes a Gemini model and method, as in
	// /v1beta/models/gemini-2.5-flash:generateContent.
	GeminiModels = "/v1beta/models/"
)

// forwarded are the client headers that reach the provider: the API
// version and beta features that the client's SDK asked for. Nothing else
// is forwarded, least of all the client's key.
var forwarded = []string{"anthropic-version", "anthropic-beta"}

// Forwarded returns the headers of client that Do forwards to the provider.
// They can change the provider's answer.
func Forwarded(client http.Header) http.Header {
	h := http.Header{}
	for _, name := range forwarded {
		if v := client.Values(name); len(v) > 0 {
			h[http.CanonicalHeaderKey(name)] = v
		}
	}
	return h
}

// Do sends a JSON request body to the provider's endpoint, a path with an
// optional query, authenticated with the provider key, and returns the
// response. The caller must close the response body.
func (p *Provider) Do(ctx context.Context, endpoint string, body []byte, client http.Header) (*http.Response, error) {
	u := *p.base
	path, query, _ := strings.Cut(endpoint, "?")
	u.Path += path
	u.RawQuery = query
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provider %s: %w", p.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "chowki/"+buildinfo.Version())
	for name, v := range Forwarded(client) {
		req.Header[name] = v
	}
	if key := p.key.Reveal(); key != "" {
		switch p.Type {
		case config.TypeAnthropic:
			req.Header.Set("x-api-key", key)
		case config.TypeGemini:
			// https://ai.google.dev/gemini-api/docs/api-key: never ?key=,
			// which would leak the key into logs.
			req.Header.Set("x-goog-api-key", key)
		default:
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider %s: %w", p.Name, err)
	}
	return resp, nil
}
