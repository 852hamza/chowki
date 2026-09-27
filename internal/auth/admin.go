package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

// AdminPrefix starts every admin token, which authorizes the admin API and
// the dashboard.
const AdminPrefix = "chowki_admin_"

const (
	// AdminTokenLen is the length of an admin token.
	AdminTokenLen = len(AdminPrefix) + randomLen + checksumLen
	// AdminPrefixLen is the length of the prefix that identifies a stored
	// admin token.
	AdminPrefixLen = len(AdminPrefix) + 5
)

// CreateAdmin generates an admin token named name and saves its prefix and
// hash. It returns the token, which is shown once: only its hash is stored.
func CreateAdmin(ctx context.Context, st store.Store, name string, now time.Time) (string, store.AdminToken, error) {
	for range 5 {
		token, prefix, hash, err := generate(AdminPrefix, AdminPrefixLen)
		if err != nil {
			return "", store.AdminToken{}, err
		}
		t, err := st.CreateAdminToken(ctx, store.AdminToken{Name: name, Prefix: prefix, Hash: hash, CreatedAt: now})
		if errors.Is(err, store.ErrExists) {
			continue
		}
		if err != nil {
			return "", store.AdminToken{}, err
		}
		return token, t, nil
	}
	return "", store.AdminToken{}, errors.New("create admin token: no unused prefix after 5 tries")
}

// VerifyAdmin returns the stored admin token that matches token. It returns
// ErrInvalid for a malformed, unknown or wrong token, and ErrRevoked for a
// revoked one; any other error means the lookup itself failed.
func VerifyAdmin(ctx context.Context, st store.Store, token string) (store.AdminToken, error) {
	if err := checkFormat(token, AdminPrefix); err != nil {
		return store.AdminToken{}, err
	}
	t, err := st.AdminTokenByPrefix(ctx, token[:AdminPrefixLen])
	if errors.Is(err, store.ErrNotFound) {
		return store.AdminToken{}, ErrInvalid
	}
	if err != nil {
		return store.AdminToken{}, fmt.Errorf("verify admin token: %w", err)
	}
	hash := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(hash[:], t.Hash[:]) != 1 {
		return store.AdminToken{}, ErrInvalid
	}
	if t.Revoked() {
		return store.AdminToken{}, ErrRevoked
	}
	return t, nil
}

// AdminPrefixOf returns the prefix of a full admin token, or s itself when
// it's already a prefix, so that commands accept either.
func AdminPrefixOf(s string) string {
	if strings.HasPrefix(s, AdminPrefix) && len(s) > AdminPrefixLen {
		return s[:AdminPrefixLen]
	}
	return s
}
