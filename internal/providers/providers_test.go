package providers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/netguard"
)

func TestDoHeaders(t *testing.T) {
	var got http.Header
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, path = r.Header.Clone(), r.URL.Path
	}))
	t.Cleanup(srv.Close)

	client := http.Header{
		"Authorization":     {"Bearer chowki_virtual"},
		"X-Api-Key":         {"chowki_virtual"},
		"Anthropic-Version": {"2023-06-01"},
		"Anthropic-Beta":    {"a", "b"},
		"Cookie":            {"session=1"},
	}
	tests := []struct {
		cfg              config.Provider
		endpoint, path   string
		auth, apiKey     string
		forwardsVersions bool
	}{
		{config.Provider{Name: "o", Type: config.TypeOpenAI, BaseURL: srv.URL + "/v1/", APIKeyEnv: "K", APIKey: "sk-EXAMPLE"},
			ChatCompletions, "/v1/chat/completions", "Bearer sk-EXAMPLE", "", true},
		{config.Provider{Name: "a", Type: config.TypeAnthropic, BaseURL: srv.URL, APIKeyEnv: "K", APIKey: "sk-ant-EXAMPLE"},
			Messages, "/v1/messages", "", "sk-ant-EXAMPLE", true},
		{config.Provider{Name: "local", Type: config.TypeOpenAI, BaseURL: srv.URL + "/v1"},
			ChatCompletions, "/v1/chat/completions", "", "", true},
	}
	for _, tt := range tests {
		ps, err := New([]config.Provider{tt.cfg}, netguard.Policy{AllowPrivate: true})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := ps[tt.cfg.Name].Do(t.Context(), tt.endpoint, []byte(`{}`), client)
		if err != nil {
			t.Fatalf("%s: Do() error = %v", tt.cfg.Name, err)
		}
		_ = resp.Body.Close()
		if path != tt.path || got.Get("Authorization") != tt.auth || got.Get("X-Api-Key") != tt.apiKey {
			t.Errorf("%s: path %s, Authorization %q, x-api-key %q", tt.cfg.Name, path, got.Get("Authorization"),
				got.Get("X-Api-Key"))
		}
		if got.Get("Anthropic-Version") != "2023-06-01" || len(got.Values("Anthropic-Beta")) != 2 {
			t.Errorf("%s: API version headers weren't forwarded: %v", tt.cfg.Name, got)
		}
		if got.Get("Cookie") != "" || !strings.HasPrefix(got.Get("User-Agent"), "chowki/") ||
			got.Get("Content-Type") != "application/json" {
			t.Errorf("%s: headers = %v", tt.cfg.Name, got)
		}
	}
}

func TestDoDoesNotFollowRedirects(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	ps, err := New([]config.Provider{{Name: "o", Type: config.TypeOpenAI, BaseURL: srv.URL}}, netguard.Policy{AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ps["o"].Do(t.Context(), ChatCompletions, []byte(`{}`), http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || hits != 1 {
		t.Errorf("status %d after %d requests; want the redirect itself", resp.StatusCode, hits)
	}
}

func TestDoObeysNetworkPolicy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	ps, err := New([]config.Provider{{Name: "o", Type: config.TypeOpenAI, BaseURL: srv.URL}}, netguard.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ps["o"].Do(t.Context(), ChatCompletions, []byte(`{}`), http.Header{})
	if err == nil || !strings.Contains(err.Error(), "private address") {
		t.Errorf("Do() to loopback without AllowPrivate = %v, want a netguard error", err)
	}
}

func TestMissingKey(t *testing.T) {
	ps, err := New([]config.Provider{
		{Name: "needs", Type: config.TypeOpenAI, BaseURL: "https://api.example.com/v1", APIKeyEnv: "NEEDS_KEY"},
		{Name: "has", Type: config.TypeOpenAI, BaseURL: "https://api.example.com/v1", APIKeyEnv: "HAS_KEY", APIKey: "x"},
		{Name: "none", Type: config.TypeOpenAI, BaseURL: "http://localhost:11434/v1"},
	}, netguard.Policy{AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"needs": "NEEDS_KEY", "has": "", "none": ""} {
		if got := ps[name].MissingKey(); got != want {
			t.Errorf("%s.MissingKey() = %q, want %q", name, got, want)
		}
	}
}
