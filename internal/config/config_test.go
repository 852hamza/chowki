package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// envMap returns an environment lookup backed by m.
func envMap(m map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

const fullConfig = `
server:
  listen: "127.0.0.1:9090"
  max_body_mb: 5
  upstream_timeout: 90s
storage:
  driver: sqlite
  dsn: "file:/var/lib/chowki/chowki.db"
  cache_max_mb: 64
security:
  master_key_file: /etc/chowki/master.key
  allow_private_upstreams: false
log:
  level: debug
retention_days: 30
defaults:
  cache: exact
  cache_ttl: 10m
  prompt_cache: "off"
  redaction: block
providers:
  - name: openai
    type: openai
    base_url: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY
  - name: missing-key
    type: openai
    base_url: https://api.example.com/v1
    api_key_env: UNSET_KEY
`

func TestParseDefaults(t *testing.T) {
	for _, data := range []string{"", "# only a comment\n"} {
		cfg, err := Parse([]byte(data), envMap(nil))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", data, err)
		}
		if want := Default(); !reflect.DeepEqual(*cfg, want) {
			t.Errorf("Parse(%q) = %+v, want the defaults %+v", data, *cfg, want)
		}
	}
}

func TestParseFile(t *testing.T) {
	cfg, err := Parse([]byte(fullConfig), envMap(map[string]string{
		"OPENAI_API_KEY":    "sk-EXAMPLE-openai",
		"ANTHROPIC_API_KEY": "sk-ant-EXAMPLE",
	}))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := Config{
		Server:        Server{Listen: "127.0.0.1:9090", MaxBodyMB: 5, UpstreamTimeout: 90 * time.Second},
		Storage:       Storage{Driver: "sqlite", DSN: "file:/var/lib/chowki/chowki.db", CacheMaxMB: 64},
		Security:      Security{MasterKeyFile: "/etc/chowki/master.key", AllowPrivateUpstreams: false},
		Log:           Log{Level: "debug"},
		RetentionDays: 30,
		Defaults:      Defaults{Cache: "exact", CacheTTL: 10 * time.Minute, PromptCache: "off", Redaction: "block"},
		Providers: []Provider{
			{Name: "openai", Type: TypeOpenAI, BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY",
				APIKey: "sk-EXAMPLE-openai"},
			{Name: "anthropic", Type: TypeAnthropic, BaseURL: "https://api.anthropic.com", APIKeyEnv: "ANTHROPIC_API_KEY",
				APIKey: "sk-ant-EXAMPLE"},
			{Name: "missing-key", Type: TypeOpenAI, BaseURL: "https://api.example.com/v1", APIKeyEnv: "UNSET_KEY"},
		},
	}
	if !reflect.DeepEqual(*cfg, want) {
		t.Errorf("Parse() =\n%+v\nwant\n%+v", *cfg, want)
	}
}

func TestParseEnvOverrides(t *testing.T) {
	cfg, err := Parse([]byte(fullConfig), envMap(map[string]string{
		"CHOWKI_SERVER_LISTEN":                    ":7000",
		"CHOWKI_SERVER_MAX_BODY_MB":               "64",
		"CHOWKI_SERVER_UPSTREAM_TIMEOUT":          "2m",
		"CHOWKI_STORAGE_DSN":                      "file:other.db",
		"CHOWKI_SECURITY_ALLOW_PRIVATE_UPSTREAMS": "true",
		"CHOWKI_LOG_LEVEL":                        "warn",
		"CHOWKI_RETENTION_DAYS":                   "7",
		"CHOWKI_STORAGE_CACHE_MAX_MB":             "0",
		"CHOWKI_DEFAULTS_CACHE":                   "off",
		"CHOWKI_DEFAULTS_CACHE_TTL":               "30s",
	}))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := []any{cfg.Server.Listen, cfg.Server.MaxBodyMB, cfg.Server.UpstreamTimeout, cfg.Storage.DSN,
		cfg.Security.AllowPrivateUpstreams, cfg.Log.Level, cfg.RetentionDays, cfg.Storage.CacheMaxMB,
		cfg.Defaults.Cache, cfg.Defaults.CacheTTL}
	want := []any{":7000", 64, 2 * time.Minute, "file:other.db", true, "warn", 7, 0, "off", 30 * time.Second}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("overridden settings = %v, want %v", got, want)
	}
}

func TestEnvVars(t *testing.T) {
	want := []string{
		"CHOWKI_SERVER_LISTEN", "CHOWKI_SERVER_MAX_BODY_MB", "CHOWKI_SERVER_UPSTREAM_TIMEOUT",
		"CHOWKI_STORAGE_DRIVER", "CHOWKI_STORAGE_DSN", "CHOWKI_STORAGE_CACHE_MAX_MB",
		"CHOWKI_SECURITY_MASTER_KEY_FILE", "CHOWKI_SECURITY_ALLOW_PRIVATE_UPSTREAMS",
		"CHOWKI_LOG_LEVEL", "CHOWKI_RETENTION_DAYS", "CHOWKI_DEFAULTS_CACHE", "CHOWKI_DEFAULTS_CACHE_TTL",
		"CHOWKI_DEFAULTS_PROMPT_CACHE", "CHOWKI_DEFAULTS_REDACTION",
	}
	if got := EnvVars(); !reflect.DeepEqual(got, want) {
		t.Errorf("EnvVars() = %q, want %q", got, want)
	}
}

