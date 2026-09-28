package secretbox

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func noEnv(string) (string, bool) { return "", false }

func TestCreateAndLoadKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "master.key")
	if err := CreateKeyFile(path); err != nil {
		t.Fatalf("CreateKeyFile() error = %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("key file mode = %o, want 600", perm)
		}
	}
	key, err := LoadKey(path, noEnv)
	if err != nil || len(key) != KeySize {
		t.Fatalf("LoadKey() = %d bytes, %v", len(key), err)
	}
	if err := CreateKeyFile(path); err == nil {
		t.Error("CreateKeyFile() replaced an existing key")
	}
	again, err := LoadKey(path, noEnv)
	if err != nil || !bytes.Equal(again, key) {
		t.Errorf("the key changed after a second CreateKeyFile()")
	}
}

func TestLoadKeyFromEnv(t *testing.T) {
	want := bytes.Repeat([]byte{7}, KeySize)
	env := func(name string) (string, bool) {
		if name == MasterKeyEnv {
			return base64.StdEncoding.EncodeToString(want), true
		}
		return "", false
	}
	got, err := LoadKey(filepath.Join(t.TempDir(), "missing.key"), env)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("LoadKey() = %x, %v; want the key from %s", got, err, MasterKeyEnv)
	}
}

func TestLoadKeyErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string, perm os.FileMode) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), perm); err != nil {
			t.Fatal(err)
		}
		return path
	}
	const secretLooking = "c2VjcmV0LXZhbHVlLXRoYXQtbXVzdC1ub3QtbGVhaw=="
	tests := []struct {
		name string
		path string
		env  func(string) (string, bool)
		want string
	}{
		{"missing file", filepath.Join(dir, "none.key"), noEnv, "run chowki init"},
		{"short key", write("short.key", base64.StdEncoding.EncodeToString([]byte("short")), 0o600), noEnv,
			"want 32 random bytes"},
		{"not base64", write("text.key", "not base64!", 0o600), noEnv, "want 32 random bytes"},
		{"bad env", filepath.Join(dir, "none.key"), func(string) (string, bool) { return secretLooking, true },
			MasterKeyEnv + ": want 32 random bytes"},
	}
	if runtime.GOOS != "windows" {
		tests = append(tests, struct {
			name string
			path string
			env  func(string) (string, bool)
			want string
		}{"readable by others", write("open.key", base64.StdEncoding.EncodeToString(make([]byte, KeySize)), 0o644),
			noEnv, "readable by other users"})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadKey(tt.path, tt.env)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadKey() error = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), secretLooking) {
				t.Errorf("error shows the key: %v", err)
			}
		})
	}
}
