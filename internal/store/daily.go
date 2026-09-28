package store

import (
	"context"
	"database/sql"
	"time"
)

// daily sums requests by day in UTC, as the tables usage_daily,
// savings_daily and redactions_daily keep them.
type daily struct {
	usage      map[usageGroup]*usageSums
	savings    map[[2]string]float64 // by day and method
	redactions map[[2]string]int64   // by day and type
}

type usageGroup struct {
	day             string
	key             int64
	provider, model string
}

type usageSums struct {
	requests, errors, unpriced int64
	cost, savings              float64
	tokens                     Tokens
	hits, misses               int64
}

func sumDaily(rs []Request) daily {
	d := daily{usage: map[usageGroup]*usageSums{}, savings: map[[2]string]float64{}, redactions: map[[2]string]int64{}}
	for _, r := range rs {
		day := r.Time.UTC().Format(time.DateOnly)
		g := usageGroup{day, r.KeyID, r.Provider, r.Model}
		u := d.usage[g]
		if u == nil {
			u = &usageSums{}
			d.usage[g] = u
		}
		u.requests++
		if r.Status >= 400 {
			u.errors++
		}
		switch {
		case r.CostUSD != nil:
			u.cost += *r.CostUSD
		case r.Tokens != nil:
			u.unpriced++
		}
		u.savings += r.SavingsUSD
		if t := r.Tokens; t != nil {
			u.tokens.Input += t.Input
			u.tokens.Output += t.Output
			u.tokens.CacheRead += t.CacheRead
			u.tokens.CacheWrite += t.CacheWrite
			u.tokens.Reasoning += t.Reasoning
		}
		switch r.CacheStatus {
		case "hit":
			u.hits++
		case "miss":
			u.misses++
		}
		if r.SavingsMethod != "" {
			d.savings[[2]string{day, r.SavingsMethod}] += r.SavingsUSD
		}
		for typ, n := range r.Redactions {
			d.redactions[[2]string{day, typ}] += int64(n)
		}
	}
	return d
}

// save adds the sums to the tables.
func (d daily) save(ctx context.Context, tx *sql.Tx) error {
	usage, err := tx.PrepareContext(ctx, `INSERT INTO usage_daily (day, key_id, provider, model, requests, errors,
		unpriced, cost_usd, savings_usd, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
		reasoning_tokens, cache_hits, cache_misses) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (day, key_id, provider, model) DO UPDATE SET requests = requests + excluded.requests,
		errors = errors + excluded.errors, unpriced = unpriced + excluded.unpriced,
		cost_usd = cost_usd + excluded.cost_usd, savings_usd = savings_usd + excluded.savings_usd,
		input_tokens = input_tokens + excluded.input_tokens, output_tokens = output_tokens + excluded.output_tokens,
		cache_read_tokens = cache_read_tokens + excluded.cache_read_tokens,
		cache_write_tokens = cache_write_tokens + excluded.cache_write_tokens,
		reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens,
		cache_hits = cache_hits + excluded.cache_hits, cache_misses = cache_misses + excluded.cache_misses`)
	if err != nil {
		return err
	}
	defer func() { _ = usage.Close() }()
	for g, u := range d.usage {
		if _, err := usage.ExecContext(ctx, g.day, g.key, g.provider, g.model, u.requests, u.errors, u.unpriced,
			u.cost, u.savings, u.tokens.Input, u.tokens.Output, u.tokens.CacheRead, u.tokens.CacheWrite,
			u.tokens.Reasoning, u.hits, u.misses); err != nil {
			return err
		}
	}
	for k, usd := range d.savings {
		if _, err := tx.ExecContext(ctx, `INSERT INTO savings_daily (day, method, usd) VALUES (?, ?, ?)
			ON CONFLICT (day, method) DO UPDATE SET usd = usd + excluded.usd`, k[0], k[1], usd); err != nil {
			return err
		}
	}
	for k, n := range d.redactions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO redactions_daily (day, type, count) VALUES (?, ?, ?)
			ON CONFLICT (day, type) DO UPDATE SET count = count + excluded.count`, k[0], k[1], n); err != nil {
			return err
		}
	}
	return nil
}
