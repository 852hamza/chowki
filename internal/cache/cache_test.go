package cache

import (
	"bytes"
	"crypto/sha256"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/store"
)

var t0 = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

type harness struct {
	t    *testing.T
	st   *store.SQLite
	opts Options
	logs *strings.Builder
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	st, err := store.OpenSQLite(t.Context(), "file:"+filepath.Join(t.TempDir(), "chowki.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &harness{t: t, st: st, opts: opts, logs: &strings.Builder{}}
}

// open returns a cache on the harness's database, sealed with the master
// key made of masterByte; Close it to finish its writes.
func (h *harness) open(masterByte byte) *Cache {
	h.t.Helper()
	box, err := secretbox.New(bytes.Repeat([]byte{masterByte}, secretbox.KeySize), "cache")
	if err != nil {
		h.t.Fatal(err)
	}
	c, err := New(h.t.Context(), h.st, box, h.opts, slog.New(slog.NewTextHandler(h.logs, nil)))
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

func key(s string) [32]byte { return sha256.Sum256([]byte(s)) }

func TestOn(t *testing.T) {
	tests := []struct {
		maxBytes         int64
		def, key, header string
		want             bool
	}{
		{1, ModeOff, "", "", false},
		{1, ModeExact, "", "", true},
		{1, ModeOff, ModeExact, "", true},
		{1, ModeExact, ModeOff, "", false},
		{1, ModeOff, ModeOff, "on", true},
		{1, ModeExact, ModeExact, "off", false},
		{0, ModeExact, ModeExact, "on", false}, // a cache of 0 bytes is off
	}
	for _, tt := range tests {
		c := &Cache{opts: Options{Default: tt.def, MaxBytes: tt.maxBytes}}
		if got := c.On(tt.key, tt.header); got != tt.want {
			t.Errorf("On(%q, %q) with default %s and %d bytes = %v, want %v", tt.key, tt.header, tt.def,
				tt.maxBytes, got, tt.want)
		}
	}
}

func TestPutAndGet(t *testing.T) {
	h := newHarness(t, Options{TTL: time.Hour, MaxBytes: 1 << 20})
	c := h.open(1)
	if _, ok := c.Get(t.Context(), key("q"), t0); ok {
		t.Fatal("Get() of an empty cache hit")
	}
	cost := 0.25
	body := []byte(`{"answer":"the secret reply"}`)
	c.Put(key("q"), Response{Body: body, ContentType: "application/json", CostUSD: &cost}, t0)
	c.Close()

	c = h.open(1)
	defer c.Close()
	r, ok := c.Get(t.Context(), key("q"), t0.Add(59*time.Minute))
	if !ok || !bytes.Equal(r.Body, body) || r.ContentType != "application/json" || r.CostUSD == nil || *r.CostUSD != 0.25 {
		t.Fatalf("Get() = %+v, %v; want the stored response", r, ok)
	}
	// AC: cache entries are encrypted at rest.
	k := key("q")
	e, err := h.st.CacheEntry(t.Context(), k[:], t0)
	if err != nil || bytes.Contains(e.Ciphertext, []byte("secret reply")) {
		t.Errorf("the stored entry holds the response in plain text: %q, %v", e.Ciphertext, err)
	}
	// AC: TTL.
	if _, ok := c.Get(t.Context(), key("q"), t0.Add(time.Hour)); ok {
		t.Error("Get() served an expired response")
	}
	if _, ok := c.Get(t.Context(), key("other"), t0); ok {
		t.Error("Get() of another request hit")
	}
}

func TestAnotherMasterKeyMisses(t *testing.T) {
	h := newHarness(t, Options{TTL: time.Hour, MaxBytes: 1 << 20})
	c := h.open(1)
	c.Put(key("q"), Response{Body: []byte(`{}`)}, t0)
	c.Close()
	c = h.open(2)
	defer c.Close()
	if _, ok := c.Get(t.Context(), key("q"), t0); ok {
		t.Error("Get() opened an entry sealed with another master key")
	}
	if !strings.Contains(h.logs.String(), "master key may have changed") {
		t.Errorf("no warning was logged:\n%s", h.logs.String())
	}
}

// AC: size eviction removes the least recently used entries.
func TestEviction(t *testing.T) {
	// Each entry takes 1029 bytes: 1000 of body, the key version, the nonce
	// and the tag. Four fit in 4500 bytes, a fifth doesn't.
	h := newHarness(t, Options{TTL: time.Hour, MaxBytes: 4500})
	c := h.open(1)
	body := bytes.Repeat([]byte("x"), 1000)
	for i, name := range []string{"a", "b", "c", "d"} {
		c.Put(key(name), Response{Body: body}, t0.Add(time.Duration(i)*time.Second))
	}
	c.Close()
	c = h.open(1)
	// A hit makes a the most recently used, so b and c go first.
	if _, ok := c.Get(t.Context(), key("a"), t0.Add(time.Minute)); !ok {
		t.Fatal("a is missing before any eviction")
	}
	c.Put(key("e"), Response{Body: body}, t0.Add(2*time.Minute))
	c.Put(key("huge"), Response{Body: make([]byte, 4500)}, t0) // larger than the cache
	c.Close()

	c = h.open(1)
	defer c.Close()
	for name, want := range map[string]bool{"a": true, "b": false, "c": false, "d": true, "e": true, "huge": false} {
		if _, ok := c.Get(t.Context(), key(name), t0.Add(3*time.Minute)); ok != want {
			t.Errorf("entry %s cached = %v, want %v", name, ok, want)
		}
	}
	if size, err := h.st.CacheSize(t.Context()); err != nil || size > 4500*9/10 {
		t.Errorf("CacheSize() = %d, %v; want at most 90%% of the limit", size, err)
	}
}

func TestExpire(t *testing.T) {
	h := newHarness(t, Options{TTL: time.Minute, MaxBytes: 1 << 20})
	c := h.open(1)
	c.Put(key("q"), Response{Body: []byte(`{}`)}, t0)
	c.Close()
	c = h.open(1)
	c.expire(t0.Add(time.Minute))
	c.Close()
	if size, err := h.st.CacheSize(t.Context()); err != nil || size != 0 {
		t.Errorf("CacheSize() after expiry = %d, %v; want 0", size, err)
	}
}

func TestWritesAfterCloseAreDropped(t *testing.T) {
	h := newHarness(t, Options{TTL: time.Hour, MaxBytes: 1 << 20})
	c := h.open(1)
	c.Close()
	c.Close() // twice is fine
	c.Put(key("q"), Response{Body: []byte(`{}`)}, t0)
	if size, err := h.st.CacheSize(t.Context()); err != nil || size != 0 {
		t.Errorf("CacheSize() = %d, %v; want nothing stored after Close", size, err)
	}
}
