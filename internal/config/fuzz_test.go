package config

import (
	"strings"
	"testing"
)

func FuzzParseDotEnv(f *testing.F) {
	for _, seed := range []string{
		"KEY=value\n", "export KEY='single'\n", `KEY="a\nb"` + "\n", "KEY= # comment\n", "# comment\n\nA=1\r\n",
		`KEY="unterminated`, "KEY='x' y", "=value", "export",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		vars, err := ParseDotEnv([]byte(data))
		if err != nil {
			return
		}
		for key := range vars {
			if !envNameRE.MatchString(key) {
				t.Errorf("ParseDotEnv(%q) returned invalid name %q", data, key)
			}
		}
	})
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{fullConfig, "", "server: [", "providers:\n  - name: x\n", "retention_days: -1\n"} {
		f.Add(seed)
	}
	env := envMap(map[string]string{"OPENAI_API_KEY": "sk-EXAMPLE"})
	f.Fuzz(func(t *testing.T, data string) {
		cfg, err := Parse([]byte(data), env)
		if err != nil {
			return
		}
		// Whatever the input, a configuration that loads is valid.
		if verr := cfg.validate(); verr != nil {
			t.Errorf("Parse(%q) returned an invalid config: %v", data, verr)
		}
		for _, p := range cfg.Providers {
			if strings.Contains(p.APIKey.String(), "sk-") {
				t.Errorf("provider key isn't redacted")
			}
		}
	})
}
