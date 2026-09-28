package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

//go:embed migrations/*.sql
var migrations embed.FS

// SQLite is the SQLite implementation of Store.
type SQLite struct {
	db *sql.DB
}

var _ Store = (*SQLite)(nil)

// OpenSQLite opens the SQLite database at dsn, such as
// "file:data/chowki.db", creating it if needed, and applies pending
// migrations. A new database file and its folder are readable only by the
// current user.
func OpenSQLite(ctx context.Context, dsn string) (*SQLite, error) {
	full, path, err := prepareDSN(dsn)
	if err != nil {
		return nil, err
	}
	if path != "" {
		if err := createPrivateFile(path); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", full)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	s := &SQLite{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// prepareDSN adds the connection settings Chowki relies on, unless the DSN
// sets them, and returns the database file path, or "" for an in-memory
// database.
func prepareDSN(dsn string) (full, path string, err error) {
	name, rawQuery, _ := strings.Cut(dsn, "?")
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", "", fmt.Errorf("storage.dsn: %w", err)
	}
	for key, value := range map[string]string{
		"_busy_timeout": "5000",   // wait for the single writer instead of failing
		"_journal_mode": "WAL",    // readers don't block the writer
		"_synchronous":  "NORMAL", // safe with WAL, and much faster than FULL
		"_foreign_keys": "on",
	} {
		if !q.Has(key) {
			q.Set(key, value)
		}
	}
	path = strings.TrimPrefix(name, "file:")
	path = strings.TrimPrefix(path, "//")
	if path == "" || path == ":memory:" || strings.HasPrefix(path, ":memory:") || q.Get("mode") == "memory" {
		path = ""
	}
	return name + "?" + q.Encode(), path, nil
}

// createPrivateFile creates the database file with mode 0600, and its
// folder with mode 0700, so that SQLite doesn't create a world-readable
// file.
func createPrivateFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create database folder: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create database file: %w", err)
	}
	return f.Close()
}

func (s *SQLite) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	var current int
	if err := s.db.QueryRowContext(ctx, `SELECT coalesce(max(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	ms, err := migrationList()
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	for _, m := range ms {
		if m.version <= current {
			continue
		}
		body, err := migrations.ReadFile(m.name)
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		if err := s.inTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
				m.version, time.Now().UnixMilli())
			return err
		}); err != nil {
			return fmt.Errorf("migrate to version %d: %w", m.version, err)
		}
	}
	if latest := ms[len(ms)-1].version; current > latest {
		return fmt.Errorf("migrate: the database has schema version %d, but this Chowki knows only up to %d; "+
			"use a newer Chowki", current, latest)
	}
	return nil
}

// migration is a schema change, in migrations/<version>_<name>.sql.
type migration struct {
	version int
	name    string
}

// migrationList returns the migrations in version order.
func migrationList() ([]migration, error) {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	ms := make([]migration, 0, len(names))
	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(filepath.Base(name), "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("bad migration name %s", name)
		}
		ms = append(ms, migration{version, name})
	}
	return ms, nil
}

// Inspection is what InspectSQLite finds in a database.
type Inspection struct {
	// Path is the database file, or "" for an in-memory database.
	Path string
	// Version is the database's schema version, 0 before any migration.
	Version int
	// Latest is the newest schema version that this Chowki knows.
	Latest int
	// Keys and AdminTokens count the virtual keys and the admin tokens that
	// aren't revoked, and ProviderKeys holds the sealed provider keys, by
	// provider, once the schema is up to date.
	Keys, AdminTokens int
	ProviderKeys      map[string][]byte
}

// InspectSQLite reads the schema version of the SQLite database at dsn, and
// counts its keys. It neither creates the database nor migrates it, so that
// checks such as chowki doctor change nothing. A database file that doesn't
// exist yet returns an error that wraps fs.ErrNotExist.
func InspectSQLite(ctx context.Context, dsn string) (Inspection, error) {
	ms, err := migrationList()
	if err != nil {
		return Inspection{}, err
	}
	full, path, err := prepareDSN(dsn)
	if err != nil {
		return Inspection{}, err
	}
	in := Inspection{Path: path, Latest: ms[len(ms)-1].version}
	if path == "" {
		return in, nil // an in-memory database starts empty every time
	}
	if _, err := os.Stat(path); err != nil {
		return in, fmt.Errorf("database: %w", err)
	}
	// mode=rw opens the file without creating it, should it go away.
	db, err := sql.Open("sqlite", full+"&mode=rw")
	if err != nil {
		return in, fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }() // nothing was written
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND
		name = 'schema_migrations'`).Scan(&tables); err != nil {
		return in, fmt.Errorf("open database: %w", err)
	}
	if tables == 0 {
		return in, nil
	}
	if err := db.QueryRowContext(ctx, `SELECT coalesce(max(version), 0) FROM schema_migrations`).
		Scan(&in.Version); err != nil {
		return in, fmt.Errorf("read schema version: %w", err)
	}
	if in.Version != in.Latest {
		return in, nil // the tables may differ from what this Chowki knows
	}
	if err := db.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM virtual_keys WHERE revoked_at IS NULL),
		(SELECT count(*) FROM admin_tokens WHERE revoked_at IS NULL)`).Scan(&in.Keys, &in.AdminTokens); err != nil {
		return in, fmt.Errorf("count keys: %w", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT provider, sealed FROM provider_keys`)
	if err != nil {
		return in, fmt.Errorf("read provider keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	in.ProviderKeys = map[string][]byte{}
	for rows.Next() {
		var name string
		var sealed []byte
		if err := rows.Scan(&name, &sealed); err != nil {
			return in, fmt.Errorf("read provider keys: %w", err)
		}
		in.ProviderKeys[name] = sealed
	}
	if err := rows.Err(); err != nil {
		return in, fmt.Errorf("read provider keys: %w", err)
	}
	return in, nil
}

func (s *SQLite) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback() // the transaction failed; its error is the one to report
		return err
	}
	return tx.Commit()
}

