package router

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/catalog"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/netguard"
	"github.com/852hamza/chowki/internal/providers"
)

var t0 = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func newRouter(t *testing.T, aliases map[string][]string, extra ...config.Provider) *Router {
	t.Helper()
	cfgs := append([]config.Provider{
		{Name: "openai", Type: config.TypeOpenAI, BaseURL: "https://api.example.com/v1"},
		{Name: "anthropic", Type: config.TypeAnthropic, BaseURL: "https://api.example.org"},
	}, extra...)
	ps, err := providers.New(cfgs, netguard.Policy{AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load([]byte(`{"models":[{"provider":"openai","model":"gpt-listed","price":{"input":1,"output":1},` +
		`"source":"https://example.com","updated":"2026-09-27"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(ps, cat, aliases)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func names(ts []Target) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.String())
	}
	return out
}

var local = config.Provider{Name: "local", Type: config.TypeOpenAI, BaseURL: "http://127.0.0.1:11434/v1"}

func TestResolve(t *testing.T) {
	r := newRouter(t, map[string][]string{
		"fast":  {"local/small", "openai/gpt-x", "anthropic/claude-x"},
		"smart": {"anthropic/claude-y"},
	}, local)
	tests := []struct {
		wantType, requested string
		want                []string
		code                string
	}{
		{"openai", "fast", []string{"local/small", "openai/gpt-x"}, ""},
		{"anthropic", "fast", []string{"anthropic/claude-x"}, ""},
		{"openai", "smart", nil, CodeWrongEndpoint},
		{"openai", "openai/gpt-x", []string{"openai/gpt-x"}, ""},
		{"openai", "anthropic/claude", nil, CodeWrongEndpoint},
		{"openai", "gpt-listed", []string{"openai/gpt-listed"}, ""},
		{"openai", "unlisted", nil, CodeUnknownProvider},
		{"anthropic", "claude-z", []string{"anthropic/claude-z"}, ""},
	}
	for _, tt := range tests {
		got, err := r.Resolve(tt.wantType, tt.requested, t0)
		var e *Error
		if tt.code != "" {
			if !errors.As(err, &e) || e.Code != tt.code {
				t.Errorf("Resolve(%s, %s) error = %v, want %s", tt.wantType, tt.requested, err, tt.code)
			}
			continue
		}
		if err != nil || !slices.Equal(names(got), tt.want) {
			t.Errorf("Resolve(%s, %s) = %v, %v; want %v", tt.wantType, tt.requested, names(got), err, tt.want)
		}
	}
	// With one provider of a type, any model name goes to it.
	if got, err := newRouter(t, nil).Resolve("openai", "anything", t0); err != nil ||
		!slices.Equal(names(got), []string{"openai/anything"}) {
		t.Errorf("Resolve() with one OpenAI provider = %v, %v", names(got), err)
	}
}

func TestBreaker(t *testing.T) {
	r := newRouter(t, map[string][]string{"fast": {"local/small", "openai/gpt-x"}}, local)
	order := func(at time.Time) []string {
		t.Helper()
		got, err := r.Resolve("openai", "fast", at)
		if err != nil {
			t.Fatal(err)
		}
		return names(got)
	}
	small := Target{r.providers["local"], "small"}
	for range failuresToOpen - 1 {
		r.Report(small, false, t0)
	}
	if got := order(t0); got[0] != "local/small" {
		t.Errorf("after %d failures, order = %v; want local/small still first", failuresToOpen-1, got)
	}
	r.Report(small, false, t0)
	if got := order(t0.Add(openFor - time.Second)); !slices.Equal(got, []string{"openai/gpt-x", "local/small"}) {
		t.Errorf("an unhealthy target comes first: %v", got)
	}
	if got := order(t0.Add(openFor)); got[0] != "local/small" {
		t.Errorf("after %v, order = %v; want local/small tried again", openFor, got)
	}
	// One more failure after the pause is enough to skip it again; a
	// success makes it healthy.
	r.Report(small, false, t0.Add(openFor))
	if got := order(t0.Add(openFor + time.Second)); got[0] != "openai/gpt-x" {
		t.Errorf("after a failure in the retry, order = %v", got)
	}
	r.Report(small, true, t0.Add(openFor+time.Second))
	if got := order(t0.Add(openFor + time.Second)); got[0] != "local/small" {
		t.Errorf("after a success, order = %v", got)
	}
}

func TestNewChecksAliases(t *testing.T) {
	ps, err := providers.New([]config.Provider{{Name: "openai", Type: config.TypeOpenAI,
		BaseURL: "https://api.example.com/v1"}}, netguard.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"missing/model", "openai", "openai/"} {
		if _, err := New(ps, nil, map[string][]string{"a": {target}}); err == nil {
			t.Errorf("New() accepted the alias target %q", target)
		}
	}
}

func TestModels(t *testing.T) {
	r := newRouter(t, map[string][]string{"fast": {"anthropic/c", "openai/g"}, "smart": {"anthropic/c"}})
	var got []string
	for _, m := range r.Models("openai") {
		got = append(got, m.ID+":"+m.Owner)
	}
	if want := []string{"fast:chowki", "openai/gpt-listed:openai"}; !slices.Equal(got, want) {
		t.Errorf("Models(openai) = %v, want %v", got, want)
	}
}
