package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

var keyRE = regexp.MustCompile(`^chowki_[0-9A-Za-z]{49}$`)

func TestNewKey(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		key, prefix, hash, err := NewKey()
		if err != nil {
			t.Fatal(err)
		}
		if !keyRE.MatchString(key) || len(key) != KeyLen {
			t.Fatalf("key %q has the wrong format", key)
		}
		if prefix != key[:PrefixLen] || hash != sha256.Sum256([]byte(key)) {
			t.Fatalf("prefix %q or hash don't match key %q", prefix, key)
		}
		if err := Check(key); err != nil {
			t.Fatalf("Check(%q) = %v", key, err)
		}
		if seen[key] {
			t.Fatalf("key %q generated twice", key)
		}
		seen[key] = true
	}
}

func TestCheck(t *testing.T) {
	key, _, _, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	flip := func(i int) string {
		b := []byte(key)
		if b[i] == 'a' {
			b[i] = 'b'
		} else {
			b[i] = 'a'
		}
		return string(b)
	}
	for name, bad := range map[string]string{
		"empty":               "",
		"other prefix":        "sk_" + key[len(KeyPrefix):],
		"too short":           key[:KeyLen-1],
		"too long":            key + "a",
		"not base62":          key[:20] + "-" + key[21:],
		"changed random":      flip(20),
		"changed checksum":    flip(KeyLen - 1),
		"prefix only":         key[:PrefixLen],
		"uppercase prefix":    "CHOWKI_" + key[len(KeyPrefix):],
		"non-ASCII character": key[:20] + "é" + key[22:],
	} {
		if err := Check(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check(%s) = %v, want ErrInvalid", name, err)
		}
	}
}

func TestPrefixOf(t *testing.T) {
	key, prefix, _, _ := NewKey()
	for in, want := range map[string]string{key: prefix, prefix: prefix, "anything": "anything"} {
		if got := PrefixOf(in); got != want {
			t.Errorf("PrefixOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func openStore(t *testing.T) *store.SQLite {
	t.Helper()
	st, err := store.OpenSQLite(t.Context(), "file:"+filepath.Join(t.TempDir(), "chowki.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestCreateAndVerify(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	key, k, err := Create(ctx, st, "team", "alice", now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if k.Name != "alice" || k.Project != "team" || k.Prefix != key[:PrefixLen] || !k.CreatedAt.Equal(now) {
		t.Errorf("Create() stored %+v", k)
	}

	got, err := Verify(ctx, st, key)
	if err != nil || got.ID != k.ID {
		t.Fatalf("Verify() = %+v, %v; want key %d", got, err, k.ID)
	}

	// A well-formed key with a stored prefix but a different secret.
	other, _, _, _ := NewKey()
	random := key[len(KeyPrefix):PrefixLen] + other[PrefixLen:KeyLen-checksumLen]
	forged := KeyPrefix + random + checksum(random)
	if err := Check(forged); err != nil {
		t.Fatalf("forged key is malformed: %v", err)
	}
	if _, err := Verify(ctx, st, forged); !errors.Is(err, ErrInvalid) {
		t.Errorf("Verify() of a forged key = %v, want ErrInvalid", err)
	}
	if _, err := Verify(ctx, st, other); !errors.Is(err, ErrInvalid) {
		t.Errorf("Verify() of an unknown key = %v, want ErrInvalid", err)
	}

	if _, err := st.RevokeKey(ctx, k.Prefix, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(ctx, st, key); !errors.Is(err, ErrRevoked) {
		t.Errorf("Verify() of a revoked key = %v, want ErrRevoked", err)
	}
}

// fakeStore returns canned results and counts lookups.
type fakeStore struct {
	store.Store
	lookups   int
	existsFor int // CreateKey fails with ErrExists this many times
	lookupErr error
}

func (f *fakeStore) EnsureProject(context.Context, string) (store.Project, error) {
	return store.Project{ID: 1}, nil
}

func (f *fakeStore) CreateKey(_ context.Context, k store.Key) (store.Key, error) {
	if f.existsFor > 0 {
		f.existsFor--
		return store.Key{}, store.ErrExists
	}
	return k, nil
}

func (f *fakeStore) KeyByPrefix(context.Context, string) (store.Key, error) {
	f.lookups++
	return store.Key{}, f.lookupErr
}

func TestVerifySkipsLookupForMalformedKeys(t *testing.T) {
	f := &fakeStore{}
	if _, err := Verify(t.Context(), f, "chowki_not-a-real-key"); !errors.Is(err, ErrInvalid) {
		t.Errorf("Verify() = %v, want ErrInvalid", err)
	}
	if f.lookups != 0 {
		t.Errorf("Verify() looked up a malformed key %d times", f.lookups)
	}
}

func TestVerifyReportsStoreErrors(t *testing.T) {
	key, _, _, _ := NewKey()
	f := &fakeStore{lookupErr: errors.New("disk on fire")}
	_, err := Verify(t.Context(), f, key)
	if err == nil || errors.Is(err, ErrInvalid) {
		t.Errorf("Verify() = %v, want the store error, not ErrInvalid", err)
	}
}

func TestCreateRetriesUsedPrefixes(t *testing.T) {
	if _, _, err := Create(t.Context(), &fakeStore{existsFor: 2}, "p", "n", time.Now()); err != nil {
		t.Errorf("Create() after two used prefixes: %v", err)
	}
	if _, _, err := Create(t.Context(), &fakeStore{existsFor: 5}, "p", "n", time.Now()); err == nil {
		t.Error("Create() succeeded although every prefix was used")
	}
}

func FuzzCheck(f *testing.F) {
	key, _, _, _ := NewKey()
	for _, seed := range []string{key, "", "chowki_", key[:30], key + "x"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if Check(s) == nil && !keyRE.MatchString(s) {
			t.Errorf("Check(%q) accepted a malformed key", s)
		}
	})
}
