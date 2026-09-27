package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// childEnv marks the processes that TestRenameEndToEnd starts, so the test
// suite running inside the renamed copy doesn't start yet another copy.
const childEnv = "CHOWKI_SYNC_E2E_CHILD"

// TestRenameEndToEnd changes GITHUB_OWNER and PROJECT_DOMAIN in a copy of this
// repository, runs `make sync`, and checks that go.mod, imports, docs and the
// buildinfo defaults follow, that `make build test sync-check` still pass,
// and that the binary reports the new repository URL.
func TestRenameEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: builds and tests a renamed copy of the repository")
	}
	if os.Getenv(childEnv) != "" {
		t.Skip("already inside a renamed copy")
	}
	for _, tool := range []string{"git", "make", "go"} {
		requireTool(t, tool)
	}

	src, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	old, err := readIdentity(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	files := copyRepo(t, src, dst)

	// The new domain is built at run time: as a literal in this file, it
	// would be a hard-coded domain in the renamed copy. It also mustn't
	// contain the old domain, for the substring check below.
	renamed := old
	renamed.Owner = "renamed-owner"
	renamed.Domain = "renamed-" + strings.ReplaceAll(old.Domain, ".", "-") + ".example"
	// Like a person renaming the project, also update the URL overrides that
	// name the old owner or domain; make sync refuses stale ones.
	m, err := replacements(old, renamed)
	if err != nil {
		t.Fatal(err)
	}
	renamed.Website, _ = m.replace(old.Website)
	renamed.Docs, _ = m.replace(old.Docs)
	env := readFile(t, dst, envFile)
	for _, kv := range [][3]string{
		{"GITHUB_OWNER", old.Owner, renamed.Owner},
		{"PROJECT_DOMAIN", old.Domain, renamed.Domain},
		{"WEBSITE_URL", old.Website, renamed.Website},
		{"DOCS_URL", old.Docs, renamed.Docs},
	} {
		env = strings.Replace(env, "\n"+kv[0]+"="+kv[1]+"\n", "\n"+kv[0]+"="+kv[2]+"\n", 1)
	}
	writeFile(t, dst, envFile, env)
	if id, err := readIdentity(dst); err != nil || id != renamed {
		t.Fatalf("edited %s = %+v, %v; want %+v", envFile, id, err, renamed)
	}

	runIn(t, dst, "make", "sync")

	if got := readFile(t, dst, "go.mod"); !strings.HasPrefix(got, "module "+renamed.Module()+"\n") {
		t.Errorf("go.mod starts with %q, want module %s", strings.SplitN(got, "\n", 2)[0], renamed.Module())
	}
	contains := map[string][]string{
		"cmd/chowki/main.go":                     {strconv.Quote(renamed.Module() + "/internal/buildinfo")},
		"README.md":                              {renamed.RepoURL()},
		"SECURITY.md":                            {renamed.RepoURL() + "/security/advisories/new"},
		"docs/contributing/development-setup.md": {renamed.RepoURL()},
		defaultsFile:                             {strconv.Quote(renamed.Owner), strconv.Quote(renamed.Domain)},
	}
	for rel, wants := range contains {
		got := readFile(t, dst, rel)
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s doesn't contain %s after make sync", rel, w)
			}
		}
	}
	// Independent of the tool's own check: nothing may still name the old
	// module path or domain.
	for _, rel := range files {
		if rel == envFile || rel == lockFile {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range []string{old.Module(), old.Domain} {
			if strings.Contains(string(data), v) {
				t.Errorf("%s still contains %q after make sync", rel, v)
			}
		}
	}

	runIn(t, dst, "make", "build", "test", "sync-check", "VERSION=v0.0.0-e2e")
	out := runIn(t, dst, filepath.Join(dst, "bin", "chowki"), "version")
	for _, w := range []string{"chowki v0.0.0-e2e\n", renamed.RepoURL() + "\n"} {
		if !strings.Contains(out, w) {
			t.Errorf("chowki version output doesn't contain %q:\n%s", w, out)
		}
	}
}

// copyRepo copies the files that git tracks or would add from src to dst,
// makes dst a git work tree with one commit, and returns the copied files.
// The gitignored .internal/ folder stays behind.
func copyRepo(t *testing.T, src, dst string) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = src
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		from := filepath.Join(src, filepath.FromSlash(rel))
		info, err := os.Lstat(from)
		if os.IsNotExist(err) || err == nil && !info.Mode().IsRegular() {
			continue // deleted from the work tree, or a symlink
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		to := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, data, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		files = append(files, rel)
	}

	// A commit gives `go build` VCS information to stamp. The -c options keep
	// the user's hooks and signing setup out of the copy.
	git := []string{"-c", "user.name=chowki-e2e", "-c", "user.email=e2e@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}
	runIn(t, dst, "git", "init", "-q")
	runIn(t, dst, "git", "add", "-A")
	runIn(t, dst, "git", append(git, "commit", "-q", "-m", "copy")...)
	return files
}

// runIn runs a command in dir, marked as a child of TestRenameEndToEnd, and
// returns its combined output. It fails the test if the command fails.
func runIn(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	// Drop make's variables from a parent `make test`: they refer to a job
	// server that the child can't reach.
	for _, kv := range os.Environ() {
		switch k, _, _ := strings.Cut(kv, "="); k {
		case "MAKEFLAGS", "MFLAGS", "MAKELEVEL":
		default:
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, childEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}
