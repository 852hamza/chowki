package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// BackupSQLite writes a consistent copy of the SQLite database at dsn to
// path, a new file readable only by the current user. It may run while the
// gateway runs: VACUUM INTO reads one snapshot. It neither creates nor
// migrates the database, so a backup before an upgrade keeps the old schema.
func BackupSQLite(ctx context.Context, dsn, path string) error {
	db, err := openExisting(dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	// VACUUM INTO writes only to a new or empty file, which this makes
	// private first.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s exists; name a new file", path)
	}
	if err != nil {
		return fmt.Errorf("create the backup: %w", err)
	}
	_ = f.Close()
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("back up the database: %w", err)
	}
	return nil
}

// RestoreSQLite replaces the SQLite database at dsn with the backup at
// path, once the backup passes SQLite's integrity check and has a schema
// version that this Chowki knows. The gateway must be stopped. The
// replaced database, with its write-ahead log, stays next to it, named
// <name>.before-restore-<time>, which RestoreSQLite returns; "" when
// there was none.
func RestoreSQLite(ctx context.Context, dsn, path string, now time.Time) (kept string, err error) {
	if err := checkBackup(ctx, path); err != nil {
		return "", err
	}
	_, live, err := prepareDSN(dsn)
	if err != nil {
		return "", err
	}
	if live == "" {
		return "", errors.New("the database is in memory; there is nothing to restore into")
	}
	if err := os.MkdirAll(filepath.Dir(live), 0o700); err != nil {
		return "", fmt.Errorf("create database folder: %w", err)
	}
	// Copy first, so that a failure leaves the database as it was.
	tmp := live + ".restoring"
	if err := copyPrivate(path, tmp); err != nil {
		return "", err
	}
	if _, err := os.Stat(live); err == nil {
		kept = live + ".before-restore-" + now.UTC().Format("20060102-150405")
		for _, suffix := range []string{"", "-wal"} {
			if err := os.Rename(live+suffix, kept+suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
				_ = os.Remove(tmp)
				return "", fmt.Errorf("keep the replaced database: %w", err)
			}
		}
		_ = os.Remove(live + "-shm") // an index of the old log, rebuilt on open
	}
	if err := os.Rename(tmp, live); err != nil {
		return kept, fmt.Errorf("restore the database: %w", err)
	}
	return kept, nil
}

// checkBackup opens a backup read-only and checks its integrity and its
// schema version.
func checkBackup(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open the backup: %w", err)
	}
	defer func() { _ = db.Close() }()
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("%s isn't a Chowki database: %w", path, err)
	}
	if result != "ok" {
		return fmt.Errorf("%s is damaged: %s", path, result)
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT coalesce(max(version), 0) FROM schema_migrations`).
		Scan(&version); err != nil {
		return fmt.Errorf("%s isn't a Chowki database: %w", path, err)
	}
	ms, err := migrationList()
	if err != nil {
		return err
	}
	if latest := ms[len(ms)-1].version; version > latest {
		return fmt.Errorf("%s has schema version %d, but this Chowki knows only up to %d; use a newer Chowki",
			path, version, latest)
	}
	return nil
}

// openExisting opens the SQLite database at dsn without creating it.
func openExisting(dsn string) (*sql.DB, error) {
	full, path, err := prepareDSN(dsn)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("the database is in memory; there is nothing to back up")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	db, err := sql.Open("sqlite", full+"&mode=rw")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return db, nil
}

func copyPrivate(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("read the backup: %w", err)
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("restore the database: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(to)
		return fmt.Errorf("restore the database: %w", err)
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		return fmt.Errorf("restore the database: %w", err)
	}
	return dst.Close()
}
