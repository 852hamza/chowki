package store

import (
	"context"
	"fmt"
	"time"
)

// ProviderKey is a provider key stored in the database, sealed.
type ProviderKey struct {
	// Provider is the provider's name in the configuration.
	Provider string
	// Sealed is the key as secretbox seals it; only the master key opens it.
	Sealed    []byte
	UpdatedAt time.Time
}

// SetProviderKey implements Store.
func (s *SQLite) SetProviderKey(ctx context.Context, k ProviderKey) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO provider_keys (provider, sealed, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (provider) DO UPDATE SET sealed = excluded.sealed, updated_at = excluded.updated_at`,
		k.Provider, k.Sealed, k.UpdatedAt.UnixMilli()); err != nil {
		return fmt.Errorf("store provider key: %w", err)
	}
	return nil
}

// ProviderKeys implements Store.
func (s *SQLite) ProviderKeys(ctx context.Context) ([]ProviderKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider, sealed, updated_at FROM provider_keys ORDER BY provider`)
	if err != nil {
		return nil, fmt.Errorf("read provider keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []ProviderKey
	for rows.Next() {
		var k ProviderKey
		var updated int64
		if err := rows.Scan(&k.Provider, &k.Sealed, &updated); err != nil {
			return nil, fmt.Errorf("read provider keys: %w", err)
		}
		k.UpdatedAt = time.UnixMilli(updated)
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read provider keys: %w", err)
	}
	return keys, nil
}

// DeleteProviderKey implements Store.
func (s *SQLite) DeleteProviderKey(ctx context.Context, provider string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM provider_keys WHERE provider = ?`, provider)
	if err != nil {
		return fmt.Errorf("delete provider key: %w", err)
	}
	n, err := res.RowsAffected()
	switch {
	case err != nil:
		return fmt.Errorf("delete provider key: %w", err)
	case n == 0:
		return ErrNotFound
	}
	return nil
}
