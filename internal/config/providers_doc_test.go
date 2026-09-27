package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The providers in the guide to OpenAI-compatible providers load, each with
// its key from the environment.
func TestCompatibleProvidersGuide(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "docs", "how-to", "connect-openai-compatible-providers.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)checks that this block loads\\. -->\\s*```yaml\n(.*?)```").FindSubmatch(page)
	if m == nil {
		t.Fatal("the guide has no marked YAML block")
	}
	yaml := strings.ReplaceAll(string(m[1]), "\n   ", "\n") // the block is indented in a list
	yaml = strings.TrimPrefix(yaml, "   ")
	env := map[string]string{}
	for _, name := range []string{"DEEPSEEK", "XAI", "MISTRAL", "GROQ", "OPENROUTER"} {
		env[name+"_API_KEY"] = "EXAMPLE-" + name
	}
	cfg, err := Parse([]byte(yaml), envMap(env))
	if err != nil {
		t.Fatalf("the guide's providers don't load: %v\n%s", err, yaml)
	}
	want := map[string]string{"deepseek": "https://api.deepseek.com", "xai": "https://api.x.ai/v1",
		"mistral": "https://api.mistral.ai/v1", "groq": "https://api.groq.com/openai/v1",
		"openrouter": "https://openrouter.ai/api/v1"}
	if len(cfg.Providers) != len(want) {
		t.Fatalf("the guide has %d providers, want %d", len(cfg.Providers), len(want))
	}
	for _, p := range cfg.Providers {
		if p.Type != TypeOpenAI || want[p.Name] != p.BaseURL || p.APIKey.Reveal() == "" {
			t.Errorf("provider %+v", p)
		}
	}
}
