package usage

import (
	"github.com/852hamza/chowki/internal/catalog"
)

// SavingsPromptCache is the savings method of provider prompt caching.
const SavingsPromptCache = "prompt_cache"

// Cost is the price of one request.
type Cost struct {
	// USD is nil when the request can't be priced; Reason says why.
	USD    *float64
	Reason string
	// SavingsUSD is the net saving from provider prompt caching, which is
	// negative when cache writes cost more than the reads saved.
	SavingsUSD    float64
	SavingsMethod string
}

// Compute prices a request that model m served, with the formulas in the
// architecture document (section 8). m is nil for a model that isn't in the
// catalog. Prices are per million tokens.
func Compute(f Family, r Report, m *catalog.Model) Cost {
	switch {
	case r.Usage == nil:
		return Cost{Reason: "the provider reported no usage"}
	case m == nil:
		return Cost{Reason: "the model isn't in the catalog"}
	case r.Modifier != "":
		return Cost{Reason: r.Modifier + " isn't priced yet"}
	}
	u := *r.Usage
	uncached := u.Input - u.CacheRead - u.CacheWrite
	if uncached < 0 || u.CacheWrite1h > u.CacheWrite {
		return Cost{Reason: "the usage numbers are inconsistent"}
	}
	p := m.PriceFor(u.Input)
	if u.CacheRead > 0 && p.CacheRead == nil {
		return Cost{Reason: "the catalog lists no cache read price"}
	}
	cacheRead := value(p.CacheRead)

	var writes float64 // cost of the cache writes, in tokens × price
	switch f {
	case OpenAI:
		// Without a cache write price, writes cost the input price, as for
		// OpenAI models before GPT-5.6.
		writes = float64(u.CacheWrite) * valueOr(p.CacheWrite, p.Input)
	case Anthropic:
		w5m, w1h := u.CacheWrite-u.CacheWrite1h, u.CacheWrite1h
		if w5m > 0 && p.CacheWrite == nil || w1h > 0 && p.CacheWrite1h == nil {
			return Cost{Reason: "the catalog lists no cache write price"}
		}
		writes = float64(w5m)*value(p.CacheWrite) + float64(w1h)*value(p.CacheWrite1h)
	default:
		return Cost{Reason: "unknown API family " + string(f)}
	}

	usd := (float64(uncached)*p.Input + float64(u.CacheRead)*cacheRead + writes + float64(u.Output)*p.Output) / 1e6
	c := Cost{USD: &usd}
	if u.CacheRead > 0 || u.CacheWrite > 0 {
		c.SavingsUSD = (float64(u.CacheRead)*(p.Input-cacheRead) - (writes - float64(u.CacheWrite)*p.Input)) / 1e6
		c.SavingsMethod = SavingsPromptCache
	}
	return c
}

func value(p *float64) float64 { return valueOr(p, 0) }

func valueOr(p *float64, fallback float64) float64 {
	if p == nil {
		return fallback
	}
	return *p
}
