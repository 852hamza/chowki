package main

import (
	"bytes"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
)

// initDir runs chowki init in a new temporary working directory.
func initDir(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	if code := run([]string{"init"}, &out, &out); code != exitOK {
		t.Fatalf("chowki init = %d\n%s", code, out.String())
	}
}

func runOK(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != exitOK {
		t.Fatalf("chowki %s = %d\nstderr: %s", strings.Join(args, " "), code, stderr.String())
	}
	return stdout.String()
}

var printedKeyRE = regexp.MustCompile(`(?m)^  (chowki_[0-9A-Za-z]{49})$`)

func TestKeyLifecycle(t *testing.T) {
	initDir(t)
	if out := runOK(t, "key", "list"); !strings.Contains(out, "No virtual keys yet") {
		t.Errorf("empty list = %q", out)
	}

	out := runOK(t, "key", "create", "--name", "alice", "--project", "team")
	m := printedKeyRE.FindStringSubmatch(out)
	if m == nil || !strings.Contains(out, `"alice" in project "team"`) || !strings.Contains(out, "can't show it again") {
		t.Fatalf("create output = %q", out)
	}
	key := m[1]
	prefix := key[:auth.PrefixLen]

	st, err := store.OpenSQLite(t.Context(), "file:data/chowki.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if k, err := auth.Verify(t.Context(), st, key); err != nil || k.Name != "alice" {
		t.Fatalf("the printed key doesn't verify: %+v, %v", k, err)
	}

	list := runOK(t, "key", "list")
	for _, want := range []string{"PREFIX", prefix, "alice", "team", "active"} {
		if !strings.Contains(list, want) {
			t.Errorf("list doesn't contain %q:\n%s", want, list)
		}
	}
	if strings.Contains(list, key) {
		t.Error("list shows the full key")
	}

	// Revoking by the full key works too.
	if out := runOK(t, "key", "revoke", key); !strings.Contains(out, "Revoked virtual key "+prefix) {
		t.Errorf("revoke output = %q", out)
	}
	if _, err := auth.Verify(t.Context(), st, key); !errors.Is(err, auth.ErrRevoked) {
		t.Errorf("Verify() after revoke = %v, want ErrRevoked", err)
	}
	if out := runOK(t, "key", "revoke", prefix); !strings.Contains(out, "was already revoked") {
		t.Errorf("second revoke output = %q", out)
	}
	if list := runOK(t, "key", "list"); !strings.Contains(list, "revoked 20") {
		t.Errorf("list after revoke:\n%s", list)
	}
}

func TestKeyErrors(t *testing.T) {
	initDir(t)
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"key"}, exitUsage, "Usage:"},
		{[]string{"key", "rotate"}, exitUsage, `unknown command "rotate"`},
		{[]string{"key", "create"}, exitUsage, "--name is required"},
		{[]string{"key", "create", "--name", "a\tb"}, exitUsage, "printable"},
		{[]string{"key", "create", "--name", strings.Repeat("x", 65)}, exitUsage, "at most 64"},
		{[]string{"key", "create", "--name", "a", "extra"}, exitUsage, ""},
		{[]string{"key", "list", "extra"}, exitUsage, ""},
		{[]string{"key", "revoke"}, exitUsage, "Usage:"},
		{[]string{"key", "revoke", "chowki_nope1"}, exitError, `no virtual key has the prefix "chowki_nope1"`},
		{[]string{"key", "create", "--name", "a", "--budget-usd", "-1"}, exitUsage, "must be an amount of 0 or more"},
		{[]string{"key", "create", "--name", "a", "--budget-usd", "NaN"}, exitUsage, "must be an amount of 0 or more"},
		{[]string{"key", "create", "--name", "a", "--budget-usd", "ten"}, exitUsage, "invalid value"},
		{[]string{"key", "update", "chowki_nope1"}, exitUsage, "nothing to change"},
		{[]string{"key", "create", "--name", "a", "--rpm", "-1"}, exitUsage, "--rpm must be 0 or more"},
		{[]string{"key", "create", "--name", "a", "--tpm", "1.5"}, exitUsage, "invalid value"},
		{[]string{"key", "update", "--tpm", "-5", "chowki_nope1"}, exitUsage, "--tpm must be 0 or more"},
		{[]string{"key", "create", "--name", "a", "--cache", "always"}, exitUsage, "--cache must be exact, off or default"},
		{[]string{"key", "create", "--name", "a", "--redaction", "hide"}, exitUsage, "--redaction must be mask, block"},
		{[]string{"key", "update", "--budget-usd", "5"}, exitUsage, "Usage:"},
		{[]string{"key", "update", "--budget-usd", "Inf", "chowki_nope1"}, exitUsage, "must be an amount"},
		{[]string{"key", "update", "--budget-usd", "5", "chowki_nope1"}, exitError,
			`no virtual key has the prefix "chowki_nope1"`},
		{[]string{"key", "list", "--config", "missing.yaml"}, exitError, "read config"},
	}
	for _, tt := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(tt.args, &stdout, &stderr); code != tt.code {
			t.Errorf("chowki %q = %d, want %d\nstderr: %s", tt.args, code, tt.code, stderr.String())
		}
		if !strings.Contains(stderr.String(), tt.want) {
			t.Errorf("chowki %q stderr doesn't contain %q:\n%s", tt.args, tt.want, stderr.String())
		}
	}
	if out := runOK(t, "key", "help"); !strings.Contains(out, "chowki key create") {
		t.Errorf("key help = %q", out)
	}
}

