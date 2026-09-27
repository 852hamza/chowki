package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	t.Chdir(t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("chowki init = %d\nstderr: %s", code, stderr.String())
	}
	for _, want := range []string{"Created chowki.yaml.", "Created the master key in .chowki/master.key.",
		"The database is ready: file:data/chowki.db.", "chowki key create"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output doesn't contain %q:\n%s", want, stdout.String())
		}
	}
	files := map[string]os.FileMode{"chowki.yaml": 0o600, ".chowki/master.key": 0o600, "data/chowki.db": 0o600}
	for name, perm := range files {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != perm {
			t.Errorf("%s mode = %o, want %o", name, info.Mode().Perm(), perm)
		}
	}
	key, err := os.ReadFile(".chowki/master.key")
	if err != nil {
		t.Fatal(err)
	}

	// A second run keeps everything.
	stdout.Reset()
	if code := run([]string{"init"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("second chowki init = %d\nstderr: %s", code, stderr.String())
	}
	for _, want := range []string{"Using the existing chowki.yaml.", "Using the existing master key"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output doesn't contain %q:\n%s", want, stdout.String())
		}
	}
	if again, _ := os.ReadFile(".chowki/master.key"); !bytes.Equal(again, key) {
		t.Error("the second run replaced the master key")
	}
}

func TestInitWithMasterKeyEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CHOWKI_MASTER_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--config", "custom.yaml"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("chowki init = %d\nstderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Using the master key from CHOWKI_MASTER_KEY.") {
		t.Errorf("output = %s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(".chowki", "master.key")); !os.IsNotExist(err) {
		t.Errorf("init wrote a key file although CHOWKI_MASTER_KEY is set: %v", err)
	}
}

func TestInitErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T)
		args  []string
		code  int
		want  string
	}{
		{"extra argument", nil, []string{"init", "extra"}, exitUsage, ""},
		{"unknown flag", nil, []string{"init", "--bogus"}, exitUsage, "flag provided but not defined"},
		{"invalid config", func(t *testing.T) {
			if err := os.WriteFile("chowki.yaml", []byte("log:\n  level: loud\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, []string{"init"}, exitError, "log.level"},
		{"bad .env", func(t *testing.T) {
			if err := os.WriteFile(".env", []byte("NOT A LINE\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, []string{"init"}, exitError, ".env: line 1"},
		{"unreadable key file", func(t *testing.T) {
			if err := os.MkdirAll(".chowki", 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(".chowki/master.key", []byte("not a key"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, []string{"init"}, exitError, "want 32 random bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tt.setup != nil {
				tt.setup(t)
			}
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != tt.code {
				t.Errorf("run(%q) = %d, want %d\nstderr: %s", tt.args, code, tt.code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr doesn't contain %q:\n%s", tt.want, stderr.String())
			}
		})
	}
}
