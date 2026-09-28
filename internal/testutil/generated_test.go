package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recorder records the errors of CheckGenerated instead of failing.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func TestCheckGenerated(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{"go.mod": "module example\n",
		"doc.md": "# Title\n\n<!-- generated:table -->\nold line\n<!-- end generated:table -->\n\nMore.\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(dir, "pkg")) // tests run in their package's folder

	r := &recorder{TB: t}
	CheckGenerated(r, "doc.md", "table", "new line\n")
	if len(r.errs) != 1 || !strings.Contains(r.errs[0], "run make docs") ||
		!strings.Contains(r.errs[0], "in the file: old line\n  from code:   new line") {
		t.Fatalf("a stale block: errors %q", r.errs)
	}

	t.Setenv(UpdateDocsEnv, "1")
	r = &recorder{TB: t}
	CheckGenerated(r, "doc.md", "table", "new line\n")
	data, err := os.ReadFile(filepath.Join(dir, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Title\n\n<!-- generated:table -->\nnew line\n<!-- end generated:table -->\n\nMore.\n"; len(r.errs) > 0 ||
		string(data) != want {
		t.Fatalf("after the update: errors %q, file %q", r.errs, data)
	}

	t.Setenv(UpdateDocsEnv, "")
	r = &recorder{TB: t}
	CheckGenerated(r, "doc.md", "table", "new line\n")
	if len(r.errs) > 0 {
		t.Errorf("a current block: errors %q", r.errs)
	}
}
