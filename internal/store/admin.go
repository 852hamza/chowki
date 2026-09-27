package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

// RecentRequests implements Store.
func (s *SQLite) RecentRequests(ctx context.Context, before time.Time, limit int) ([]Request, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, ts, key_id, project_id, api_family, endpoint, provider, model,
		stream, status, error_type, latency_ms, ttfb_ms, input_tokens, output_tokens, cache_read_tokens,
		cache_write_tokens, reasoning_tokens, cost_usd, savings_usd, savings_method, cache_status, redactions
		FROM requests WHERE ts < ? ORDER BY ts DESC, id LIMIT ?`, before.UnixMilli(), limit)
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
