package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestBackupRestoreCommands(t *testing.T) {
	initDir(t)
	runOK(t, "key", "create", "--name", "alice")
	out := runOK(t, "backup", "before.db")
	for _, want := range []string{"Backed up the database to before.db (", "Keep it with the master key, " +
		".chowki/master.key"} {
		if !strings.Contains(out, want) {
			t.Errorf("backup output lacks %q:\n%s", want, out)
		}
	}
	runOK(t, "key", "create", "--name", "bob")

	out = runOK(t, "restore", "before.db")
	if !strings.Contains(out, "Restored the database from before.db.") ||
		!strings.Contains(out, "The database that it replaced is in data/chowki.db.before-restore-") {
		t.Errorf("restore output:\n%s", out)
	}
	if out := runOK(t, "key", "list"); !strings.Contains(out, "alice") || strings.Contains(out, "bob") {
		t.Errorf("after the restore, key list:\n%s", out)
	}

	// Through stdout and stdin, as with docker compose exec -T.
	var backupData, note bytes.Buffer
	if code := run([]string{"backup", "-"}, &backupData, &note); code != exitOK ||
		!strings.HasPrefix(backupData.String(), "SQLite format 3\x00") ||
		!strings.Contains(note.String(), "Backed up the database to stdout") {
		t.Fatalf("backup - = %d, %d bytes, stderr %q", code, backupData.Len(), note.String())
	}
	runOK(t, "key", "create", "--name", "carol")
	withStdin(t, backupData.String())
	if out := runOK(t, "restore", "-"); !strings.Contains(out, "Restored the database from stdin.") {
		t.Errorf("restore - output:\n%s", out)
	}
	if out := runOK(t, "key", "list"); strings.Contains(out, "carol") {
		t.Errorf("after restore -, key list:\n%s", out)
	}

	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"backup", "before.db"}, exitError, "before.db exists; name a new file"},
		{[]string{"restore", "missing.db"}, exitError, "no such file"},
		{[]string{"backup"}, exitUsage, "Usage:"},
		{[]string{"restore", "a.db", "b.db"}, exitUsage, "Usage:"},
		{[]string{"backup", "--bogus", "x.db"}, exitUsage, "flag provided but not defined"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(tc.args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("run(%q) = %d, stderr %q; want %d with %q", tc.args, code, stderr.String(), tc.code, tc.want)
		}
	}
}