func TestParseErrors(t *testing.T) {
	provider := func(fields string) string {
		return "providers:\n  - name: p\n    type: openai\n    base_url: https://api.example.com/v1\n" + fields
	}
	tests := []struct {
		name string
		data string
		env  map[string]string
		want []string
	}{
		{"unknown field", "server:\n  port: 80\n", nil, []string{"field port not found"}},
		{"key in the file", provider("    api_key: sk-EXAMPLE\n"), nil, []string{"field api_key not found"}},
		{"duration without unit", "server:\n  upstream_timeout: 600\n", nil, []string{"parse YAML"}},
		{"not YAML", "server: [", nil, []string{"parse YAML"}},
		{"several problems", "server:\n  listen: nowhere\n  max_body_mb: 0\nlog:\n  level: loud\nretention_days: 0\n", nil,
			[]string{"server.listen", "server.max_body_mb", "log.level", "retention_days"}},
		{"storage", "storage:\n  driver: postgres\n  dsn: \"\"\n", nil, []string{"storage.driver", "storage.dsn"}},
		{"timeout", "server:\n  upstream_timeout: -1s\n", nil, []string{"server.upstream_timeout"}},
		{"master key file", "security:\n  master_key_file: \"\"\n", nil, []string{"security.master_key_file"}},
		{"cache", "storage:\n  cache_max_mb: -1\ndefaults:\n  cache: always\n  cache_ttl: 0s\n  prompt_cache: on\n" +
			"  redaction: strict\n", nil, []string{"storage.cache_max_mb", "defaults.cache: must be off or exact",
			"defaults.cache_ttl", "defaults.prompt_cache: must be auto or off", "defaults.redaction"}},
		{"provider name", strings.Replace(provider(""), "name: p", "name: Open/AI", 1), nil,
			[]string{"providers[0].name"}},
		{"duplicate provider", provider("") + "  - name: p\n    type: openai\n    base_url: https://x.example.com\n", nil,
			[]string{`providers[1].name: "p" is used twice`}},
		{"provider type", strings.Replace(provider(""), "type: openai", "type: azure", 1), nil,
			[]string{"providers[0].type"}},
		{"relative base URL", strings.Replace(provider(""), "https://api.example.com/v1", "api.example.com", 1), nil,
			[]string{"providers[0].base_url: must be an absolute http or https URL"}},
		{"credentials in base URL", strings.Replace(provider(""), "https://", "https://user:pass@", 1), nil,
			[]string{"must not contain credentials"}},
		{"query in base URL", strings.Replace(provider(""), "/v1", "/v1?key=x", 1), nil,
			[]string{"must not have a query"}},
		{"metadata base URL", strings.Replace(provider(""), "api.example.com", "169.254.169.254", 1), nil,
			[]string{"always blocked"}},
		{"private base URL not allowed", "security:\n  allow_private_upstreams: false\n" +
			strings.Replace(provider(""), "https://api.example.com", "http://localhost:11434", 1), nil,
			[]string{"allow_private_upstreams"}},
		{"key variable name", provider("    api_key_env: sk-EXAMPLE\n"), nil, []string{"providers[0].api_key_env"}},
		{"env not a number", "", map[string]string{"CHOWKI_SERVER_MAX_BODY_MB": "lots"}, []string{
			"CHOWKI_SERVER_MAX_BODY_MB: not a whole number"}},
		{"env not a duration", "", map[string]string{"CHOWKI_SERVER_UPSTREAM_TIMEOUT": "soon"}, []string{
			"CHOWKI_SERVER_UPSTREAM_TIMEOUT: not a duration"}},
		{"env not a bool", "", map[string]string{"CHOWKI_SECURITY_ALLOW_PRIVATE_UPSTREAMS": "maybe"}, []string{
			"CHOWKI_SECURITY_ALLOW_PRIVATE_UPSTREAMS: not true or false"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.data), envMap(tt.env))
			if err == nil {
				t.Fatal("Parse() succeeded, want an error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error doesn't contain %q:\n%v", w, err)
				}
			}
		})
	}
}

func TestPrivateUpstreamAllowedByDefault(t *testing.T) {
	data := "providers:\n  - name: local\n    type: openai\n    base_url: http://localhost:11434/v1\n"
	if _, err := Parse([]byte(data), envMap(nil)); err != nil {
		t.Errorf("Parse() error = %v, want a local provider to be allowed by default", err)
	}
}

func TestErrorsDontShowEnvValues(t *testing.T) {
	const secret = "sk-EXAMPLE-do-not-print"
	_, err := Parse(nil, envMap(map[string]string{"CHOWKI_SERVER_MAX_BODY_MB": secret}))
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("error = %v; want an error without the value", err)
	}
}

func TestSecretIsRedacted(t *testing.T) {
	const key = "sk-EXAMPLE-secret-value"
	p := Provider{Name: "openai", APIKey: key}
	var out bytes.Buffer
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		fmt.Fprintf(&out, format+"\n", p)
		fmt.Fprintf(&out, format+"\n", p.APIKey)
	}
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	logger.Info("provider", "provider", p, "key", p.APIKey)
	b, err := json.Marshal(p) //nolint:gosec // on purpose: the key must come out redacted
	if err != nil {
		t.Fatal(err)
	}
	out.Write(b)
	if strings.Contains(out.String(), key) {
		t.Errorf("output contains the secret:\n%s", out.String())
	}
	if p.APIKey.Reveal() != key {
		t.Errorf("Reveal() = %q, want the key", p.APIKey.Reveal())
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chowki.yaml")
	if err := os.WriteFile(path, []byte("log:\n  level: error\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, envMap(nil))
	if err != nil || cfg.Log.Level != "error" {
		t.Fatalf("Load() = %+v, %v", cfg, err)
	}
	if err := os.WriteFile(path, []byte("log:\n  level: loud\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, envMap(nil)); err == nil || !strings.Contains(err.Error(), path+": log.level") {
		t.Errorf("Load() error = %v, want it to name the file and the setting", err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml"), envMap(nil)); err == nil {
		t.Error("Load() of a missing file succeeded")
	}
}