// EnsureProject implements Store.
func (s *SQLite) EnsureProject(ctx context.Context, name string) (Project, error) {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO projects (name, created_at) VALUES (?, ?)
		ON CONFLICT (name) DO NOTHING`, name, time.Now().UnixMilli()); err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	p, err := s.projectWhere(ctx, name)
	if err != nil {
		return Project{}, fmt.Errorf("read project: %w", err)
	}
	return p, nil
}

const projectColumns = `SELECT id, name, monthly_budget_usd, created_at FROM projects`

func (s *SQLite) projectWhere(ctx context.Context, name string) (Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, projectColumns+" WHERE name = ?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	var budget sql.NullFloat64
	var created int64
	if err := row.Scan(&p.ID, &p.Name, &budget, &created); err != nil {
		return Project{}, err
	}
	p.BudgetUSD = budget.Float64
	p.CreatedAt = time.UnixMilli(created).UTC()
	return p, nil
}

// ListProjects implements Store.
func (s *SQLite) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, projectColumns+" ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ps []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("list projects: %w", err)
		}
		ps = append(ps, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return ps, nil
}

// UpdateProject implements Store.
func (s *SQLite) UpdateProject(ctx context.Context, name string, u ProjectUpdate) (Project, error) {
	if u.BudgetUSD != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE projects SET monthly_budget_usd = ? WHERE name = ?`,
			nullIfZero(*u.BudgetUSD), name); err != nil {
			return Project{}, fmt.Errorf("update project: %w", err)
		}
	}
	p, err := s.projectWhere(ctx, name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Project{}, fmt.Errorf("read project: %w", err)
	}
	return p, err
}

// nullIfZero stores the zero value, which means "no limit" or "the
// default", as NULL.
func nullIfZero[T int64 | float64 | string](v T) any {
	var zero T
	if v == zero {
		return nil
	}
	return v
}

// CreateKey implements Store.
func (s *SQLite) CreateKey(ctx context.Context, k Key) (Key, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO virtual_keys (project_id, name, prefix, key_hash,
		monthly_budget_usd, rpm, tpm, cache_mode, redaction_mode, allowed_models, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.ProjectID, k.Name, k.Prefix, k.Hash[:], nullIfZero(k.BudgetUSD), nullIfZero(k.RPM), nullIfZero(k.TPM),
		nullIfZero(k.CacheMode), nullIfZero(k.RedactionMode), modelList(k.AllowedModels), k.CreatedAt.UnixMilli())
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		return Key{}, fmt.Errorf("create key: prefix %s: %w", k.Prefix, ErrExists)
	}
	if err != nil {
		return Key{}, fmt.Errorf("create key: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Key{}, fmt.Errorf("create key: %w", err)
	}
	return s.keyWhere(ctx, "k.id = ?", id)
}

