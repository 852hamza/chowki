package testutil

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// UpdateDocsEnv names the environment variable that makes CheckGenerated
// rewrite the blocks it checks, as `make docs` does.
const UpdateDocsEnv = "UPDATE_DOCS"

// CheckGenerated checks that the lines of a file between
// "<!-- generated:NAME -->" and "<!-- end generated:NAME -->" are content,
// so that reference pages can't drift from the code that they describe.
// With UPDATE_DOCS=1, it writes content there instead. path is relative to
// the repository's root.
func CheckGenerated(t testing.TB, path, name, content string) {
	t.Helper()
	full := filepath.Join(repoRoot(t), filepath.FromSlash(path))
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	start, end := []byte("<!-- generated:"+name+" -->\n"), []byte("<!-- end generated:"+name+" -->")
	i, j := bytes.Index(data, start), bytes.Index(data, end)
	if i < 0 || j < i {
		t.Fatalf("%s has no block between %q and %q", path, start, end)
	}
	i += len(start)
	if string(data[i:j]) == content {
		return
	}
	if os.Getenv(UpdateDocsEnv) == "1" {
		info, err := os.Stat(full)
		if err != nil {
			t.Fatal(err)
		}
		updated := append(append(append([]byte{}, data[:i]...), content...), data[j:]...)
		//nolint:gosec // G703: the path is a constant of the calling test, in the repository
		if err := os.WriteFile(full, updated, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Errorf("the generated block %q of %s is out of date; run make docs. First difference:\n%s", name, path,
		firstDifference(string(data[i:j]), content))
}

// firstDifference returns the first line where got and want differ.
func firstDifference(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for n := range max(len(g), len(w)) {
		var gl, wl string
		if n < len(g) {
			gl = g[n]
		}
		if n < len(w) {
			wl = w[n]
		}
		if gl != wl {
			return "  in the file: " + gl + "\n  from code:   " + wl
		}
	}
	return ""
}

// repoRoot returns the folder that holds go.mod, above the test's folder.
func repoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's folder")
		}
		dir = parent
	}
}
