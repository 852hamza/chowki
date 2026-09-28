package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRelease serves a release the way GitHub does, for install.sh: the
// latest release redirects to its tag, and each tag has an archive of a
// stand-in chowki and checksums.txt. tamper breaks the checksum.
func fakeRelease(t *testing.T, version string, tamper bool) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	bin := []byte("#!/bin/sh\necho \"chowki " + version + "\"\n")
	if err := tw.WriteHeader(&tar.Header{Name: "chowki", Mode: 0o755, Size: int64(len(bin))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(bin); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	archive := fmt.Sprintf("chowki_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(buf.Bytes())
	if tamper {
		sum[0] ^= 1
	}
	checksums := hex.EncodeToString(sum[:]) + "  " + archive + "\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/"+version, http.StatusFound)
	})
	mux.HandleFunc("/tag/"+version, func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("/download/"+version+"/"+archive, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(buf.Bytes())
	})
	mux.HandleFunc("/download/"+version+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func runInstall(t *testing.T, env ...string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "sh", filepath.Join("..", "..", "install.sh"))
	cmd.Env = append(os.Environ(), append(env, "CHOWKI_INSTALL_DIR="+dir, "HOME="+t.TempDir())...)
	out, err := cmd.CombinedOutput()
	return dir, string(out), err
}

func TestInstallScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for Linux and macOS")
	}
	for _, tt := range []struct {
		name, version string
		pin           bool
	}{
		{"latest", "v0.1.0", false},
		{"pinned without the v", "v0.2.3", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := []string{"CHOWKI_RELEASES_URL=" + fakeRelease(t, tt.version, false).URL}
			if tt.pin {
				env = append(env, "CHOWKI_VERSION="+strings.TrimPrefix(tt.version, "v"))
			}
			dir, out, err := runInstall(t, env...)
			if err != nil {
				t.Fatalf("install.sh: %v\n%s", err, out)
			}
			got, err := exec.CommandContext(t.Context(), filepath.Join(dir, "chowki")).Output()
			if err != nil || strings.TrimSpace(string(got)) != "chowki "+tt.version {
				t.Errorf("installed binary printed %q, %v", got, err)
			}
			if !strings.Contains(out, "Installed chowki "+tt.version) {
				t.Errorf("output:\n%s", out)
			}
		})
	}
}

// A download that doesn't match checksums.txt is never installed.
func TestInstallScriptChecksum(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for Linux and macOS")
	}
	dir, out, err := runInstall(t, "CHOWKI_RELEASES_URL="+fakeRelease(t, "v0.1.0", true).URL)
	if err == nil || !strings.Contains(out, "doesn't match checksums.txt") {
		t.Errorf("install.sh = %v\n%s\nwant a checksum error", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "chowki")); !os.IsNotExist(err) {
		t.Errorf("chowki was installed despite the checksum: %v", err)
	}
	_, out, err = runInstall(t, "CHOWKI_RELEASES_URL="+fakeRelease(t, "v0.1.0", false).URL, "CHOWKI_VERSION=v9.9.9")
	if err == nil || !strings.Contains(out, "check that the version exists") {
		t.Errorf("install.sh for a missing version = %v\n%s", err, out)
	}
}
