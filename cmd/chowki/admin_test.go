package main

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
)

var printedAdminRE = regexp.MustCompile(`(?m)^  (chowki_admin_[0-9A-Za-z]{49})$`)

func TestAdminTokens(t *testing.T) {
	initDir(t)
	if out := runOK(t, "admin", "list"); !strings.Contains(out, "No admin tokens yet") {
		t.Errorf("empty list = %q", out)
	}
	out := runOK(t, "admin", "create", "--name", "ops")
	m := printedAdminRE.FindStringSubmatch(out)
	if m == nil || !strings.Contains(out, `Created admin token "ops"`) {
		t.Fatalf("create output = %q", out)
	}
	st, err := store.OpenSQLite(t.Context(), "file:data/chowki.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if _, err := auth.VerifyAdmin(t.Context(), st, m[1]); err != nil {
		t.Fatalf("the printed token doesn't verify: %v", err)
	}
	prefix := m[1][:auth.AdminPrefixLen]
	if list := runOK(t, "admin", "list"); !strings.Contains(list, prefix) || !strings.Contains(list, "active") ||
		strings.Contains(list, m[1]) {
		t.Errorf("list = %q", list)
	}
	if out := runOK(t, "admin", "revoke", m[1]); !strings.Contains(out, "Revoked admin token "+prefix) {
		t.Errorf("revoke output = %q", out)
	}
	if _, err := auth.VerifyAdmin(t.Context(), st, m[1]); !errors.Is(err, auth.ErrRevoked) {
		t.Errorf("VerifyAdmin() after revoke = %v", err)
	}
	if out := runOK(t, "admin", "revoke", prefix); !strings.Contains(out, "was already revoked") {
		t.Errorf("second revoke output = %q", out)
	}
	if n := countAudit(t, "admin.create", prefix) + countAudit(t, "admin.revoke", prefix); n != 2 {
		t.Errorf("audit log has %d admin entries, want 2", n)
	}

	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"admin", "create"}, "--name is required"},
		{[]string{"admin", "revoke"}, "Usage:"},
		{[]string{"admin", "revoke", "chowki_admin_nope1"}, `no admin token has the prefix "chowki_admin_nope1"`},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(tt.args, &stdout, &stderr); code == exitOK || !strings.Contains(stderr.String(), tt.want) {
			t.Errorf("chowki %q = %d, stderr %q; want an error with %q", tt.args, code, stderr.String(), tt.want)
		}
	}
}
