package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// CreateAdminToken implements Store.
func (s *SQLite) CreateAdminToken(ctx context.Context, t AdminToken) (AdminToken, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO admin_tokens (name, prefix, token_hash, created_at)
		VALUES (?, ?, ?, ?)`, t.Name, t.Prefix, t.Hash[:], t.CreatedAt.UnixMilli())
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		return AdminToken{}, fmt.Errorf("create admin token: prefix %s: %w", t.Prefix, ErrExists)
	}
	if err != nil {
		return AdminToken{}, fmt.Errorf("create admin token: %w", err)
	}
	if t.ID, err = res.LastInsertId(); err != nil {
		return AdminToken{}, fmt.Errorf("create admin token: %w", err)
	}
	return s.AdminTokenByPrefix(ctx, t.Prefix)
}

const adminColumns = `SELECT id, name, prefix, token_hash, created_at, revoked_at FROM admin_tokens`

// AdminTokenByPrefix implements Store.
func (s *SQLite) AdminTokenByPrefix(ctx context.Context, prefix string) (AdminToken, error) {
	t, err := scanAdminToken(s.db.QueryRowContext(ctx, adminColumns+" WHERE prefix = ?", prefix))
	if errors.Is(err, sql.ErrNoRows) {
		return AdminToken{}, ErrNotFound
	}
	if err != nil {
		return AdminToken{}, fmt.Errorf("read admin token: %w", err)
	}
	return t, nil
}

// ListAdminTokens implements Store.
func (s *SQLite) ListAdminTokens(ctx context.Context) ([]AdminToken, error) {
	rows, err := s.db.QueryContext(ctx, adminColumns+" ORDER BY created_at, id")
	if err != nil {
		return nil, fmt.Errorf("list admin tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AdminToken
	for rows.Next() {
		t, err := scanAdminToken(rows)
		if err != nil {
			return nil, fmt.Errorf("list admin tokens: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list admin tokens: %w", err)
	}
	return out, nil
}

func scanAdminToken(row interface{ Scan(...any) error }) (AdminToken, error) {
	var t AdminToken
	var hash []byte
	var created int64
	var revoked sql.NullInt64
	if err := row.Scan(&t.ID, &t.Name, &t.Prefix, &hash, &created, &revoked); err != nil {
		return AdminToken{}, err
	}
	if len(hash) != len(t.Hash) {
		return AdminToken{}, fmt.Errorf("admin token %s has a %d-byte hash", t.Prefix, len(hash))
	}
	copy(t.Hash[:], hash)
	t.CreatedAt = time.UnixMilli(created).UTC()
	if revoked.Valid {
		t.RevokedAt = time.UnixMilli(revoked.Int64).UTC()
	}
	return t, nil
}

// RevokeAdminToken implements Store.
func (s *SQLite) RevokeAdminToken(ctx context.Context, prefix string, at time.Time) (AdminToken, error) {
	if _, err := s.db.ExecContext(ctx, `UPDATE admin_tokens SET revoked_at = ?
		WHERE prefix = ? AND revoked_at IS NULL`, at.UnixMilli(), prefix); err != nil {
		return AdminToken{}, fmt.Errorf("revoke admin token: %w", err)
	}
	return s.AdminTokenByPrefix(ctx, prefix)
}

// reportDays returns the dates of the first day in UTC that [from, to)
// touches and of the day after the last one.
func reportDays(from, to time.Time) (first, end string) {
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
	stop := day(to.UTC())
	if stop.Before(to) {
		stop = stop.AddDate(0, 0, 1)
	}
	return day(from.UTC()).Format(time.DateOnly), stop.Format(time.DateOnly)
}

// Totals implements Store.
func (s *SQLite) Totals(ctx context.Context, from, to time.Time) (Totals, error) {
	t := Totals{SavingsUSD: map[string]float64{}, Redactions: map[string]int64{}}
	first, end := reportDays(from, to)
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(sum(requests), 0), coalesce(sum(errors), 0),
		coalesce(sum(unpriced), 0), coalesce(sum(cost_usd), 0), coalesce(sum(input_tokens), 0),
		coalesce(sum(output_tokens), 0), coalesce(sum(cache_read_tokens), 0), coalesce(sum(cache_write_tokens), 0),
		coalesce(sum(reasoning_tokens), 0), coalesce(sum(cache_hits), 0), coalesce(sum(cache_misses), 0)
		FROM usage_daily WHERE day >= ? AND day < ?`, first, end).
		Scan(&t.Requests, &t.Errors, &t.Unpriced, &t.CostUSD, &t.Tokens.Input, &t.Tokens.Output, &t.Tokens.CacheRead,
			&t.Tokens.CacheWrite, &t.Tokens.Reasoning, &t.CacheHits, &t.CacheMisses)
	if err != nil {
		return Totals{}, fmt.Errorf("sum requests: %w", err)
	}
	if err := s.sumByName(ctx, `SELECT method, sum(usd) FROM savings_daily WHERE day >= ? AND day < ?
		GROUP BY method`, first, end, func(name string, v float64) { t.SavingsUSD[name] = v }); err != nil {
		return Totals{}, err
	}
	if err := s.sumByName(ctx, `SELECT type, sum(count) FROM redactions_daily WHERE day >= ? AND day < ?
		GROUP BY type`, first, end, func(name string, v float64) { t.Redactions[name] = int64(v) }); err != nil {
		return Totals{}, err
	}
	return t, nil
}

