package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
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

func TestInspectSQLite(t *testing.T) {
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	latest := len(files) // the migrations are numbered from 1 without gaps
	dir := t.TempDir()
	migrated := filepath.Join(dir, "migrated.db")
	s, err := OpenSQLite(t.Context(), "file:"+migrated)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.EnsureProject(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProviderKey(t.Context(), ProviderKey{Provider: "openai", Sealed: []byte{7},
		UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for i, revoked := range []bool{false, false, true} {
		k, err := s.CreateKey(t.Context(), Key{ProjectID: p.ID, Name: "app", Prefix: fmt.Sprintf("chowki_k%d", i),
			Hash: [32]byte{byte(i)}, CreatedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		if revoked {
			if _, err := s.RevokeKey(t.Context(), k.Prefix, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = s.Close()
	newer := filepath.Join(dir, "newer.db")
	s, err = OpenSQLite(t.Context(), "file:"+newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), "INSERT INTO schema_migrations VALUES (999, 0)"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	empty := filepath.Join(dir, "empty.db")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, dsn    string
		want         Inspection
		wantNotExist bool
	}{
		{"migrated", "file:" + migrated, Inspection{Path: migrated, Version: latest, Latest: latest, Keys: 2,
			ProviderKeys: map[string][]byte{"openai": {7}}}, false},
		{"newer", "file:" + newer, Inspection{Path: newer, Version: 999, Latest: latest}, false},
		{"never migrated", "file:" + empty, Inspection{Path: empty, Latest: latest}, false},
		{"in memory", "file::memory:", Inspection{Latest: latest}, false},
		{"missing", "file:" + filepath.Join(dir, "missing.db"),
			Inspection{Path: filepath.Join(dir, "missing.db"), Latest: latest}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InspectSQLite(t.Context(), tt.dsn)
			if tt.wantNotExist != errors.Is(err, fs.ErrNotExist) || !tt.wantNotExist && err != nil {
				t.Fatalf("InspectSQLite() error = %v, want not exist: %v", err, tt.wantNotExist)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("InspectSQLite() = %+v, want %+v", got, tt.want)
			}
		})
	}
	// Inspecting changes nothing: a missing database stays missing.
	if _, err := os.Stat(filepath.Join(dir, "missing.db")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("InspectSQLite() created the missing database: %v", err)
	}
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
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var versions int
	if err := s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil ||
		versions != len(files) {
		t.Errorf("schema_migrations has %d rows, %v; want %d", versions, err, len(files))
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
	if !reflect.DeepEqual(k, want) || k.Revoked() {
		t.Errorf("CreateKey() = %+v, want %+v", k, want)
	}
	if _, err := s.CreateKey(ctx, Key{ProjectID: p.ID, Name: "bob", Prefix: "chowki_abcde", CreatedAt: created}); !errors.Is(err, ErrExists) {
		t.Errorf("CreateKey() with a used prefix: error = %v, want ErrExists", err)
	}
	if _, err := s.CreateKey(ctx, Key{ProjectID: 999, Name: "x", Prefix: "chowki_other", CreatedAt: created}); err == nil {
		t.Error("CreateKey() accepted an unknown project")
	}
	if got, err := s.KeyByPrefix(ctx, "chowki_abcde"); err != nil || !reflect.DeepEqual(got, want) {
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

func usd(v float64) *float64 { return &v }

func TestBudgets(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p, err := s.EnsureProject(ctx, "team")
	if err != nil || p.BudgetUSD != 0 {
		t.Fatalf("EnsureProject() = %+v, %v; want no budget", p, err)
	}
	k, err := s.CreateKey(ctx, Key{ProjectID: p.ID, Name: "alice", Prefix: "chowki_abcde", BudgetUSD: 50,
		CreatedAt: time.Now()})
	if err != nil || k.BudgetUSD != 50 || k.ProjectBudgetUSD != 0 {
		t.Fatalf("CreateKey() = %+v, %v; want a budget of 50", k, err)
	}

	if p, err := s.UpdateProject(ctx, "team", ProjectUpdate{BudgetUSD: usd(200)}); err != nil || p.BudgetUSD != 200 {
		t.Errorf("UpdateProject() = %+v, %v; want a budget of 200", p, err)
	}
	if k, err := s.UpdateKey(ctx, "chowki_abcde", KeyUpdate{BudgetUSD: usd(75)}); err != nil || k.BudgetUSD != 75 ||
		k.ProjectBudgetUSD != 200 {
		t.Errorf("UpdateKey() = %+v, %v; want budgets of 75 and 200", k, err)
	}
	if k, err := s.UpdateKey(ctx, "chowki_abcde", KeyUpdate{}); err != nil || k.BudgetUSD != 75 {
		t.Errorf("UpdateKey() with no changes = %+v, %v; want the budget kept", k, err)
	}
	// 0 removes a budget, which is stored as NULL.
	if k, err := s.UpdateKey(ctx, "chowki_abcde", KeyUpdate{BudgetUSD: usd(0)}); err != nil || k.BudgetUSD != 0 {
		t.Errorf("UpdateKey() to 0 = %+v, %v; want no budget", k, err)
	}
	var null bool
	if err := s.db.QueryRowContext(ctx, `SELECT monthly_budget_usd IS NULL FROM virtual_keys`).Scan(&null); err != nil || !null {
		t.Errorf("a removed budget is stored as NULL = %v, %v", null, err)
	}
	if _, err := s.UpdateKey(ctx, "chowki_zzzzz", KeyUpdate{BudgetUSD: usd(1)}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateKey() of an unknown key: error = %v, want ErrNotFound", err)
	}
	if _, err := s.UpdateProject(ctx, "nope", ProjectUpdate{BudgetUSD: usd(1)}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateProject() of an unknown project: error = %v, want ErrNotFound", err)
	}

	if _, err := s.EnsureProject(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	ps, err := s.ListProjects(ctx)
	if err != nil || len(ps) != 2 || ps[0].Name != "alpha" || ps[1].Name != "team" || ps[1].BudgetUSD != 200 {
		t.Errorf("ListProjects() = %+v, %v; want alpha, then team with a budget of 200", ps, err)
	}
}

func TestKeyRateLimits(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p, err := s.EnsureProject(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.CreateKey(ctx, Key{ProjectID: p.ID, Name: "bot", Prefix: "chowki_abcde", RPM: 60, TPM: 100_000,
		CreatedAt: time.Now()})
	if err != nil || k.RPM != 60 || k.TPM != 100_000 {
		t.Fatalf("CreateKey() = %+v, %v; want 60 RPM and 100000 TPM", k, err)
	}
	n := func(v int64) *int64 { return &v }
	// Only the given settings change; 0 removes a limit.
	k, err = s.UpdateKey(ctx, "chowki_abcde", KeyUpdate{RPM: n(120), TPM: n(0), BudgetUSD: usd(9)})
	if err != nil || k.RPM != 120 || k.TPM != 0 || k.BudgetUSD != 9 {
		t.Errorf("UpdateKey() = %+v, %v; want 120 RPM, no TPM and a budget of 9", k, err)
	}
	if k, err := s.UpdateKey(ctx, "chowki_abcde", KeyUpdate{TPM: n(5000)}); err != nil || k.RPM != 120 || k.TPM != 5000 ||
		k.BudgetUSD != 9 {
		t.Errorf("UpdateKey() of TPM only = %+v, %v", k, err)
	}
	mode := func(v string) *string { return &v }
	if k, err := s.UpdateKey(ctx, "chowki_abcde", KeyUpdate{CacheMode: mode("exact")}); err != nil ||
		k.CacheMode != "exact" || k.TPM != 5000 {
		t.Errorf("UpdateKey() of the cache mode = %+v, %v", k, err)
	}
	if k, err := s.UpdateKey(ctx, "chowki_abcde", KeyUpdate{CacheMode: mode("")}); err != nil || k.CacheMode != "" {
		t.Errorf("UpdateKey() back to the default cache mode = %+v, %v", k, err)
	}
	if k, err := s.UpdateKey(ctx, "chowki_abcde", KeyUpdate{RedactionMode: mode("block")}); err != nil ||
		k.RedactionMode != "block" || k.TPM != 5000 {
		t.Errorf("UpdateKey() of the redaction mode = %+v, %v", k, err)
	}
	models := []string{"fast", "openai/*"}
	if k, err := s.UpdateKey(ctx, "chowki_abcde", KeyUpdate{AllowedModels: &models}); err != nil ||
		!reflect.DeepEqual(k.AllowedModels, models) || k.RedactionMode != "block" {
		t.Errorf("UpdateKey() of the allowed models = %+v, %v", k, err)
	}
	if k, err := s.UpdateKey(ctx, "chowki_abcde", KeyUpdate{AllowedModels: &[]string{}}); err != nil ||
		k.AllowedModels != nil {
		t.Errorf("UpdateKey() allowing every model = %+v, %v", k, err)
	}
}

func TestSpend(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p, err := s.EnsureProject(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, prefix := range []string{"chowki_aaaaa", "chowki_bbbbb"} {
		k, err := s.CreateKey(ctx, Key{ProjectID: p.ID, Name: prefix, Prefix: prefix, CreatedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, k.ID)
	}
	// October 1 at 03:00 in UTC+5 is still September in UTC.
	sept := time.Date(2026, 10, 1, 3, 0, 0, 0, time.FixedZone("UTC+5", 5*60*60))
	oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rs := []Request{
		{ID: "r1", Time: sept, KeyID: ids[0], ProjectID: p.ID, CostUSD: usd(1.25)},
		{ID: "r2", Time: sept, KeyID: ids[0], ProjectID: p.ID, CostUSD: usd(0.5)},
		{ID: "r3", Time: sept, KeyID: ids[1], ProjectID: p.ID}, // unpriced
		{ID: "r4", Time: oct, KeyID: ids[1], ProjectID: p.ID, CostUSD: usd(2)},
	}
	// The second batch adds to the first.
	for _, batch := range [][]Request{rs[:1], rs[1:]} {
		if err := s.InsertRequests(ctx, batch); err != nil {
			t.Fatal(err)
		}
	}
	// A batch that fails adds no spend.
	if err := s.InsertRequests(ctx, []Request{{ID: "r1", Time: sept, KeyID: ids[0], CostUSD: usd(100)}}); err == nil {
		t.Fatal("InsertRequests() accepted a duplicate ID")
	}
	// Spend outlives the request records.
	if _, err := s.DeleteRequestsBefore(ctx, oct.AddDate(1, 0, 0)); err != nil {
		t.Fatal(err)
	}

	for period, want := range map[string][]KeySpend{
		"2026-09": {{KeyID: ids[0], ProjectID: p.ID, USD: 1.75}},
		"2026-10": {{KeyID: ids[1], ProjectID: p.ID, USD: 2}},
		"2026-11": nil,
	} {
		if got, err := s.SpendByKey(ctx, period); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("SpendByKey(%s) = %+v, %v; want %+v", period, got, err, want)
		}
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
			CostUSD: &cost, SavingsUSD: 0.001, SavingsMethod: "prompt_cache", Redactions: map[string]int{"email": 2}},
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

	var redactions []string
	rows, err = s.db.QueryContext(ctx, `SELECT redactions FROM requests ORDER BY ts`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		redactions = append(redactions, r)
	}
	_ = rows.Close()
	if !reflect.DeepEqual(redactions, []string{`{"email":2}`, ""}) {
		t.Errorf("stored redactions = %q, want the counts and nothing", redactions)
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
	_, checks["UpdateKey"] = s.UpdateKey(ctx, "x", KeyUpdate{BudgetUSD: usd(1)})
	_, checks["ListProjects"] = s.ListProjects(ctx)
	_, checks["UpdateProject"] = s.UpdateProject(ctx, "p", ProjectUpdate{BudgetUSD: usd(1)})
	checks["InsertRequests"] = s.InsertRequests(ctx, []Request{{ID: "x"}})
	_, checks["SpendByKey"] = s.SpendByKey(ctx, "2026-09")
	_, checks["CacheEntry"] = s.CacheEntry(ctx, []byte("h"), time.Now())
	_, checks["PutCacheEntry"] = s.PutCacheEntry(ctx, CacheEntry{Hash: []byte("h")})
	checks["TouchCacheEntry"] = s.TouchCacheEntry(ctx, []byte("h"), time.Now())
	_, checks["DeleteExpiredCacheEntries"] = s.DeleteExpiredCacheEntries(ctx, time.Now())
	_, checks["EvictCacheEntries"] = s.EvictCacheEntries(ctx, 1)
	_, checks["CacheSize"] = s.CacheSize(ctx)
	_, checks["DeleteRequestsBefore"] = s.DeleteRequestsBefore(ctx, time.Now())
	checks["AddAudit"] = s.AddAudit(ctx, AuditEvent{})
	checks["Ping"] = s.Ping(ctx)
	_, checks["CreateAdminToken"] = s.CreateAdminToken(ctx, AdminToken{})
	_, checks["AdminTokenByPrefix"] = s.AdminTokenByPrefix(ctx, "x")
	_, checks["ListAdminTokens"] = s.ListAdminTokens(ctx)
	_, checks["RevokeAdminToken"] = s.RevokeAdminToken(ctx, "x", time.Now())
	_, checks["Totals"] = s.Totals(ctx, time.Now(), time.Now())
	_, checks["Breakdown"] = s.Breakdown(ctx, ByKey, time.Now(), time.Now())
	_, checks["RecentRequests"] = s.RecentRequests(ctx, time.Now(), 1)
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s() on a closed database succeeded", name)
		}
	}
}
