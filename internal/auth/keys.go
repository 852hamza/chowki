package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"hash/crc32"
	"math/big"
	"strings"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

// KeyPrefix starts every virtual key, so that people and secret scanners
// recognize one.
const KeyPrefix = "chowki_"

const (
	randomLen   = 43 // 32 random bytes in base62
	checksumLen = 6  // CRC32 of the random part in base62
	// KeyLen is the length of a virtual key.
	KeyLen = len(KeyPrefix) + randomLen + checksumLen
	// PrefixLen is the length of the prefix that identifies a stored key.
	PrefixLen = 12
)

var (
	// ErrInvalid means the key is malformed, unknown or wrong. Callers
	// shouldn't tell these cases apart in responses.
	ErrInvalid = errors.New("invalid virtual key")
	// ErrRevoked means the key was valid but has been revoked.
	ErrRevoked = errors.New("revoked virtual key")
)

// NewKey generates a new virtual key and returns it with the prefix and
// hash to store. Only the caller ever sees the key itself.
func NewKey() (key, prefix string, hash [32]byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", hash, fmt.Errorf("generate key: %w", err)
	}
	random := base62(new(big.Int).SetBytes(b), randomLen)
	key = KeyPrefix + random + checksum(random)
	return key, key[:PrefixLen], sha256.Sum256([]byte(key)), nil
}

// Check reports whether key has the format and checksum of a virtual key,
// without a database lookup. It returns ErrInvalid otherwise.
func Check(key string) error {
	random, ok := strings.CutPrefix(key, KeyPrefix)
	if !ok || len(key) != KeyLen {
		return ErrInvalid
	}
	for i := range len(random) {
		if strings.IndexByte(base62Digits, random[i]) < 0 {
			return ErrInvalid
		}
	}
	if sum := random[randomLen:]; sum != checksum(random[:randomLen]) {
		return ErrInvalid
	}
	return nil
}

// Verify returns the stored key that matches key. It returns ErrInvalid
// for a malformed, unknown or wrong key, and ErrRevoked for a revoked one;
// any other error means the lookup itself failed.
func Verify(ctx context.Context, st store.Store, key string) (store.Key, error) {
	if err := Check(key); err != nil {
		return store.Key{}, err
	}
	k, err := st.KeyByPrefix(ctx, key[:PrefixLen])
	if errors.Is(err, store.ErrNotFound) {
		return store.Key{}, ErrInvalid
	}
	if err != nil {
		return store.Key{}, fmt.Errorf("verify key: %w", err)
	}
	hash := sha256.Sum256([]byte(key))
	if subtle.ConstantTimeCompare(hash[:], k.Hash[:]) != 1 {
		return store.Key{}, ErrInvalid
	}
	if k.Revoked() {
		return store.Key{}, ErrRevoked
	}
	return k, nil
}

// Create generates a key in the project, creating the project if needed,
// and saves its prefix and hash with the name and settings of k, such as
// its budget. It returns the key, which is shown once: only its hash is
// stored.
func Create(ctx context.Context, st store.Store, project string, k store.Key, now time.Time) (string, store.Key, error) {
	p, err := st.EnsureProject(ctx, project)
	if err != nil {
		return "", store.Key{}, err
	}
	// A prefix has 5 random characters, so two keys can share one; a
	// new random key almost certainly won't.
	for range 5 {
		key, prefix, hash, err := NewKey()
		if err != nil {
			return "", store.Key{}, err
		}
		k.ProjectID, k.Prefix, k.Hash, k.CreatedAt = p.ID, prefix, hash, now
		stored, err := st.CreateKey(ctx, k)
		if errors.Is(err, store.ErrExists) {
			continue
		}
		if err != nil {
			return "", store.Key{}, err
		}
		return key, stored, nil
	}
	return "", store.Key{}, errors.New("create key: no unused prefix after 5 tries")
}

// PrefixOf returns the prefix of a full key, or s itself when it's already
// a prefix, so commands accept either.
func PrefixOf(s string) string {
	if strings.HasPrefix(s, KeyPrefix) && len(s) > PrefixLen {
		return s[:PrefixLen]
	}
	return s
}

const base62Digits = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// base62 encodes n in exactly width digits, padding with zeros.
func base62(n *big.Int, width int) string {
	s := n.Text(62) // big.Int uses the same digits as base62Digits
	return strings.Repeat("0", width-len(s)) + s
}

func checksum(random string) string {
	return base62(big.NewInt(int64(crc32.ChecksumIEEE([]byte(random)))), checksumLen)
}
