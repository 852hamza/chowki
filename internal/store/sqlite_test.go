package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// openTest opens a new database in a temporary folder.
func openTest(t *testing.T) *SQLite {
	t.Helper()
	s, err := OpenSQLite(t.Context(), "file:"+filepath.Join(t.TempDir(), "data", "chowki.db"))
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenSQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data", "chowki.db")
	s, err := OpenSQLite(t.Context(), "file:"+path)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != want {
				t.Errorf("%s mode = %o, want %o", p, got, want)
			}
		}
	}
	for pragma, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "busy_timeout": "5000"} {
		var got string
		if err := s.db.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
			t.Errorf("PRAGMA %s = %q, %v; want %q", pragma, got, err, want)
		}
	}
	var versions int
	if err := s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil ||
		versions != 1 {
		t.Errorf("schema_migrations has %d rows, %v; want 1", versions, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Opening again applies nothing new.
	s, err = OpenSQLite(t.Context(), "file:"+path)
	if err != nil {
		t.Fatalf("second OpenSQLite() error = %v", err)
	}
	if _, err := s.db.ExecContext(t.Context(), "INSERT INTO schema_migrations VALUES (999, 0)"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := OpenSQLite(t.Context(), "file:"+path); err == nil || !strings.Contains(err.Error(), "newer Chowki") {
		t.Errorf("OpenSQLite() of a newer database: error = %v", err)
	}
}

func TestPrepareDSN(t *testing.T) {
	tests := []struct{ dsn, path, contains string }{
		{"file:data/chowki.db", "data/chowki.db", "_journal_mode=WAL"},
		{"file:///var/lib/chowki.db?_busy_timeout=100", "/var/lib/chowki.db", "_busy_timeout=100"},
		{"plain.db", "plain.db", "_foreign_keys=on"},
		{":memory:", "", "_synchronous=NORMAL"},
		{"file::memory:", "", "_busy_timeout=5000"},
		{"file:shared?mode=memory&cache=shared", "", "cache=shared"},
	}
	for _, tt := range tests {
		full, path, err := prepareDSN(tt.dsn)
		if err != nil || path != tt.path || !strings.Contains(full, tt.contains) {
			t.Errorf("prepareDSN(%q) = %q, %q, %v; want path %q and %q", tt.dsn, full, path, err, tt.path, tt.contains)
		}
	}
	if _, _, err := prepareDSN("file:x.db?%zz"); err == nil {
		t.Error("prepareDSN() accepted a bad query")
	}
}

func TestKeys(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p, err := s.EnsureProject(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.EnsureProject(ctx, "team"); err != nil || again.ID != p.ID {
		t.Errorf("EnsureProject() again = %+v, %v; want the same project", again, err)
	}

	created := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	k, err := s.CreateKey(ctx, Key{ProjectID: p.ID, Name: "alice", Prefix: "chowki_abcde", Hash: [32]byte{1, 2, 3},
		CreatedAt: created})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	want := Key{ID: k.ID, ProjectID: p.ID, Project: "team", Name: "alice", Prefix: "chowki_abcde",
		Hash: [32]byte{1, 2, 3}, CreatedAt: created}
	if k != want || k.Revoked() {
		t.Errorf("CreateKey() = %+v, want %+v", k, want)
	}
	if _, err := s.CreateKey(ctx, Key{ProjectID: p.ID, Name: "bob", Prefix: "chowki_abcde", CreatedAt: created}); !errors.Is(err, ErrExists) {
		t.Errorf("CreateKey() with a used prefix: error = %v, want ErrExists", err)
	}
	if _, err := s.CreateKey(ctx, Key{ProjectID: 999, Name: "x", Prefix: "chowki_other", CreatedAt: created}); err == nil {
		t.Error("CreateKey() accepted an unknown project")
	}
	if got, err := s.KeyByPrefix(ctx, "chowki_abcde"); err != nil || got != want {
		t.Errorf("KeyByPrefix() = %+v, %v", got, err)
	}
	if _, err := s.KeyByPrefix(ctx, "chowki_zzzzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("KeyByPrefix() of an unknown key: error = %v, want ErrNotFound", err)
	}

	second, err := s.CreateKey(ctx, Key{ProjectID: p.ID, Name: "bob", Prefix: "chowki_fghij", CreatedAt: created.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := s.ListKeys(ctx)
	if err != nil || len(keys) != 2 || keys[0].ID != k.ID || keys[1].ID != second.ID {
		t.Errorf("ListKeys() = %+v, %v", keys, err)
	}

	revokedAt := created.Add(2 * time.Hour)
	r, err := s.RevokeKey(ctx, "chowki_abcde", revokedAt)
	if err != nil || !r.Revoked() || !r.RevokedAt.Equal(revokedAt) {
		t.Errorf("RevokeKey() = %+v, %v", r, err)
	}
	// Revoking again keeps the first revocation time.
	if r, err := s.RevokeKey(ctx, "chowki_abcde", revokedAt.Add(time.Hour)); err != nil || !r.RevokedAt.Equal(revokedAt) {
		t.Errorf("RevokeKey() again = %+v, %v; want the first time", r, err)
	}
	if _, err := s.RevokeKey(ctx, "chowki_zzzzz", revokedAt); !errors.Is(err, ErrNotFound) {
		t.Errorf("RevokeKey() of an unknown key: error = %v", err)
	}
}

func TestRequests(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	cost := 0.0125
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rs := []Request{
		{ID: "r1", Time: base, KeyID: 1, ProjectID: 1, APIFamily: "openai", Endpoint: "/v1/chat/completions",
			Provider: "openai", Model: "m", Stream: true, Status: 200, Latency: 1500 * time.Millisecond,
			TTFB: 300 * time.Millisecond, Tokens: &Tokens{Input: 100, Output: 20, CacheRead: 60, CacheWrite: 10, Reasoning: 5},
			CostUSD: &cost, SavingsUSD: 0.001, SavingsMethod: "prompt_cache"},
		{ID: "r2", Time: base.Add(48 * time.Hour), KeyID: 1, ProjectID: 1, APIFamily: "anthropic",
			Endpoint: "/anthropic/v1/messages", Provider: "anthropic", Model: "m", Status: 502,
			ErrorType: "upstream_error", Latency: 20 * time.Millisecond},
	}
	if err := s.InsertRequests(ctx, rs); err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}

	type row struct {
		ID                                 string
		Stream                             bool
		TTFB, Input, CacheWrite, Reasoning sql.NullInt64
		Cost                               sql.NullFloat64
		Savings                            float64
		Method, ErrorType                  string
	}
	var got []row
	rows, err := s.db.QueryContext(ctx, `SELECT id, stream, ttfb_ms, input_tokens, cache_write_tokens,
		reasoning_tokens, cost_usd, savings_usd, savings_method, error_type FROM requests ORDER BY ts`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.Stream, &r.TTFB, &r.Input, &r.CacheWrite, &r.Reasoning, &r.Cost, &r.Savings,
			&r.Method, &r.ErrorType); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	_ = rows.Close()
	want := []row{
		{ID: "r1", Stream: true, TTFB: sql.NullInt64{Int64: 300, Valid: true}, Input: sql.NullInt64{Int64: 100, Valid: true},
			CacheWrite: sql.NullInt64{Int64: 10, Valid: true}, Reasoning: sql.NullInt64{Int64: 5, Valid: true},
			Cost: sql.NullFloat64{Float64: 0.0125, Valid: true}, Savings: 0.001, Method: "prompt_cache"},
		{ID: "r2", ErrorType: "upstream_error"}, // unknown usage, cost and TTFB stay NULL
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored requests =\n%+v\nwant\n%+v", got, want)
	}

	if err := s.InsertRequests(ctx, rs[:1]); err == nil {
		t.Error("InsertRequests() accepted a duplicate ID")
	}
	n, err := s.DeleteRequestsBefore(ctx, base.Add(time.Hour))
	if err != nil || n != 1 {
		t.Errorf("DeleteRequestsBefore() = %d, %v; want 1", n, err)
	}
}

func TestAddAudit(t *testing.T) {
	s := openTest(t)
	e := AuditEvent{Time: time.Now(), Actor: "cli", Action: "key.create", Target: "chowki_abcde",
		Details: map[string]string{"name": "alice"}}
	if err := s.AddAudit(t.Context(), e); err != nil {
		t.Fatalf("AddAudit() error = %v", err)
	}
	var action, details string
	if err := s.db.QueryRowContext(t.Context(), "SELECT action, details FROM audit_log").Scan(&action, &details); err != nil ||
		action != "key.create" || details != `{"name":"alice"}` {
		t.Errorf("audit row = %q, %q, %v", action, details, err)
	}
}

func TestClosedDatabaseErrors(t *testing.T) {
	s := openTest(t)
	_ = s.Close()
	ctx := context.Background()
	checks := map[string]error{}
	_, checks["EnsureProject"] = s.EnsureProject(ctx, "p")
	_, checks["CreateKey"] = s.CreateKey(ctx, Key{})
	_, checks["KeyByPrefix"] = s.KeyByPrefix(ctx, "x")
	_, checks["ListKeys"] = s.ListKeys(ctx)
	_, checks["RevokeKey"] = s.RevokeKey(ctx, "x", time.Now())
	checks["InsertRequests"] = s.InsertRequests(ctx, []Request{{ID: "x"}})
	_, checks["DeleteRequestsBefore"] = s.DeleteRequestsBefore(ctx, time.Now())
	checks["AddAudit"] = s.AddAudit(ctx, AuditEvent{})
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s() on a closed database succeeded", name)
		}
	}
}
