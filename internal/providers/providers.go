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
		ps[c.Name] = &Provider{Name: c.Name, Type: c.Type, base: base, key: c.APIKey, keyEnv: c.APIKeyEnv, client: client}
	}
	return ps, nil
}

// Endpoints, relative to the provider's base URL. OpenAI base URLs end in
// /v1, as in the OpenAI SDKs; Anthropic base URLs don't, as in its SDKs.
const (
	ChatCompletions = "/chat/completions"
	Messages        = "/v1/messages"
)

// forwarded are the client headers that reach the provider: the API
// version and beta features that the client's SDK asked for. Nothing else
// is forwarded, least of all the client's key.
var forwarded = []string{"anthropic-version", "anthropic-beta"}

// Do sends a JSON request body to the provider's endpoint, authenticated
// with the provider key, and returns the response. The caller must close
// the response body.
func (p *Provider) Do(ctx context.Context, endpoint string, body []byte, client http.Header) (*http.Response, error) {
	u := *p.base
	u.Path += endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provider %s: %w", p.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "chowki/"+buildinfo.Version())
	for _, h := range forwarded {
		if v := client.Values(h); len(v) > 0 {
			req.Header[http.CanonicalHeaderKey(h)] = v
		}
	}
	if key := p.key.Reveal(); key != "" {
		switch p.Type {
		case config.TypeAnthropic:
			req.Header.Set("x-api-key", key)
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
