package providerkeys

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/store"
)

func newKeys(t *testing.T, fill byte) *Keys {
	t.Helper()
	k, err := New(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpen(t *testing.T) {
	k := newKeys(t, 1)
	sealed := k.Seal("openai", "not-a-real-key")
	if bytes.Contains(sealed, []byte("not-a-real-key")) {
		t.Fatal("the sealed key holds the key in the clear")
	}
	if key, err := k.Open("openai", sealed); err != nil || key.Reveal() != "not-a-real-key" {
		t.Errorf("Open() = %q, %v", key.Reveal(), err)
	}
	for name, open := range map[string]func() (config.Secret, error){
		"another provider":   func() (config.Secret, error) { return k.Open("anthropic", sealed) },
		"another master key": func() (config.Secret, error) { return newKeys(t, 2).Open("openai", sealed) },
	} {
		_, err := open()
		if err == nil || !strings.Contains(err.Error(), "chowki provider set-key") ||
			strings.Contains(err.Error(), "not-a-real-key") {
			t.Errorf("Open() with %s: error = %v", name, err)
		}
	}
	if _, err := New([]byte("short")); err == nil {
		t.Error("New() with a short master key succeeded")
	}
}

func TestClean(t *testing.T) {
	for raw, want := range map[string]string{"key-1\n": "key-1", "  key-2\r\n": "key-2"} {
		if got, err := Clean(raw); err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", " \n", "two words", "tab\there", "bell\a", strings.Repeat("k", MaxLen+1)} {
		if _, err := Clean(raw); err == nil {
			t.Errorf("Clean(%q) succeeded", raw)
		} else if len(raw) > 3 && strings.Contains(err.Error(), raw) {
			t.Errorf("Clean() error shows the input: %v", err)
		}
	}
}

func TestApply(t *testing.T) {
	st, err := store.OpenSQLite(t.Context(), "file:"+filepath.Join(t.TempDir(), "chowki.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	k := newKeys(t, 1)
	now := time.Now()
	for name, sealed := range map[string][]byte{"openai": k.Seal("openai", "stored-openai"),
		"anthropic": k.Seal("anthropic", "stored-anthropic"), "ollama": k.Seal("ollama", "stored-ollama"),
		"gemini": newKeys(t, 2).Seal("gemini", "other-master-key"), "gone": k.Seal("gone", "not-configured")} {
		if err := st.SetProviderKey(t.Context(), store.ProviderKey{Provider: name, Sealed: sealed, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	providers := []config.Provider{
		{Name: "openai", APIKeyEnv: "OPENAI_API_KEY", APIKey: "from-env"}, // the environment wins
		{Name: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
		{Name: "ollama"},
		{Name: "gemini", APIKeyEnv: "GEMINI_API_KEY"},
		{Name: "mistral", APIKeyEnv: "MISTRAL_API_KEY"},
	}
	used, errs, err := Apply(t.Context(), st, k, providers)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(used, []string{"anthropic", "ollama"}) {
		t.Errorf("Apply() used %v, want anthropic and ollama", used)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "gemini") {
		t.Errorf("Apply() errors = %v, want one for gemini", errs)
	}
	for i, want := range []string{"from-env", "stored-anthropic", "stored-ollama", "", ""} {
		if got := providers[i].APIKey.Reveal(); got != want {
			t.Errorf("provider %s key = %q, want %q", providers[i].Name, got, want)
		}
	}
}
