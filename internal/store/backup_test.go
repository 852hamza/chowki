package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBackupRestore(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "data", "chowki.db")
	s, err := OpenSQLite(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.EnsureProject(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	addKey := func(prefix string) {
		t.Helper()
		if _, err := s.CreateKey(t.Context(), Key{ProjectID: p.ID, Name: "app", Prefix: prefix,
			CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	addKey("chowki_one00")

	// A backup while the database is open, as while the gateway runs.
	backup := filepath.Join(dir, "backup.db")
	if err := BackupSQLite(t.Context(), dsn, backup); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(backup); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("backup file: %v, %v", info, err)
	}
	if err := BackupSQLite(t.Context(), dsn, backup); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("a second backup to the same file: %v", err)
	}
	addKey("chowki_two00")
	_ = s.Close()

	now := time.Date(2026, 9, 28, 10, 30, 0, 0, time.UTC)
	kept, err := RestoreSQLite(t.Context(), dsn, backup, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "data", "chowki.db.before-restore-20260928-103000"); kept != want {
		t.Errorf("kept %q, want %q", kept, want)
	}
	s, err = OpenSQLite(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := s.ListKeys(t.Context())
	_ = s.Close()
	if err != nil || len(keys) != 1 || keys[0].Prefix != "chowki_one00" {
		t.Errorf("after the restore, keys = %+v, %v; want the one before the backup", keys, err)
	}
	s, err = OpenSQLite(t.Context(), "file:"+kept)
	if err != nil {
		t.Fatal(err)
	}
	if keys, _ := s.ListKeys(t.Context()); len(keys) != 2 {
		t.Errorf("the kept database has %d keys, want 2", len(keys))
	}
	_ = s.Close()
}

func TestRestoreRefuses(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "chowki.db")
	garbage := filepath.Join(dir, "garbage.db")
	if err := os.WriteFile(garbage, []byte("not a database, not at all, not even close to one"), 0o600); err != nil {
		t.Fatal(err)
	}
	newer := filepath.Join(dir, "newer.db")
	s, err := OpenSQLite(t.Context(), "file:"+newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), "INSERT INTO schema_migrations VALUES (999, 0)"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	for file, want := range map[string]string{garbage: "isn't a Chowki database",
		newer: "use a newer Chowki", filepath.Join(dir, "missing.db"): "no such file"} {
		if _, err := RestoreSQLite(t.Context(), dsn, file, time.Now()); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("RestoreSQLite(%s) = %v, want %q", filepath.Base(file), err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "chowki.db")); err == nil {
		t.Error("a refused restore created the database")
	}
	if err := BackupSQLite(t.Context(), dsn, filepath.Join(dir, "b.db")); err == nil {
		t.Error("a backup of a missing database succeeded")
	}
	if err := BackupSQLite(t.Context(), "file::memory:", filepath.Join(dir, "b.db")); err == nil {
		t.Error("a backup of an in-memory database succeeded")
	}
}
