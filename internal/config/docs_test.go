package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/testutil"
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

// TestConfigurationSummary writes the summary table of the configuration
// reference from the settings, their variables and their defaults.
func TestConfigurationSummary(t *testing.T) {
	var b strings.Builder
	b.WriteString("\n| Setting | Environment variable | Default |\n|---|---|---|\n")
	walkSettings(reflect.ValueOf(Default()), EnvPrefix, "", func(name, path string, field reflect.Value) {
		anchor := strings.ReplaceAll(path, ".", "") // as GitHub and Docusaurus make heading anchors
		fmt.Fprintf(&b, "| [`%s`](#%s) | `%s` | %s |\n", path, anchor, name, defaultOf(field))
	})
	for _, list := range []string{"providers", "aliases"} {
		fmt.Fprintf(&b, "| [`%s`](#%s) | None: only in the file | None |\n", list, list)
	}
	b.WriteString("\n")
	testutil.CheckGenerated(t, "docs/reference/configuration.md", "summary", b.String())
}

// defaultOf formats a default as the file would hold it.
func defaultOf(v reflect.Value) string {
	switch {
	case v.Type() == durationType:
		d := time.Duration(v.Int()).String()
		for _, zero := range []string{"0s", "0m"} { // 1h0m0s reads as 1h
			if strings.HasSuffix(d, "m"+zero) || strings.HasSuffix(d, "h"+zero) {
				d = strings.TrimSuffix(d, zero)
			}
		}
		return "`" + d + "`"
	case v.Kind() == reflect.String && v.String() == "":
		return "None"
	}
	return fmt.Sprintf("`%v`", v.Interface())
}
