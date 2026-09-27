package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/852hamza/chowki/internal/netguard"
)

// Config is the gateway configuration from chowki.yaml.
type Config struct {
	Server        Server     `yaml:"server"`
	Storage       Storage    `yaml:"storage"`
	Security      Security   `yaml:"security"`
	Log           Log        `yaml:"log"`
	RetentionDays int        `yaml:"retention_days"`
	Defaults      Defaults   `yaml:"defaults"`
	Providers     []Provider `yaml:"providers"`
	// Aliases are model names that stand for provider/model targets, tried
	// in order: when one fails with a rate limit, a server error or a
	// timeout, the next one gets the request.
	Aliases map[string][]string `yaml:"aliases"`
}

// Server configures the HTTP server.
type Server struct {
	// Listen is the address to listen on, such as ":8080".
	Listen string `yaml:"listen"`
	// MaxBodyMB is the largest request body the gateway accepts, in MiB.
	MaxBodyMB int `yaml:"max_body_mb"`
	// UpstreamTimeout limits one provider call, including a whole stream.
	UpstreamTimeout time.Duration `yaml:"upstream_timeout"`
}

// Storage configures the database.
type Storage struct {
	// Driver is the database type. Only "sqlite" exists so far.
	Driver string `yaml:"driver"`
	// DSN locates the database, such as "file:data/chowki.db".
	DSN string `yaml:"dsn"`
	// CacheMaxMB limits the size of the exact response cache, in MiB; 0
	// turns the cache off.
	CacheMaxMB int `yaml:"cache_max_mb"`
}

// Security configures keys and network access.
type Security struct {
	// MasterKeyFile holds the master key that encrypts stored secrets. The
	// CHOWKI_MASTER_KEY environment variable takes precedence.
	MasterKeyFile string `yaml:"master_key_file"`
	// AllowPrivateUpstreams allows provider base URLs on loopback and
	// private networks, such as a local Ollama server.
	AllowPrivateUpstreams bool `yaml:"allow_private_upstreams"`
}

// Defaults are request settings; a virtual key can override some of them.
type Defaults struct {
	// Cache is off or exact: whether non-streaming requests use the exact
	// response cache.
	Cache string `yaml:"cache"`
	// CacheTTL is how long the exact cache serves a response.
	CacheTTL time.Duration `yaml:"cache_ttl"`
	// PromptCache is auto or off: whether Chowki adds a prompt-cache
	// breakpoint to Anthropic requests whose prefix repeats.
	PromptCache string `yaml:"prompt_cache"`
	// Redaction is off, mask, block or alert: what happens to secrets and
	// personal data in the text of requests.
	Redaction string `yaml:"redaction"`
}

// Log configures logging.
type Log struct {
	// Level is debug, info, warn or error.
	Level string `yaml:"level"`
}

// Provider is an upstream API that the gateway forwards requests to.
type Provider struct {
	// Name identifies the provider in model names such as "openai/<model>".
	Name string `yaml:"name"`
	// Type is the API the provider speaks: openai (also for every
	// OpenAI-compatible API), anthropic or gemini.
	Type string `yaml:"type"`
	// BaseURL is the API base URL, such as https://api.openai.com/v1.
	BaseURL string `yaml:"base_url"`
	// APIKeyEnv names the environment variable that holds the provider key.
	// Empty means the provider needs no key, like a local Ollama server.
	APIKeyEnv string `yaml:"api_key_env"`
	// FreeTier means the provider doesn't bill the requests, as on the free
	// tier of the Gemini API: they cost $0 whatever the catalog's prices.
	FreeTier bool `yaml:"free_tier"`
	// APIKey is the provider key, read from APIKeyEnv when the
	// configuration loads. Keys are never read from the file.
	APIKey Secret `yaml:"-"`
}

// Provider types.
const (
	TypeOpenAI    = "openai"
	TypeAnthropic = "anthropic"
	TypeGemini    = "gemini"
)

// Default returns the configuration that applies before the file and the
// environment are read.
func Default() Config {
	return Config{
		Server:        Server{Listen: ":8080", MaxBodyMB: 20, UpstreamTimeout: 10 * time.Minute},
		Storage:       Storage{Driver: "sqlite", DSN: "file:data/chowki.db", CacheMaxMB: 256},
		Security:      Security{MasterKeyFile: ".chowki/master.key", AllowPrivateUpstreams: true},
		Log:           Log{Level: "info"},
		RetentionDays: 90,
		Defaults:      Defaults{Cache: "off", CacheTTL: time.Hour, PromptCache: "auto", Redaction: "mask"},
	}
}

