package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CacheEntry implements Store.
func (s *SQLite) CacheEntry(ctx context.Context, hash []byte, now time.Time) (CacheEntry, error) {
	e := CacheEntry{Hash: hash}
	var cost sql.NullFloat64
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT ciphertext, content_type, original_cost_usd, created_at, expires_at
		FROM cache_entries WHERE hash = ? AND expires_at > ?`, hash, now.UnixMilli()).
		Scan(&e.Ciphertext, &e.ContentType, &cost, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return CacheEntry{}, ErrNotFound
	}
	if err != nil {
		return CacheEntry{}, fmt.Errorf("read cache entry: %w", err)
	}
	if cost.Valid {
		e.CostUSD = &cost.Float64
	}
	e.CreatedAt, e.ExpiresAt = time.UnixMilli(created).UTC(), time.UnixMilli(expires).UTC()
	return e, nil
}

// PutCacheEntry implements Store.
func (s *SQLite) PutCacheEntry(ctx context.Context, e CacheEntry) (int64, error) {
	var replaced int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `DELETE FROM cache_entries WHERE hash = ? RETURNING size_bytes`, e.Hash).
			Scan(&replaced)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var cost any
		if e.CostUSD != nil {
			cost = *e.CostUSD
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO cache_entries (hash, ciphertext, content_type, original_cost_usd,
			created_at, expires_at, used_at, hits, size_bytes) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
			e.Hash, e.Ciphertext, e.ContentType, cost, e.CreatedAt.UnixMilli(), e.ExpiresAt.UnixMilli(),
			e.CreatedAt.UnixMilli(), e.Size())
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("save cache entry: %w", err)
	}
	return replaced, nil
}

// TouchCacheEntry implements Store.
func (s *SQLite) TouchCacheEntry(ctx context.Context, hash []byte, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE cache_entries SET hits = hits + 1, used_at = max(used_at, ?)
		WHERE hash = ?`, now.UnixMilli(), hash); err != nil {
		return fmt.Errorf("count cache hit: %w", err)
	}
	return nil
}

// DeleteExpiredCacheEntries implements Store.
func (s *SQLite) DeleteExpiredCacheEntries(ctx context.Context, now time.Time) (int64, error) {
	return s.deleteCacheEntries(ctx, `DELETE FROM cache_entries WHERE expires_at <= ? RETURNING size_bytes`,
		now.UnixMilli())
}

// EvictCacheEntries implements Store.
func (s *SQLite) EvictCacheEntries(ctx context.Context, size int64) (int64, error) {
	// Running totals of the sizes, in order of last use, find the fewest
	// least recently used entries that free size bytes.
	return s.deleteCacheEntries(ctx, `DELETE FROM cache_entries WHERE hash IN (
		SELECT hash FROM (SELECT hash, size_bytes, sum(size_bytes) OVER (ORDER BY used_at, hash) AS total
			FROM cache_entries)
		WHERE total - size_bytes < ?) RETURNING size_bytes`, size)
}

// deleteCacheEntries runs a DELETE that returns the size of each entry it
// deletes, and returns their total.
func (s *SQLite) deleteCacheEntries(ctx context.Context, query string, arg any) (int64, error) {
	rows, err := s.db.QueryContext(ctx, query, arg)
	if err != nil {
		return 0, fmt.Errorf("delete cache entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var freed int64
	for rows.Next() {
		var size int64
		if err := rows.Scan(&size); err != nil {
			return 0, fmt.Errorf("delete cache entries: %w", err)
		}
		freed += size
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("delete cache entries: %w", err)
	}
	return freed, nil
}

// CacheSize implements Store.
func (s *SQLite) CacheSize(ctx context.Context) (int64, error) {
	var size int64
	if err := s.db.QueryRowContext(ctx, `SELECT coalesce(sum(size_bytes), 0) FROM cache_entries`).
		Scan(&size); err != nil {
		return 0, fmt.Errorf("read cache size: %w", err)
	}
	return size, nil
}