// KeyByPrefix implements Store.
func (s *SQLite) KeyByPrefix(ctx context.Context, prefix string) (Key, error) {
	return s.keyWhere(ctx, "k.prefix = ?", prefix)
}

const keyColumns = `SELECT k.id, k.project_id, p.name, k.name, k.prefix, k.key_hash, k.monthly_budget_usd,
	p.monthly_budget_usd, k.rpm, k.tpm, k.cache_mode, k.redaction_mode, k.allowed_models, k.created_at, k.revoked_at
	FROM virtual_keys k JOIN projects p ON p.id = k.project_id`

func (s *SQLite) keyWhere(ctx context.Context, where string, arg any) (Key, error) {
	k, err := scanKey(s.db.QueryRowContext(ctx, keyColumns+" WHERE "+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return Key{}, ErrNotFound
	}
	if err != nil {
		return Key{}, fmt.Errorf("read key: %w", err)
	}
	return k, nil
}

// ListKeys implements Store.
func (s *SQLite) ListKeys(ctx context.Context) ([]Key, error) {
	rows, err := s.db.QueryContext(ctx, keyColumns+" ORDER BY k.created_at, k.id")
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []Key
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, fmt.Errorf("list keys: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	return keys, nil
}

func scanKey(row interface{ Scan(...any) error }) (Key, error) {
	var k Key
	var hash []byte
	var budget, projectBudget sql.NullFloat64
	var rpm, tpm, revoked sql.NullInt64
	var cacheMode, redactionMode, models sql.NullString
	var created int64
	if err := row.Scan(&k.ID, &k.ProjectID, &k.Project, &k.Name, &k.Prefix, &hash, &budget, &projectBudget,
		&rpm, &tpm, &cacheMode, &redactionMode, &models, &created, &revoked); err != nil {
		return Key{}, err
	}
	if models.Valid {
		if err := json.Unmarshal([]byte(models.String), &k.AllowedModels); err != nil {
			return Key{}, fmt.Errorf("key %s has invalid allowed models: %w", k.Prefix, err)
		}
	}
	if len(hash) != len(k.Hash) {
		return Key{}, fmt.Errorf("key %s has a %d-byte hash", k.Prefix, len(hash))
	}
	copy(k.Hash[:], hash)
	k.BudgetUSD, k.ProjectBudgetUSD = budget.Float64, projectBudget.Float64
	k.RPM, k.TPM, k.CacheMode, k.RedactionMode = rpm.Int64, tpm.Int64, cacheMode.String, redactionMode.String
	k.CreatedAt = time.UnixMilli(created).UTC()
	if revoked.Valid {
		k.RevokedAt = time.UnixMilli(revoked.Int64).UTC()
	}
	return k, nil
}

// RevokeKey implements Store.
func (s *SQLite) RevokeKey(ctx context.Context, prefix string, at time.Time) (Key, error) {
	if _, err := s.db.ExecContext(ctx, `UPDATE virtual_keys SET revoked_at = ?
		WHERE prefix = ? AND revoked_at IS NULL`, at.UnixMilli(), prefix); err != nil {
		return Key{}, fmt.Errorf("revoke key: %w", err)
	}
	return s.KeyByPrefix(ctx, prefix)
}

// UpdateKey implements Store.
func (s *SQLite) UpdateKey(ctx context.Context, prefix string, u KeyUpdate) (Key, error) {
	// Each setting is a pair of arguments: whether to change it, and its value.
	if _, err := s.db.ExecContext(ctx, `UPDATE virtual_keys SET
		monthly_budget_usd = CASE WHEN ? THEN ? ELSE monthly_budget_usd END,
		rpm = CASE WHEN ? THEN ? ELSE rpm END,
		tpm = CASE WHEN ? THEN ? ELSE tpm END,
		cache_mode = CASE WHEN ? THEN ? ELSE cache_mode END,
		redaction_mode = CASE WHEN ? THEN ? ELSE redaction_mode END,
		allowed_models = CASE WHEN ? THEN ? ELSE allowed_models END
		WHERE prefix = ?`,
		u.BudgetUSD != nil, optional(u.BudgetUSD), u.RPM != nil, optional(u.RPM), u.TPM != nil, optional(u.TPM),
		u.CacheMode != nil, optional(u.CacheMode), u.RedactionMode != nil, optional(u.RedactionMode),
		u.AllowedModels != nil, allowedModels(u.AllowedModels), prefix); err != nil {
		return Key{}, fmt.Errorf("update key: %w", err)
	}
	return s.KeyByPrefix(ctx, prefix)
}

// modelList stores a list of models as a JSON array, or NULL when it's
// empty and every model is allowed.
func modelList(models []string) any {
	if len(models) == 0 {
		return nil
	}
	b, _ := json.Marshal(models) // strings always marshal
	return string(b)
}

func allowedModels(models *[]string) any {
	if models == nil {
		return nil
	}
	return modelList(*models)
}

// optional returns what to store for a setting that may be unset: NULL when
// it's unset or zero.
func optional[T int64 | float64 | string](v *T) any {
	if v == nil {
		return nil
	}
	return nullIfZero(*v)
}

// InsertRequests implements Store.
func (s *SQLite) InsertRequests(ctx context.Context, rs []Request) error {
	type month struct {
		key    int64
		period string
	}
	spend := map[month]float64{}
	for _, r := range rs {
		if r.CostUSD != nil && *r.CostUSD > 0 {
			spend[month{r.KeyID, Period(r.Time)}] += *r.CostUSD
		}
	}
	d := sumDaily(rs)
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		for m, usd := range spend {
			if _, err := tx.ExecContext(ctx, `INSERT INTO spend (key_id, period, spent_usd) VALUES (?, ?, ?)
				ON CONFLICT (key_id, period) DO UPDATE SET spent_usd = spent_usd + excluded.spent_usd`,
				m.key, m.period, usd); err != nil {
				return err
			}
		}
		if err := d.save(ctx, tx); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO requests (id, ts, key_id, project_id, api_family,
			endpoint, provider, model, stream, status, error_type, latency_ms, ttfb_ms, input_tokens,
			output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens, cost_usd, savings_usd,
			savings_method, cache_status, redactions)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, r := range rs {
			var ttfb, cost any
			if r.TTFB > 0 {
				ttfb = r.TTFB.Milliseconds()
			}
			if r.CostUSD != nil {
				cost = *r.CostUSD
			}
			var redactions string
			if len(r.Redactions) > 0 {
				b, _ := json.Marshal(r.Redactions) // a map of counts always marshals
				redactions = string(b)
			}
			tokens := [5]any{}
			if t := r.Tokens; t != nil {
				tokens = [5]any{t.Input, t.Output, t.CacheRead, t.CacheWrite, t.Reasoning}
			}
			if _, err := stmt.ExecContext(ctx, r.ID, r.Time.UnixMilli(), r.KeyID, r.ProjectID, r.APIFamily,
				r.Endpoint, r.Provider, r.Model, r.Stream, r.Status, r.ErrorType, r.Latency.Milliseconds(), ttfb,
				tokens[0], tokens[1], tokens[2], tokens[3], tokens[4], cost, r.SavingsUSD, r.SavingsMethod,
				r.CacheStatus, redactions); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save requests: %w", err)
	}
	return nil
}

// SpendByKey implements Store.
func (s *SQLite) SpendByKey(ctx context.Context, period string) ([]KeySpend, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.key_id, k.project_id, s.spent_usd FROM spend s
		JOIN virtual_keys k ON k.id = s.key_id WHERE s.period = ? ORDER BY s.key_id`, period)
	if err != nil {
		return nil, fmt.Errorf("read spend: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var spend []KeySpend
	for rows.Next() {
		var ks KeySpend
		if err := rows.Scan(&ks.KeyID, &ks.ProjectID, &ks.USD); err != nil {
			return nil, fmt.Errorf("read spend: %w", err)
		}
		spend = append(spend, ks)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read spend: %w", err)
	}
	return spend, nil
}

// DeleteRequestsBefore implements Store.
func (s *SQLite) DeleteRequestsBefore(ctx context.Context, t time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM requests WHERE ts < ?`, t.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("delete old requests: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete old requests: %w", err)
	}
	return n, nil
}

// AddAudit implements Store.
func (s *SQLite) AddAudit(ctx context.Context, e AuditEvent) error {
	details, err := json.Marshal(e.Details)
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO audit_log (ts, actor, action, target, details)
		VALUES (?, ?, ?, ?, ?)`, e.Time.UnixMilli(), e.Actor, e.Action, e.Target, string(details)); err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return nil
}

// Ping implements Store.
func (s *SQLite) Ping(ctx context.Context) error {
	var one int
	if err := s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// Close implements Store.
func (s *SQLite) Close() error { return s.db.Close() }
