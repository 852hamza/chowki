package usage

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/catalog"
)

func ptr(v float64) *float64 { return &v }

// Prices per million tokens, chosen so each formula gives round numbers.
var (
	openAIModel = &catalog.Model{
		Price: catalog.Price{Input: 2, Output: 10, CacheRead: ptr(0.2), CacheWrite: ptr(2.5)},
		Tiers: []catalog.Tier{{AboveInputTokens: 272000,
			Price: catalog.Price{Input: 4, Output: 15, CacheRead: ptr(0.4), CacheWrite: ptr(5)}}},
	}
	openAINoWritePrice = &catalog.Model{Price: catalog.Price{Input: 2, Output: 10, CacheRead: ptr(0.2)}}
	anthropicModel     = &catalog.Model{
		Price: catalog.Price{Input: 4, Output: 20, CacheRead: ptr(0.2), CacheWrite: ptr(5), CacheWrite1h: ptr(8)},
	}
	anthropicNo1h = &catalog.Model{Price: catalog.Price{Input: 4, Output: 20, CacheRead: ptr(0.2), CacheWrite: ptr(5)}}
	// The prices of gemini-2.5-pro, with a price change like Gemini 3.7
	// Flash's.
	geminiModel = &catalog.Model{
		Price:   catalog.Price{Input: 1.25, Output: 10, CacheRead: ptr(0.125)},
		Tiers:   []catalog.Tier{{AboveInputTokens: 200000, Price: catalog.Price{Input: 2.5, Output: 15, CacheRead: ptr(0.25)}}},
		Changes: []catalog.Change{{From: "2027-01-01", Price: catalog.Price{Input: 3, Output: 30, CacheRead: ptr(0.3)}}},
	}
)

