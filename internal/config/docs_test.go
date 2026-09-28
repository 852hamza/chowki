package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReferenceListsEverySetting keeps the configuration reference, which
// is written by hand, in step with the code.
func TestReferenceListsEverySetting(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "configuration.md"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, name := range EnvVars() {
		if !strings.Contains(page, "`"+name+"`") {
			t.Errorf("docs/reference/configuration.md doesn't list %s", name)
		}
		// CHOWKI_SERVER_LISTEN documents the section `server.listen`.
		path := strings.ToLower(strings.TrimPrefix(name, EnvPrefix))
		for _, section := range []string{"server", "storage", "security", "log", "defaults"} {
			if rest, ok := strings.CutPrefix(path, section+"_"); ok {
				path = section + "." + rest
			}
		}
		if !strings.Contains(page, "## `"+path+"`") {
			t.Errorf("docs/reference/configuration.md has no section for %s", path)
		}
	}
}
