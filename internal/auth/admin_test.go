package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

func storeKey(name string) store.Key { return store.Key{Name: name} }

func TestAdminTokens(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()
	token, stored, err := CreateAdmin(ctx, st, "ops", time.Now())
	if err != nil || len(token) != AdminTokenLen || !strings.HasPrefix(token, AdminPrefix) ||
		stored.Prefix != token[:AdminPrefixLen] || stored.Name != "ops" {
		t.Fatalf("CreateAdmin() = %q, %+v, %v", token, stored, err)
	}
	if got, err := VerifyAdmin(ctx, st, token); err != nil || got.ID != stored.ID {
		t.Errorf("VerifyAdmin() = %+v, %v", got, err)
	}
	key, _, err := Create(ctx, st, "p", storeKey("k"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"a virtual key":   key,
		"wrong checksum":  token[:len(token)-1] + "x",
		"unknown prefix":  AdminPrefix + strings.Repeat("0", AdminTokenLen-len(AdminPrefix)),
		"empty":           "",
		"prefix of admin": AdminPrefixOf(token),
	} {
		if _, err := VerifyAdmin(ctx, st, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("VerifyAdmin(%s) error = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := st.RevokeAdminToken(ctx, stored.Prefix, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyAdmin(ctx, st, token); !errors.Is(err, ErrRevoked) {
		t.Errorf("VerifyAdmin() of a revoked token: error = %v, want ErrRevoked", err)
	}
	if AdminPrefixOf(token) != stored.Prefix || AdminPrefixOf("abc") != "abc" {
		t.Error("AdminPrefixOf() doesn't return the stored prefix")
	}
}
