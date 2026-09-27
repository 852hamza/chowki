package catalog

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultCatalogIsValid(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("the embedded catalog is invalid: %v", err)
	}
	if c.Len() == 0 {
		t.Fatal("the embedded catalog is empty")
	}
	// An alias finds the same entry as the ID.
	byID, ok1 := c.Find("anthropic", "claude-haiku-4-5-20251001")
	byAlias, ok2 := c.Find("anthropic", "claude-haiku-4-5")
	if !ok1 || !ok2 || byID != byAlias {
		t.Errorf("Find() by ID and alias = %p, %p", byID, byAlias)
	}
	if _, ok := c.Find("openai", "claude-haiku-4-5"); ok {
		t.Error("Find() matched a model of another provider")
	}
	if got := c.Providers("claude-haiku-4-5"); len(got) != 1 || got[0] != "anthropic" {
		t.Errorf("Providers(alias) = %q, want [anthropic]", got)
	}
	if got := c.Providers("no-such-model"); got != nil {
		t.Errorf("Providers(unknown) = %q, want none", got)
	}
}

func TestPriceFor(t *testing.T) {
	m := Model{
		Price: Price{Input: 1, Output: 2},
		Tiers: []Tier{
			{AboveInputTokens: 1000, Price: Price{Input: 3, Output: 4}},
			{AboveInputTokens: 5000, Price: Price{Input: 5, Output: 6}},
		},
	}
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	for tokens, want := range map[int64]float64{0: 1, 1000: 1, 1001: 3, 5000: 3, 5001: 5, 1 << 40: 5} {
		if got := m.PriceFor(tokens, now).Input; got != want {
			t.Errorf("PriceFor(%d).Input = %v, want %v", tokens, got, want)
		}
	}

	m.Changes = []Change{{From: "2027-01-01", Price: Price{Input: 10, Output: 20}},
		{From: "2027-06-01", Price: Price{Input: 30, Output: 40}, Tiers: []Tier{{AboveInputTokens: 10, Price: Price{Input: 50}}}}}
	for _, tc := range []struct {
		at     time.Time
		tokens int64
		want   float64
	}{
		{now, 2000, 3},
		{time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC), 0, 1},
		{time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 2000, 10},
		{time.Date(2027, 1, 1, 0, 30, 0, 0, time.FixedZone("PKT", 5*3600)), 0, 1}, // still 2026 in UTC
		{time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC), 5, 30},
		{time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC), 11, 50},
	} {
		if got := m.PriceFor(tc.tokens, tc.at).Input; got != tc.want {
			t.Errorf("PriceFor(%d, %v).Input = %v, want %v", tc.tokens, tc.at, got, tc.want)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	const valid = `{"provider":"p","model":"m","price":{"input":1,"output":2},` +
		`"source":"https://example.com/pricing","updated":"2026-09-27"}`
	tests := []struct{ name, data, want string }{
		{"not JSON", `{`, "parse catalog"},
		{"unknown field", `{"models":[{"provider":"p","model":"m","prize":{}}]}`, "unknown field"},
		{"missing model", `{"models":[` + strings.Replace(valid, `"model":"m"`, `"model":""`, 1) + `]}`,
			"provider and model are required"},
		{"http source", `{"models":[` + strings.Replace(valid, "https://", "http://", 1) + `]}`, "source must be"},
		{"no source", `{"models":[` + strings.Replace(valid, `"source":"https://example.com/pricing",`, "", 1) + `]}`,
			"source must be"},
		{"bad date", `{"models":[` + strings.Replace(valid, "2026-09-27", "yesterday", 1) + `]}`, "updated must be"},
		{"negative price", `{"models":[` + strings.Replace(valid, `"input":1`, `"input":-1`, 1) + `]}`,
			"price.input must be"},
		{"negative cache price", `{"models":[` + strings.Replace(valid, `"output":2`, `"output":2,"cache_read":-0.1`, 1) + `]}`,
			"price.cache_read must be"},
		{"tiers out of order", `{"models":[` + strings.Replace(valid, `"source"`,
			`"tiers":[{"above_input_tokens":500,"input":1,"output":1},{"above_input_tokens":100,"input":1,"output":1}],"source"`, 1) + `]}`,
			"ascending"},
		{"duplicate", `{"models":[` + valid + `,` + valid + `]}`, "p/m is listed twice"},
		{"unknown capability", `{"models":[` + strings.Replace(valid, `"source"`, `"supports":["telepathy"],"source"`, 1) +
			`]}`, "unknown capability"},
		{"negative limit", `{"models":[` + strings.Replace(valid, `"source"`, `"max_output":-1,"source"`, 1) + `]}`,
			"must not be negative"},
		{"change before updated", `{"models":[` + strings.Replace(valid, `"source"`,
			`"changes":[{"from":"2026-01-01","price":{"input":1,"output":1}}],"source"`, 1) + `]}`, "changes[0].from must be"},
		{"changes out of order", `{"models":[` + strings.Replace(valid, `"source"`,
			`"changes":[{"from":"2027-06-01","price":{"input":1,"output":1}},{"from":"2027-01-01","price":{"input":1,"output":1}}],"source"`, 1) + `]}`,
			"changes[1].from must be"},
		{"negative changed price", `{"models":[` + strings.Replace(valid, `"source"`,
			`"changes":[{"from":"2027-01-01","price":{"input":-1,"output":1}}],"source"`, 1) + `]}`, "changes[0].price.input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load([]byte(tt.data)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load() error = %v, want %q", err, tt.want)
			}
		})
	}
	if c, err := Load([]byte(`{"models":[` + valid + `]}`)); err != nil || c.Len() != 1 {
		t.Errorf("Load(valid) = %v, %v", c, err)
	}
}

func TestAll(t *testing.T) {
	c, err := Load([]byte(`{"models":[
 {"provider":"b","model":"m","price":{"input":1,"output":1},"source":"https://example.com","updated":"2026-09-27"},
 {"provider":"a","model":"z","aliases":["y"],"price":{"input":1,"output":1},"source":"https://example.com","updated":"2026-09-27"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	all := c.All()
	if len(all) != 2 || all[0].Provider != "a" || all[1].Provider != "b" {
		t.Errorf("All() = %+v; want each model once, by provider", all)
	}
}

// The embedded Gemini prices: long-context tiers, an announced price change
// and an alias.
func TestDefaultGeminiPrices(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	pro, ok := c.Find("gemini", "gemini-2.5-pro")
	if !ok || pro.PriceFor(200000, now).Input != 1.25 || pro.PriceFor(200001, now).Output != 15 {
		t.Errorf("gemini-2.5-pro = %+v", pro)
	}
	flash, ok := c.Find("gemini", "gemini-3.7-flash")
	if !ok || flash.PriceFor(0, now).Output != 3.75 ||
		flash.PriceFor(0, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)).Output != 7.5 {
		t.Errorf("gemini-3.7-flash = %+v", flash)
	}
	if _, ok := c.Find("gemini", "gemini-3.1-pro-preview-customtools"); !ok {
		t.Error("the alias of gemini-3.1-pro-preview is missing")
	}
	// Every chat model says how much it can write: translation for
	// Anthropic needs it.
	for _, m := range c.All() {
		if m.MaxOutput == 0 && !strings.Contains(m.Model, "embedding") {
			t.Errorf("%s/%s has no max_output", m.Provider, m.Model)
		}
	}
}