// Load reads the configuration file at path. See Parse.
func Load(path string, env func(string) (string, bool)) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := Parse(data, env)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse builds the configuration from the defaults, the YAML document in
// data and CHOWKI_ environment variables, in that order, then validates it
// and reads the provider keys. env looks up environment variables; use
// os.LookupEnv or the result of DotEnv.
func Parse(data []byte, env func(string) (string, bool)) (*Config, error) {
	cfg := Default()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	if err := applyEnv(&cfg, env); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	for i := range cfg.Providers {
		if name := cfg.Providers[i].APIKeyEnv; name != "" {
			key, _ := env(name)
			cfg.Providers[i].APIKey = Secret(key)
		}
	}
	return &cfg, nil
}

var (
	providerNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	envNameRE      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// validate reports every problem at once, each with the path of its field.
func (c *Config) validate() error {
	var errs []error
	add := func(field, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: "+format, append([]any{field}, args...)...))
	}

	if _, _, err := net.SplitHostPort(c.Server.Listen); err != nil {
		add("server.listen", "want host:port, such as :8080")
	}
	if c.Server.MaxBodyMB < 1 || c.Server.MaxBodyMB > 1024 {
		add("server.max_body_mb", "must be between 1 and 1024")
	}
	if c.Server.UpstreamTimeout <= 0 {
		add("server.upstream_timeout", "must be positive, such as 600s")
	}
	if c.Storage.Driver != "sqlite" {
		add("storage.driver", "must be sqlite")
	}
	if c.Storage.DSN == "" {
		add("storage.dsn", "must not be empty")
	}
	if c.Storage.CacheMaxMB < 0 || c.Storage.CacheMaxMB > 1<<20 {
		add("storage.cache_max_mb", "must be between 0, which turns the cache off, and 1048576")
	}
	if c.Security.MasterKeyFile == "" {
		add("security.master_key_file", "must not be empty")
	}
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, c.Log.Level) {
		add("log.level", "must be debug, info, warn or error")
	}
	if c.RetentionDays < 1 {
		add("retention_days", "must be at least 1")
	}
	if c.Defaults.Cache != "off" && c.Defaults.Cache != "exact" {
		add("defaults.cache", "must be off or exact")
	}
	if c.Defaults.CacheTTL <= 0 {
		add("defaults.cache_ttl", "must be positive, such as 1h")
	}
	if c.Defaults.PromptCache != "auto" && c.Defaults.PromptCache != "off" {
		add("defaults.prompt_cache", "must be auto or off")
	}
	if !slices.Contains([]string{"off", "mask", "block", "alert"}, c.Defaults.Redaction) {
		add("defaults.redaction", "must be off, mask, block or alert")
	}

	guard := netguard.Policy{AllowPrivate: c.Security.AllowPrivateUpstreams}
	seen := map[string]bool{}
	for i, p := range c.Providers {
		field := fmt.Sprintf("providers[%d]", i)
		switch {
		case !providerNameRE.MatchString(p.Name):
			add(field+".name", "must be lowercase letters, digits, - and _, such as openai")
		case seen[p.Name]:
			add(field+".name", "%q is used twice", p.Name)
		}
		seen[p.Name] = true
		if !slices.Contains([]string{TypeOpenAI, TypeAnthropic, TypeGemini}, p.Type) {
			add(field+".type", "must be openai, anthropic or gemini")
		}
		if err := checkBaseURL(p.BaseURL, guard); err != nil {
			add(field+".base_url", "%v", err)
		}
		if p.APIKeyEnv != "" && !envNameRE.MatchString(p.APIKeyEnv) {
			add(field+".api_key_env", "must be an environment variable name, such as OPENAI_API_KEY")
		}
	}
	for _, name := range slices.Sorted(maps.Keys(c.Aliases)) {
		field := "aliases." + name
		switch {
		case name == "" || strings.ContainsAny(name, "/ "):
			add(field, "an alias name must not be empty or hold / or spaces")
		case len(c.Aliases[name]) == 0:
			add(field, "must list at least one <provider>/<model> target")
		}
		for i, target := range c.Aliases[name] {
			p, model, ok := strings.Cut(target, "/")
			if !ok || model == "" || !seen[p] {
				add(fmt.Sprintf("%s[%d]", field, i), "must be <provider>/<model> with a provider from providers")
			}
		}
	}
	return errors.Join(errs...)
}

func checkBaseURL(raw string, guard netguard.Policy) error {
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https":
		return errors.New("must be an absolute http or https URL")
	case u.User != nil:
		return errors.New("must not contain credentials; use api_key_env")
	case u.RawQuery != "" || u.Fragment != "":
		return errors.New("must not have a query or fragment")
	}
	return guard.CheckHost(u.Hostname())
}
