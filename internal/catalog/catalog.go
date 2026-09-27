package catalog

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	embedded "github.com/852hamza/chowki/catalog"
)

// Catalog is a validated set of models with prices.
type Catalog struct {
	models map[string]*Model // by provider + "/" + model ID or alias
}

// Model is a catalog entry.
type Model struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Aliases  []string `json:"aliases,omitempty"`
	// ContextWindow, MaxOutput and MinCacheableTokens are 0 when unknown.
	ContextWindow      int      `json:"context_window,omitempty"`
	MaxOutput          int      `json:"max_output,omitempty"`
	MinCacheableTokens int      `json:"min_cacheable_tokens,omitempty"`
	Supports           []string `json:"supports,omitempty"`
	Price              Price    `json:"price"`
	// Tiers are prices for large prompts, in ascending order of threshold.
	Tiers []Tier `json:"tiers,omitempty"`
	// Changes are announced prices, in ascending order of date: from its
	// date on, a change's price and tiers replace the earlier ones.
	Changes []Change `json:"changes,omitempty"`
	// Source is the official page that lists the prices.
	Source string `json:"source"`
	// Updated is the date the prices were checked, as YYYY-MM-DD.
	Updated string `json:"updated"`
}

// Price is in USD per million tokens. A nil price isn't listed.
type Price struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
	// CacheRead is the price of tokens read from the prompt cache.
	CacheRead *float64 `json:"cache_read,omitempty"`
	// CacheWrite is the price of tokens written to the prompt cache; for
	// Anthropic, to the 5-minute cache.
	CacheWrite *float64 `json:"cache_write,omitempty"`
	// CacheWrite1h is Anthropic's price for writes to the 1-hour cache.
	CacheWrite1h *float64 `json:"cache_write_1h,omitempty"`
}

// Tier is the price for requests with more than AboveInputTokens input
// tokens.
type Tier struct {
	AboveInputTokens int64 `json:"above_input_tokens"`
	Price
}

// Change is a price that applies from a date on.
type Change struct {
	// From is the first day in UTC of the price, as YYYY-MM-DD.
	From  string `json:"from"`
	Price Price  `json:"price"`
	Tiers []Tier `json:"tiers,omitempty"`
}

// PriceFor returns the price of a request made at a time, with the given
// total input tokens: of the prices in effect then, the highest tier whose
// threshold the request exceeds, or the base price.
func (m *Model) PriceFor(inputTokens int64, at time.Time) Price {
	price, tiers := m.Price, m.Tiers
	day := at.UTC().Format(time.DateOnly)
	for _, c := range m.Changes {
		if day >= c.From {
			price, tiers = c.Price, c.Tiers
		}
	}
	for _, t := range tiers {
		if inputTokens > t.AboveInputTokens {
			price = t.Price
		}
	}
	return price
}

// Find returns the model of a provider by its ID or an alias.
func (c *Catalog) Find(provider, model string) (*Model, bool) {
	m, ok := c.models[provider+"/"+model]
	return m, ok
}

// Providers returns the providers that list model, by ID or alias, in
// alphabetical order.
func (c *Catalog) Providers(model string) []string {
	var names []string
	for _, m := range c.models {
		if (m.Model == model || slices.Contains(m.Aliases, model)) && !slices.Contains(names, m.Provider) {
			names = append(names, m.Provider)
		}
	}
	slices.Sort(names)
	return names
}

// All returns every model in the catalog, by provider and model ID.
func (c *Catalog) All() []*Model {
	seen := map[*Model]bool{}
	var out []*Model
	for _, m := range c.models {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b *Model) int {
		return cmp.Or(strings.Compare(a.Provider, b.Provider), strings.Compare(a.Model, b.Model))
	})
	return out
}

// Len returns the number of models in the catalog.
func (c *Catalog) Len() int {
	seen := map[*Model]bool{}
	for _, m := range c.models {
		seen[m] = true
	}
	return len(seen)
}

var defaultCatalog = sync.OnceValues(func() (*Catalog, error) { return Load(embedded.ModelsJSON) })

// Default returns the catalog embedded in the binary.
func Default() (*Catalog, error) { return defaultCatalog() }

// Load parses a catalog file and validates every entry.
func Load(data []byte) (*Catalog, error) {
	var file struct {
		Models []*Model `json:"models"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}
	c := &Catalog{models: map[string]*Model{}}
	var errs []error
	for i, m := range file.Models {
		if err := m.validate(); err != nil {
			errs = append(errs, fmt.Errorf("catalog entry %d (%s/%s): %w", i, m.Provider, m.Model, err))
			continue
		}
		for _, name := range append([]string{m.Model}, m.Aliases...) {
			key := m.Provider + "/" + name
			if _, dup := c.models[key]; dup {
				errs = append(errs, fmt.Errorf("catalog entry %d: %s is listed twice", i, key))
			}
			c.models[key] = m
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return c, nil
}

func (m *Model) validate() error {
	var errs []error
	if m.Provider == "" || m.Model == "" {
		errs = append(errs, errors.New("provider and model are required"))
	}
	if u, err := url.Parse(m.Source); err != nil || u.Scheme != "https" || u.Host == "" {
		errs = append(errs, errors.New("source must be the https URL of the official pricing page"))
	}
	if _, err := time.Parse(time.DateOnly, m.Updated); err != nil {
		errs = append(errs, errors.New("updated must be a date such as 2026-09-27"))
	}
	errs = append(errs, m.Price.validate("price"), validateTiers("tiers", m.Tiers))
	last := m.Updated
	for i, c := range m.Changes {
		field := fmt.Sprintf("changes[%d]", i)
		if _, err := time.Parse(time.DateOnly, c.From); err != nil || c.From <= last {
			errs = append(errs, fmt.Errorf("%s.from must be a date after updated and the changes before it", field))
		}
		last = c.From
		errs = append(errs, c.Price.validate(field+".price"), validateTiers(field+".tiers", c.Tiers))
	}
	return errors.Join(errs...)
}

func validateTiers(field string, tiers []Tier) error {
	var errs []error
	var last int64
	for i, t := range tiers {
		if t.AboveInputTokens <= last {
			errs = append(errs, fmt.Errorf("%s[%d]: thresholds must be positive and ascending", field, i))
		}
		last = t.AboveInputTokens
		errs = append(errs, t.validate(fmt.Sprintf("%s[%d]", field, i)))
	}
	return errors.Join(errs...)
}

func (p Price) validate(field string) error {
	for name, v := range map[string]*float64{
		"input": &p.Input, "output": &p.Output, "cache_read": p.CacheRead, "cache_write": p.CacheWrite,
		"cache_write_1h": p.CacheWrite1h,
	} {
		if v != nil && (*v < 0 || math.IsNaN(*v) || math.IsInf(*v, 0)) {
			return fmt.Errorf("%s.%s must be a price of at least 0", field, name)
		}
	}
	return nil
}
