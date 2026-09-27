package secretbox

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// KeySize is the size of the master key: 32 bytes, for AES-256.
const KeySize = 32

// MasterKeyEnv is the environment variable that can hold the master key,
// base64-encoded. It takes precedence over the key file.
const MasterKeyEnv = "CHOWKI_MASTER_KEY"

// CreateKeyFile writes a new random master key to path, base64-encoded,
// readable only by the current user. It fails if the file exists, so an
// existing key is never replaced.
func CreateKeyFile(path string) error {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("create master key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create master key folder: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create master key: %w", err)
	}
	_, err = f.WriteString(base64.StdEncoding.EncodeToString(key) + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("create master key: %w", err)
	}
	return nil
}

// LoadKey returns the master key from the CHOWKI_MASTER_KEY environment
// variable or, when it's unset, from the file at path. On Unix, it refuses a
// key file that other users can read. Errors never contain the key.
func LoadKey(path string, env func(string) (string, bool)) ([]byte, error) {
	if v, ok := env(MasterKeyEnv); ok && v != "" {
		key, err := decodeKey(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", MasterKeyEnv, err)
		}
		return key, nil
	}
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("master key file %s doesn't exist; run chowki init, or set %s", path, MasterKeyEnv)
	}
	if err != nil {
		return nil, fmt.Errorf("master key: %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("master key file %s is readable by other users; run chmod 600 %s", path, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("master key: %w", err)
	}
	key, err := decodeKey(string(data))
	if err != nil {
		return nil, fmt.Errorf("master key file %s: %w", path, err)
	}
	return key, nil
}

func decodeKey(s string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(key) != KeySize {
		return nil, fmt.Errorf("want %d random bytes in base64, as chowki init creates", KeySize)
	}
	return key, nil
}
