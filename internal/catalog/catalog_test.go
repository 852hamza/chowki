package catalog

import (
	"strings"
	"testing"
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
}

func TestPriceFor(t *testing.T) {
	m := Model{
		Price: Price{Input: 1, Output: 2},
		Tiers: []Tier{
			{AboveInputTokens: 1000, Price: Price{Input: 3, Output: 4}},
			{AboveInputTokens: 5000, Price: Price{Input: 5, Output: 6}},
		},
	}
	for tokens, want := range map[int64]float64{0: 1, 1000: 1, 1001: 3, 5000: 3, 5001: 5, 1 << 40: 5} {
		if got := m.PriceFor(tokens).Input; got != want {
			t.Errorf("PriceFor(%d).Input = %v, want %v", tokens, got, want)
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
