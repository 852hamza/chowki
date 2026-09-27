package main

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/buildinfo"
)

var (
	acme = buildinfo.Identity{Domain: "widget.example", Owner: "acme", Repo: "widget"}
	zeta = buildinfo.Identity{Domain: "gadget.example", Owner: "zeta", Repo: "widget"}
)

const readmeBefore = `Code: https://github.com/acme/widget
Clone: git clone https://github.com/acme/widget.git
Docs: https://widget.example/docs/start
Site: https://www.widget.example
Image: ghcr.io/acme/widget:latest
Mail: security@widget.example
Install: go install github.com/acme/widget/cmd/widget@latest
Untouched: acme, widget, github.com/acme/widget2, mywidget.example, widget.examples, widget.example.net
`

const readmeAfter = `Code: https://github.com/zeta/widget
Clone: git clone https://github.com/zeta/widget.git
Docs: https://gadget.example/docs/start
Site: https://www.gadget.example
Image: ghcr.io/zeta/widget:latest
Mail: security@gadget.example
Install: go install github.com/zeta/widget/cmd/widget@latest
Untouched: acme, widget, github.com/acme/widget2, mywidget.example, widget.examples, widget.example.net
`

// fixture is a small repository that uses the acme identity.
var fixture = map[string]string{
	".gitignore": "ignored.txt\n.internal/\n",
	"go.mod":     "module github.com/acme/widget\n\ngo 1.27\n",
	"main.go": "package main\n\nimport (\n\t\"github.com/acme/widget/internal/x\"\n\t\"github.com/beta/lib\"\n)\n\n" +
		"var _ = x.X\nvar _ = lib.L\n",
	"README.md":             readmeBefore,
	"ignored.txt":           "https://github.com/acme/widget\n",
	"vendor/v.txt":          "https://github.com/acme/widget\n",
	"logo.bin":              "\x00https://github.com/acme/widget\n",
	".internal/plan.md":     "See https://widget.example/docs.\n",
	".internal/.git/config": "url = https://github.com/acme/widget\n",
}

func envContent(id buildinfo.Identity) string {
	return fmt.Sprintf("# Project identity.\nPROJECT_DOMAIN=%s\nGITHUB_OWNER=%s\nGITHUB_REPO=%s\nWEBSITE_URL=%s\nDOCS_URL=%s\n",
		id.Domain, id.Owner, id.Repo, id.Website, id.Docs)
}