var now = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func TestCompute(t *testing.T) {
	tests := []struct {
		name     string
		family   Family
		usage    *Usage
		modifier string
		model    *catalog.Model
		cost     float64 // ignored when reason is set
		savings  float64
		method   string
		reason   string
	}{
		// OpenAI: uncached × input + cache reads × cache_read + cache writes × cache_write + output × output.
		{"openai plain", OpenAI, &Usage{Input: 1000, Output: 500}, "", openAIModel,
			(1000*2 + 500*10) / 1e6, 0, "", ""},
		{"openai cache read", OpenAI, &Usage{Input: 1000, CacheRead: 600, Output: 100}, "", openAIModel,
			(400*2 + 600*0.2 + 100*10) / 1e6, 600 * (2 - 0.2) / 1e6, SavingsPromptCache, ""},
		{"openai cache write costs extra", OpenAI, &Usage{Input: 1000, CacheWrite: 1000}, "", openAIModel,
			1000 * 2.5 / 1e6, -(1000*2.5 - 1000*2) / 1e6, SavingsPromptCache, ""},
		{"openai write without a write price", OpenAI, &Usage{Input: 1000, CacheWrite: 1000}, "", openAINoWritePrice,
			1000 * 2 / 1e6, 0, SavingsPromptCache, ""},
		{"openai reasoning is inside output", OpenAI, &Usage{Input: 10, Output: 100, Reasoning: 80}, "", openAIModel,
			(10*2 + 100*10) / 1e6, 0, "", ""},
		{"openai long context tier", OpenAI, &Usage{Input: 300000, Output: 1000}, "", openAIModel,
			(300000*4 + 1000*15) / 1e6, 0, "", ""},
		{"openai at the tier threshold", OpenAI, &Usage{Input: 272000}, "", openAIModel, 272000 * 2 / 1e6, 0, "", ""},

		// Anthropic: input_tokens × input + writes (5m and 1h) + reads × cache_read + output × output.
		{"anthropic 5-minute writes", Anthropic, &Usage{Input: 3100, CacheRead: 1000, CacheWrite: 2000, Output: 500}, "",
			anthropicModel, (100*4 + 1000*0.2 + 2000*5 + 500*20) / 1e6,
			(1000*(4-0.2) - (2000*5 - 2000*4)) / 1e6, SavingsPromptCache, ""},
		{"anthropic 1-hour writes", Anthropic,
			&Usage{Input: 3100, CacheRead: 1000, CacheWrite: 2000, CacheWrite1h: 500, Output: 500}, "", anthropicModel,
			(100*4 + 1000*0.2 + 1500*5 + 500*8 + 500*20) / 1e6,
			(1000*(4-0.2) - (1500*5 + 500*8 - 2000*4)) / 1e6, SavingsPromptCache, ""},
		{"anthropic writes only: negative savings", Anthropic, &Usage{Input: 2000, CacheWrite: 2000}, "", anthropicModel,
			2000 * 5 / 1e6, -(2000*5 - 2000*4) / 1e6, SavingsPromptCache, ""},

		// Gemini: (prompt − cached) × input + cached × cache_read + (candidates + thoughts) × output. The
		// parser adds the thoughts to Output; the tier depends on the whole prompt, cached tokens included.
		{"gemini plain", Gemini, &Usage{Input: 1000, Output: 500, Reasoning: 200}, "", geminiModel,
			(1000*1.25 + 500*10) / 1e6, 0, "", ""},
		{"gemini implicit cache", Gemini, &Usage{Input: 1000, CacheRead: 400, Output: 100}, "", geminiModel,
			(600*1.25 + 400*0.125 + 100*10) / 1e6, 400 * (1.25 - 0.125) / 1e6, SavingsPromptCache, ""},
		{"gemini long context", Gemini, &Usage{Input: 250000, CacheRead: 100000, Output: 1000}, "", geminiModel,
			(150000*2.5 + 100000*0.25 + 1000*15) / 1e6, 100000 * (2.5 - 0.25) / 1e6, SavingsPromptCache, ""},

		{"no usage", OpenAI, nil, "", openAIModel, 0, 0, "", "reported no usage"},
		{"unknown model", OpenAI, &Usage{Input: 1}, "", nil, 0, 0, "", "isn't in the catalog"},
		{"modifier", Anthropic, &Usage{Input: 1}, "fast mode", anthropicModel, 0, 0, "", "fast mode isn't priced yet"},
		{"inconsistent", OpenAI, &Usage{Input: 10, CacheRead: 20}, "", openAIModel, 0, 0, "", "inconsistent"},
		{"1h larger than all writes", Anthropic, &Usage{Input: 10, CacheWrite: 1, CacheWrite1h: 2}, "", anthropicModel,
			0, 0, "", "inconsistent"},
		{"no cache read price", OpenAI, &Usage{Input: 10, CacheRead: 5}, "", &catalog.Model{}, 0, 0, "", "no cache read price"},
		{"no 1-hour write price", Anthropic, &Usage{Input: 10, CacheWrite: 5, CacheWrite1h: 5}, "", anthropicNo1h,
			0, 0, "", "no cache write price"},
		{"unknown family", "cohere", &Usage{Input: 10}, "", openAIModel, 0, 0, "", "unknown API family"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Compute(tt.family, Report{Usage: tt.usage, Modifier: tt.modifier}, tt.model, now)
			if tt.reason != "" {
				if c.USD != nil || !strings.Contains(c.Reason, tt.reason) {
					t.Errorf("Compute() = %+v, want no cost because %q", c, tt.reason)
				}
				return
			}
			if c.USD == nil {
				t.Fatalf("Compute() = no cost (%s), want %v", c.Reason, tt.cost)
			}
			if !near(*c.USD, tt.cost) || !near(c.SavingsUSD, tt.savings) || c.SavingsMethod != tt.method {
				t.Errorf("Compute() = cost %v, savings %v (%q); want %v, %v (%q)",
					*c.USD, c.SavingsUSD, c.SavingsMethod, tt.cost, tt.savings, tt.method)
			}
		})
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestComputePriceChange(t *testing.T) {
	r := Report{Usage: &Usage{Input: 1000, Output: 100}}
	for at, want := range map[time.Time]float64{
		now: (1000*1.25 + 100*10) / 1e6,
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC): (1000*3 + 100*30) / 1e6,
	} {
		if c := Compute(Gemini, r, geminiModel, at); c.USD == nil || !near(*c.USD, want) {
			t.Errorf("Compute() at %v = %+v, want %v", at, c, want)
		}
	}
}