func (s *SQLite) sumByName(ctx context.Context, query, first, end string, add func(string, float64)) error {
	rows, err := s.db.QueryContext(ctx, query, first, end)
	if err != nil {
		return fmt.Errorf("sum requests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var v float64
		if err := rows.Scan(&name, &v); err != nil {
			return fmt.Errorf("sum requests: %w", err)
		}
		add(name, v)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sum requests: %w", err)
	}
	return nil
}

// breakdowns are the queries of Breakdown: the ID and label of each group,
// the grouping and the order.
var breakdowns = map[string]string{
	ByKey: `SELECT coalesce(k.prefix, ''), coalesce(k.name, ''), %s FROM usage_daily u
		LEFT JOIN virtual_keys k ON k.id = u.key_id WHERE u.day >= ? AND u.day < ?
		GROUP BY u.key_id ORDER BY 4 DESC, 1`,
	ByModel: `SELECT CASE WHEN u.provider = '' THEN '' ELSE u.provider || '/' || u.model END, '', %s
		FROM usage_daily u WHERE u.day >= ? AND u.day < ? GROUP BY 1 ORDER BY 4 DESC, 1`,
	ByDay: `SELECT u.day, '', %s FROM usage_daily u WHERE u.day >= ? AND u.day < ? GROUP BY 1 ORDER BY 1`,
}

// Breakdown implements Store.
func (s *SQLite) Breakdown(ctx context.Context, by string, from, to time.Time) ([]Group, error) {
	query, ok := breakdowns[by]
	if !ok {
		return nil, fmt.Errorf("break down requests: unknown grouping %q", by)
	}
	first, end := reportDays(from, to)
	sums := `sum(u.requests), sum(u.cost_usd), sum(u.savings_usd), sum(u.input_tokens), sum(u.output_tokens)`
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(query, sums), first, end)
	if err != nil {
		return nil, fmt.Errorf("break down requests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Label, &g.Requests, &g.CostUSD, &g.SavingsUSD, &g.InputTokens,
			&g.OutputTokens); err != nil {
			return nil, fmt.Errorf("break down requests: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("break down requests: %w", err)
	}
	return out, nil
}

// requestColumns are the columns of a request record, in the order that
// queryRequests scans them.
const requestColumns = `id, ts, key_id, project_id, api_family, endpoint, provider, model, stream, status,
	error_type, latency_ms, ttfb_ms, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
	reasoning_tokens, cost_usd, savings_usd, savings_method, cache_status, redactions`

// RecentRequests implements Store.
func (s *SQLite) RecentRequests(ctx context.Context, before time.Time, limit int) ([]Request, error) {
	return s.queryRequests(ctx, `SELECT `+requestColumns+` FROM requests WHERE ts < ? ORDER BY ts DESC, id LIMIT ?`,
		before.UnixMilli(), limit)
}

// ListRequests implements Store.
func (s *SQLite) ListRequests(ctx context.Context, f RequestFilter) ([]Request, error) {
	var where []string
	var args []any
	add := func(clause string, values ...any) {
		where = append(where, clause)
		args = append(args, values...)
	}
	if f.KeyID != 0 {
		add("key_id = ?", f.KeyID)
	}
	if f.Provider != "" {
		add("provider = ?", f.Provider)
	}
	if f.Model != "" {
		add("model = ?", f.Model)
	}
	switch f.Status {
	case StatusFailed:
		add("status >= 400")
	case StatusSucceeded:
		add("status < 400")
	}
	if f.Before != nil {
		ms := f.Before.Time.UnixMilli()
		// ts <= ? gives SQLite a range of the ts index to read; without
		// it, the OR makes it scan every record.
		add("ts <= ? AND (ts < ? OR id < ?)", ms, ms, f.Before.ID)
	}
	query := `SELECT ` + requestColumns + ` FROM requests`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	// The clauses are constants; every value is a bound argument.
	return s.queryRequests(ctx, query+` ORDER BY ts DESC, id DESC LIMIT ?`, append(args, f.Limit)...)
}

func (s *SQLite) queryRequests(ctx context.Context, query string, args ...any) ([]Request, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read requests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Request
	for rows.Next() {
		var r Request
		var ts, latency int64
		var ttfb, input, output, cacheRead, cacheWrite, reasoning sql.NullInt64
		var cost sql.NullFloat64
		var redactions string
		if err := rows.Scan(&r.ID, &ts, &r.KeyID, &r.ProjectID, &r.APIFamily, &r.Endpoint, &r.Provider, &r.Model,
			&r.Stream, &r.Status, &r.ErrorType, &latency, &ttfb, &input, &output, &cacheRead, &cacheWrite,
			&reasoning, &cost, &r.SavingsUSD, &r.SavingsMethod, &r.CacheStatus, &redactions); err != nil {
			return nil, fmt.Errorf("read requests: %w", err)
		}
		r.Time = time.UnixMilli(ts).UTC()
		r.Latency, r.TTFB = time.Duration(latency)*time.Millisecond, time.Duration(ttfb.Int64)*time.Millisecond
		if input.Valid {
			r.Tokens = &Tokens{Input: input.Int64, Output: output.Int64, CacheRead: cacheRead.Int64,
				CacheWrite: cacheWrite.Int64, Reasoning: reasoning.Int64}
		}
		if cost.Valid {
			r.CostUSD = &cost.Float64
		}
		if redactions != "" {
			if err := json.Unmarshal([]byte(redactions), &r.Redactions); err != nil {
				return nil, fmt.Errorf("read requests: request %s: %w", r.ID, err)
			}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read requests: %w", err)
	}
	return out, nil
}

// ProviderStats implements Store. It reads the request records, not the
// daily sums, because percentiles don't add up. Answers from the exact
// cache, and requests that the gateway refused after routing them, such as
// over a budget, never reached the provider, and don't count.
func (s *SQLite) ProviderStats(ctx context.Context, from, to time.Time) ([]ProviderStat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider, status, error_type, latency_ms FROM requests
		WHERE ts >= ? AND ts < ? AND provider != '' AND cache_status != 'hit'`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read provider stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	stats := map[string]*ProviderStat{}
	latencies := map[string][]int64{}
	for rows.Next() {
		var provider, errType string
		var status int
		var latency int64
		if err := rows.Scan(&provider, &status, &errType, &latency); err != nil {
			return nil, fmt.Errorf("read provider stats: %w", err)
		}
		upstream := strings.HasPrefix(errType, "upstream_")
		if !upstream && (status >= 400 || errType != "") {
			continue // the gateway refused it
		}
		st := stats[provider]
		if st == nil {
			st = &ProviderStat{Provider: provider}
			stats[provider] = st
		}
		st.Requests++
		switch {
		case providerFailed(errType):
			st.Failed++
		case !upstream:
			latencies[provider] = append(latencies[provider], latency)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read provider stats: %w", err)
	}
	out := make([]ProviderStat, 0, len(stats))
	for name, st := range stats {
		ls := latencies[name]
		slices.Sort(ls)
		st.P50, st.P95 = nearestRank(ls, 0.50), nearestRank(ls, 0.95)
		out = append(out, *st)
	}
	slices.SortFunc(out, func(a, b ProviderStat) int {
		return cmp.Or(cmp.Compare(b.Requests, a.Requests), strings.Compare(a.Provider, b.Provider))
	})
	return out, nil
}

// providerFailed reports whether a request's error type is a failure of its
// provider: no answer, a broken one, or a status of 429 or 5xx. Its other
// 4xx statuses reject the request itself, as the gateway's own errors do.
func providerFailed(errType string) bool {
	rest, ok := strings.CutPrefix(errType, "upstream_")
	if !ok {
		return false
	}
	if status, err := strconv.Atoi(rest); err == nil {
		return status == 429 || status >= 500
	}
	return true
}

// nearestRank returns the q-quantile of sorted latencies in milliseconds,
// or 0 when there are none.
func nearestRank(sorted []int64, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := max(int(math.Ceil(q*float64(len(sorted))))-1, 0)
	return time.Duration(sorted[i]) * time.Millisecond
}
