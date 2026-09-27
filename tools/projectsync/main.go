// Command projectsync applies the project identity in project.env (domain,
// GitHub owner and repository) to the whole repository.
//
// `make sync` runs it after project.env changes: it rewrites go.mod, Go
// imports, docs and the buildinfo defaults, and records the applied values in
// .project.lock. `make sync-check` runs it with --check, which changes nothing
// and fails when the repository is out of sync.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"github.com/852hamza/chowki/internal/buildinfo"
)

func main() {
	checkOnly := flag.Bool("check", false, "report problems and change nothing")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	run := syncRepo
	if *checkOnly {
		run = checkRepo
	}
	err := run(ctx, ".", os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "projectsync:", err)
		os.Exit(1)
	}
}

// syncRepo applies project.env to the repository in root.
func syncRepo(ctx context.Context, root string, out io.Writer) error {
	cur, err := readIdentity(root)
	if err != nil {
		return err
	}
	lock, err := readLock(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// No record of earlier values, so there is nothing to replace yet.
		lock = lockState{id: cur}
	case err != nil:
		return err
	}

	var updated []string
	if lock.id != cur {
		m, err := replacements(lock.id, cur)
		if err != nil {
			return err
		}
		// project.env is never rewritten, so an override that still names the
		// old owner or domain would keep an old value alive unnoticed.
		for _, kv := range [][2]string{{"WEBSITE_URL", cur.Website}, {"DOCS_URL", cur.Docs}} {
			if pos, i := m.find(kv[1], 0); pos >= 0 {
				return fmt.Errorf("%s: %s still contains %q, which this sync replaces; update it first",
					envFile, kv[0], m.olds[i])
			}
		}
		if updated, err = rewriteFiles(ctx, root, m); err != nil {
			return err
		}
		if lock.id.Module() != cur.Module() {
			if err := goModEdit(ctx, root, cur.Module()); err != nil {
				return err
			}
		}
		lock = lockState{id: cur, retired: retire(lock, cur)}
	}

	changed, err := writeDefaults(root, cur)
	if err != nil {
		return err
	}
	if changed {
		updated = append(updated, defaultsFile)
	}
	if changed, err = writeLock(root, lock); err != nil {
		return err
	}
	if changed {
		updated = append(updated, lockFile)
	}

	for _, f := range updated {
		fmt.Fprintln(out, "updated", f)
	}
	fmt.Fprintf(out, "project identity is in sync with %s\n", envFile)
	return nil
}

// checkRepo reports, without changing anything, whether the repository in
// root matches project.env.
func checkRepo(ctx context.Context, root string, out io.Writer) error {
	cur, err := readIdentity(root)
	if err != nil {
		return err
	}
	lock, err := readLock(root)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s is missing; run make sync", lockFile)
	} else if err != nil {
		return err
	}

	var problems []string
	if lock.id != cur {
		problems = append(problems, envFile+" changed since the last sync; run make sync")
	}
	want, err := defaultsSource(cur)
	if err != nil {
		return err
	}
	have, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(defaultsFile)))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !bytes.Equal(have, want) {
		problems = append(problems, defaultsFile+" is out of date; run make sync")
	}

	files, err := listFiles(ctx, root)
	if err != nil {
		return err
	}
	retired := newMatcher(lock.retired, nil)
	hardcoded := newMatcher(hardcodedValues(cur), nil)
	for _, rel := range files {
		data, _, ok, err := readText(root, rel)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		text := string(data)
		if pos, i := retired.find(text, 0); pos >= 0 {
			problems = append(problems, fmt.Sprintf("%s:%d: contains %q from an earlier project identity; replace it",
				rel, lineOf(text, pos), retired.olds[i]))
		}
		if strings.HasSuffix(rel, ".go") && !strings.HasPrefix(rel, "internal/buildinfo/") {
			if pos, i := hardcoded.find(text, 0); pos >= 0 {
				problems = append(problems, fmt.Sprintf("%s:%d: hard-codes %q; read it from internal/buildinfo",
					rel, lineOf(text, pos), hardcoded.olds[i]))
			}
		}
	}

	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(out, p)
		}
		return fmt.Errorf("repository is out of sync with %s: %d problem(s)", envFile, len(problems))
	}
	fmt.Fprintf(out, "project identity is in sync with %s\n", envFile)
	return nil
}

// rewriteFiles replaces old identity values in every file that sync manages
// and returns the files it changed. It prepares every change before writing
// any, so a read or format error leaves the repository untouched.
func rewriteFiles(ctx context.Context, root string, m *matcher) ([]string, error) {
	files, err := listFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	type change struct {
		rel  string
		data []byte
		perm fs.FileMode
	}
	var changes []change
	for _, rel := range files {
		data, perm, ok, err := readText(root, rel)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		text, replaced := m.replace(string(data))
		if !replaced {
			continue
		}
		data = []byte(text)
		if strings.HasSuffix(rel, ".go") {
			// A new module path can change the import order. Files that aren't
			// valid Go, such as test fixtures, keep their layout.
			if formatted, err := format.Source(data); err == nil {
				data = formatted
			}
		}
		changes = append(changes, change{rel, data, perm})
	}

	written := make([]string, 0, len(changes))
	for _, c := range changes {
		if err := writeFileAtomic(filepath.Join(root, filepath.FromSlash(c.rel)), c.data, c.perm); err != nil {
			return written, fmt.Errorf("write %s: %w", c.rel, err)
		}
		written = append(written, c.rel)
	}
	return written, nil
}

// retire returns the values that files must no longer contain once cur
// replaces the locked identity. Values of cur are never retired, so renaming
// back to an earlier identity works.
func retire(lock lockState, cur buildinfo.Identity) []string {
	keep := identityValues(cur)
	var retired []string
	for _, v := range slices.Concat(lock.retired, identityValues(lock.id)) {
		if !slices.Contains(keep, v) {
			retired = append(retired, v)
		}
	}
	slices.Sort(retired)
	return slices.Compact(retired)
}

func goModEdit(ctx context.Context, root, module string) error {
	cmd := exec.CommandContext(ctx, "go", "mod", "edit", "-module", module)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod edit: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func lineOf(text string, pos int) int { return strings.Count(text[:pos], "\n") + 1 }
