package store

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestCacheEntries(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	t0 := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	put := func(hash string, size int, at time.Time, cost *float64) {
		t.Helper()
		if _, err := s.PutCacheEntry(ctx, CacheEntry{Hash: []byte(hash), Ciphertext: bytes.Repeat([]byte{'x'}, size),
			ContentType: "application/json", CostUSD: cost, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	put("a", 10, t0, usd(0.5))
	put("b", 20, t0.Add(time.Minute), nil)
	put("c", 30, t0.Add(2*time.Minute), nil)

	e, err := s.CacheEntry(ctx, []byte("a"), t0)
	if err != nil || e.Size() != 10 || e.ContentType != "application/json" || e.CostUSD == nil || *e.CostUSD != 0.5 ||
		!e.ExpiresAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("CacheEntry(a) = %+v, %v", e, err)
	}
	if e, err := s.CacheEntry(ctx, []byte("b"), t0); err != nil || e.CostUSD != nil {
		t.Errorf("CacheEntry(b) = %+v, %v; want an unpriced entry", e, err)
	}
	if _, err := s.CacheEntry(ctx, []byte("a"), t0.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("CacheEntry() of an expired entry: error = %v, want ErrNotFound", err)
	}
	if _, err := s.CacheEntry(ctx, []byte("z"), t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("CacheEntry() of an unknown hash: error = %v, want ErrNotFound", err)
	}

	// A hit makes a the most recently used entry: the least recently used
	// are now b and then c.
	if err := s.TouchCacheEntry(ctx, []byte("a"), t0.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var hits int
	if err := s.db.QueryRowContext(ctx, `SELECT hits FROM cache_entries WHERE hash = ?`, []byte("a")).
		Scan(&hits); err != nil || hits != 1 {
		t.Errorf("hits = %d, %v; want 1", hits, err)
	}
	if size, err := s.CacheSize(ctx); err != nil || size != 60 {
		t.Errorf("CacheSize() = %d, %v; want 60", size, err)
	}
	// 20 bytes of b aren't enough to free 25, so c goes too.
	if freed, err := s.EvictCacheEntries(ctx, 25); err != nil || freed != 50 {
		t.Errorf("EvictCacheEntries(25) = %d, %v; want 50 from b and c", freed, err)
	}
	if _, err := s.CacheEntry(ctx, []byte("a"), t0); err != nil {
		t.Errorf("the most recently used entry was evicted: %v", err)
	}
	if freed, err := s.EvictCacheEntries(ctx, 0); err != nil || freed != 0 {
		t.Errorf("EvictCacheEntries(0) = %d, %v; want nothing", freed, err)
	}

	// Replacing an entry reports the size it replaced.
	if replaced, err := s.PutCacheEntry(ctx, CacheEntry{Hash: []byte("a"), Ciphertext: []byte("new"),
		CreatedAt: t0, ExpiresAt: t0.Add(time.Minute)}); err != nil || replaced != 10 {
		t.Errorf("PutCacheEntry() over a = %d, %v; want 10 replaced", replaced, err)
	}
	put("d", 5, t0, nil) // expires at t0+1h
	if freed, err := s.DeleteExpiredCacheEntries(ctx, t0.Add(time.Minute)); err != nil || freed != 3 {
		t.Errorf("DeleteExpiredCacheEntries() = %d, %v; want 3 from a", freed, err)
	}
	if size, err := s.CacheSize(ctx); err != nil || size != 5 {
		t.Errorf("CacheSize() = %d, %v; want 5 left in d", size, err)
	}
}