func TestKeyBudgets(t *testing.T) {
	initDir(t)
	out := runOK(t, "key", "create", "--name", "alice", "--budget-usd", "50")
	m := printedKeyRE.FindStringSubmatch(out)
	if m == nil || !strings.Contains(out, "\nSettings:\n  Monthly budget:       $50.00\n") {
		t.Fatalf("create output = %q", out)
	}
	prefix := m[1][:auth.PrefixLen]

	st, err := store.OpenSQLite(t.Context(), "file:data/chowki.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	k, err := st.KeyByPrefix(t.Context(), prefix)
	if err != nil {
		t.Fatal(err)
	}
	cost := 12.345
	if err := st.InsertRequests(t.Context(), []store.Request{{ID: "r1", Time: time.Now(), KeyID: k.ID,
		ProjectID: k.ProjectID, CostUSD: &cost}}); err != nil {
		t.Fatal(err)
	}
	list := runOK(t, "key", "list")
	for _, want := range []string{"SPENT " + store.Period(time.Now()), "BUDGET", "$12.35", "$50.00"} {
		if !strings.Contains(list, want) {
			t.Errorf("list doesn't contain %q:\n%s", want, list)
		}
	}

	if out := runOK(t, "key", "update", "--budget-usd", "75.5", m[1]); !strings.Contains(out,
		"Updated the settings of virtual key "+prefix+` ("alice"):`+"\n  Monthly budget:       $75.50\n") {
		t.Errorf("update output = %q", out)
	}
	if out := runOK(t, "key", "update", "--budget-usd", "0", prefix); !strings.Contains(out,
		"Monthly budget:       none") {
		t.Errorf("update to 0 output = %q", out)
	}
	if list := runOK(t, "key", "list"); !strings.Contains(list, "none") {
		t.Errorf("list after removing the budget:\n%s", list)
	}
	if n := countAudit(t, "key.update", prefix); n != 2 {
		t.Errorf("audit log has %d key.update entries, want 2", n)
	}
}

func TestKeyRateLimits(t *testing.T) {
	initDir(t)
	out := runOK(t, "key", "create", "--name", "ci-bot", "--rpm", "60", "--tpm", "100000")
	m := printedKeyRE.FindStringSubmatch(out)
	if m == nil || !strings.Contains(out, "  Monthly budget:       none\n  Requests per minute:  60\n"+
		"  Tokens per minute:    100000\n") {
		t.Fatalf("create output = %q", out)
	}
	prefix := m[1][:auth.PrefixLen]
	list := runOK(t, "key", "list")
	if fields := strings.Fields(strings.Split(list, "\n")[1]); len(fields) < 7 || fields[5] != "60" ||
		fields[6] != "100000" || !strings.Contains(list, "RPM") {
		t.Errorf("list doesn't show 60 RPM and 100000 TPM:\n%s", list)
	}
	if out := runOK(t, "key", "update", "--rpm", "0", "--tpm", "5000", prefix); !strings.Contains(out,
		"  Requests per minute:  none\n  Tokens per minute:    5000\n") {
		t.Errorf("update output = %q", out)
	}
}

func TestKeyCacheMode(t *testing.T) {
	initDir(t)
	out := runOK(t, "key", "create", "--name", "bot", "--cache", "exact")
	m := printedKeyRE.FindStringSubmatch(out)
	if m == nil || !strings.Contains(out, "  Exact cache:          exact\n") {
		t.Fatalf("create output = %q", out)
	}
	prefix := m[1][:auth.PrefixLen]
	if fields := strings.Fields(strings.Split(runOK(t, "key", "list"), "\n")[1]); len(fields) < 8 || fields[7] != "exact" {
		t.Errorf("list row = %q; want the cache mode exact", fields)
	}
	if out := runOK(t, "key", "update", "--cache", "default", prefix); !strings.Contains(out,
		"  Exact cache:          default\n") {
		t.Errorf("update output = %q", out)
	}
	if out := runOK(t, "key", "update", "--redaction", "block", prefix); !strings.Contains(out,
		"  Redaction:            block\n") {
		t.Errorf("update output = %q", out)
	}
	if fields := strings.Fields(strings.Split(runOK(t, "key", "list"), "\n")[1]); len(fields) < 9 || fields[8] != "block" {
		t.Errorf("list row = %q; want the redaction mode block", fields)
	}
	// A key without settings doesn't print them.
	if out := runOK(t, "key", "create", "--name", "plain"); strings.Contains(out, "Settings:") {
		t.Errorf("create output of a key without settings = %q", out)
	}
}

// countAudit counts the audit log entries of an action on a target.
func countAudit(t *testing.T, action, target string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:data/chowki.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_log WHERE action = ? AND target = ?`,
		action, target).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