// newRepo creates a git work tree with files and project.env set to id, and
// runs a first sync, which only records the lock and the buildinfo defaults.
func newRepo(t *testing.T, id buildinfo.Identity, files map[string]string) string {
	t.Helper()
	requireTool(t, "git")
	root := t.TempDir()
	files = maps.Clone(files)
	files[envFile] = envContent(id)
	for rel, content := range files {
		writeFile(t, root, rel, content)
	}
	cmd := exec.CommandContext(t.Context(), "git", "init", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := syncRepo(t.Context(), root, io.Discard); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	return root
}

func TestFirstSyncOnlyRecordsIdentity(t *testing.T) {
	root := newRepo(t, acme, fixture)
	for rel, want := range fixture {
		if got := readFile(t, root, rel); got != want {
			t.Errorf("%s changed:\n%s", rel, got)
		}
	}
	lock, err := readLock(root)
	if err != nil {
		t.Fatalf("readLock() error = %v", err)
	}
	if lock.id != acme || len(lock.retired) != 0 {
		t.Errorf("lock = %+v, want the acme identity and nothing retired", lock)
	}
	if err := checkRepo(t.Context(), root, io.Discard); err != nil {
		t.Errorf("checkRepo() error = %v", err)
	}
}

func TestSyncRename(t *testing.T) {
	requireTool(t, "go")
	root := newRepo(t, acme, fixture)
	writeFile(t, root, envFile, envContent(zeta))
	var out bytes.Buffer
	if err := syncRepo(t.Context(), root, &out); err != nil {
		t.Fatalf("syncRepo() error = %v", err)
	}

	want := maps.Clone(fixture)
	want["go.mod"] = "module github.com/zeta/widget\n\ngo 1.27\n"
	want["main.go"] = "package main\n\nimport (\n\t\"github.com/beta/lib\"\n\t\"github.com/zeta/widget/internal/x\"\n)\n\n" +
		"var _ = x.X\nvar _ = lib.L\n" // gofmt sorted the imports
	want["README.md"] = readmeAfter
	want[".internal/plan.md"] = "See https://gadget.example/docs.\n"
	for rel, w := range want {
		if got := readFile(t, root, rel); got != w {
			t.Errorf("%s =\n%s\nwant\n%s", rel, got, w)
		}
	}
	for _, rel := range []string{"go.mod", "main.go", "README.md", ".internal/plan.md", defaultsFile, lockFile} {
		if !strings.Contains(out.String(), "updated "+rel+"\n") {
			t.Errorf("output doesn't list %s:\n%s", rel, out.String())
		}
	}

	lock, err := readLock(root)
	if err != nil {
		t.Fatalf("readLock() error = %v", err)
	}
	wantRetired := []string{
		"ghcr.io/acme/widget", "github.com/acme/widget", "https://github.com/acme/widget",
		"https://widget.example", "https://widget.example/docs", "widget.example",
	}
	if lock.id != zeta || !reflect.DeepEqual(lock.retired, wantRetired) {
		t.Errorf("lock = %+v, want zeta with retired %q", lock, wantRetired)
	}
	defaults := readFile(t, root, defaultsFile)
	for _, v := range []string{zeta.Domain, zeta.Owner, zeta.Repo} {
		if !strings.Contains(defaults, strconv.Quote(v)) {
			t.Errorf("%s doesn't contain %q:\n%s", defaultsFile, v, defaults)
		}
	}
	if err := checkRepo(t.Context(), root, io.Discard); err != nil {
		t.Errorf("checkRepo() after sync error = %v", err)
	}
}

func TestSyncRenameBack(t *testing.T) {
	requireTool(t, "go")
	root := newRepo(t, acme, fixture)
	for _, id := range []buildinfo.Identity{zeta, acme} {
		writeFile(t, root, envFile, envContent(id))
		if err := syncRepo(t.Context(), root, io.Discard); err != nil {
			t.Fatalf("syncRepo() to %s error = %v", id.Owner, err)
		}
	}
	if got := readFile(t, root, "README.md"); got != readmeBefore {
		t.Errorf("README.md after renaming back =\n%s", got)
	}
	lock, err := readLock(root)
	if err != nil {
		t.Fatalf("readLock() error = %v", err)
	}
	for _, v := range lock.retired {
		if strings.Contains(v, "acme") || strings.Contains(v, "widget.example") {
			t.Errorf("current value %q is retired", v)
		}
	}
	if err := checkRepo(t.Context(), root, io.Discard); err != nil {
		t.Errorf("checkRepo() error = %v", err)
	}
}

func TestSyncUnchanged(t *testing.T) {
	root := newRepo(t, acme, fixture)
	var out bytes.Buffer
	if err := syncRepo(t.Context(), root, &out); err != nil {
		t.Fatalf("syncRepo() error = %v", err)
	}
	if strings.Contains(out.String(), "updated") {
		t.Errorf("sync without changes updated files:\n%s", out.String())
	}
}

func TestCheckFindsProblems(t *testing.T) {
	requireTool(t, "go")
	tests := []struct {
		name  string
		edit  func(t *testing.T, root string)
		wants []string // substrings of the report; none means the check passes
	}{
		{
			name: "retired value in a new file",
			edit: func(t *testing.T, root string) {
				writeFile(t, root, "docs/new.md", "Title\nReport bugs at https://github.com/acme/widget/issues\n")
			},
			wants: []string{`docs/new.md:2: contains "https://github.com/acme/widget"`},
		},
		{
			name: "retired value in .internal",
			edit: func(t *testing.T, root string) {
				writeFile(t, root, ".internal/notes.md", "old site: widget.example\n")
			},
			wants: []string{`.internal/notes.md:1: contains "widget.example"`},
		},
		{
			name: "hard-coded repo URL in Go",
			edit: func(t *testing.T, root string) {
				writeFile(t, root, "cmd/x.go", "package main\n\nconst u = \"https://github.com/zeta/widget\"\n")
			},
			wants: []string{`cmd/x.go:3: hard-codes "https://github.com/zeta/widget"`},
		},
		{
			name: "hard-coded domain in Go",
			edit: func(t *testing.T, root string) {
				writeFile(t, root, "x.go", "package main\n\n// See gadget.example.\n")
			},
			wants: []string{`x.go:3: hard-codes "gadget.example"`},
		},
		{
			name: "values allowed in buildinfo",
			edit: func(t *testing.T, root string) {
				writeFile(t, root, "internal/buildinfo/x.go", "package buildinfo\n\n// See gadget.example.\n")
			},
		},
		{
			name: "project.env changed without sync",
			edit: func(t *testing.T, root string) {
				writeFile(t, root, envFile, envContent(buildinfo.Identity{Domain: "other.example", Owner: "zeta", Repo: "widget"}))
			},
			wants: []string{"project.env changed since the last sync", defaultsFile + " is out of date"},
		},
		{
			name: "defaults edited by hand",
			edit: func(t *testing.T, root string) {
				writeFile(t, root, defaultsFile, "package buildinfo\n")
			},
			wants: []string{defaultsFile + " is out of date"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newRepo(t, acme, fixture)
			writeFile(t, root, envFile, envContent(zeta))
			if err := syncRepo(t.Context(), root, io.Discard); err != nil {
				t.Fatalf("syncRepo() error = %v", err)
			}
			tt.edit(t, root)

			var out bytes.Buffer
			err := checkRepo(t.Context(), root, &out)
			if len(tt.wants) == 0 {
				if err != nil {
					t.Fatalf("checkRepo() error = %v\n%s", err, out.String())
				}
				return
			}
			if err == nil {
				t.Fatalf("checkRepo() passed, want problems %q", tt.wants)
			}
			for _, w := range tt.wants {
				if !strings.Contains(out.String(), w) {
					t.Errorf("report doesn't contain %q:\n%s", w, out.String())
				}
			}
		})
	}
}

func TestCheckWithoutLock(t *testing.T) {
	root := newRepo(t, acme, fixture)
	if err := os.Remove(filepath.Join(root, lockFile)); err != nil {
		t.Fatal(err)
	}
	if err := checkRepo(t.Context(), root, io.Discard); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("checkRepo() error = %v, want missing lock", err)
	}
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not in PATH", name)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
