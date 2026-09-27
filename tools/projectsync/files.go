package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// maxFileSize is the largest file that sync reads. Bigger files are data.
const maxFileSize = 2 << 20

// listFiles returns the files that sync manages, relative to root with
// forward slashes: the files git tracks or would add (untracked but not
// ignored), plus everything under the gitignored .internal/ folder. It skips
// .git, bin, dist and vendor folders, project.env, the lock file and the
// generated buildinfo defaults.
func listFiles(ctx context.Context, root string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list files with git (sync needs a git work tree): %w", err)
	}
	var files []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel != "" && !skipped(rel) {
			files = append(files, rel)
		}
	}

	err = filepath.WalkDir(filepath.Join(root, ".internal"), func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && d.Name() == ".git":
			return filepath.SkipDir
		case !d.Type().IsRegular():
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel = filepath.ToSlash(rel); !skipped(rel) {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("list .internal: %w", err)
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

func skipped(rel string) bool {
	switch rel {
	case envFile, lockFile, defaultsFile:
		return true
	}
	for part := range strings.SplitSeq(rel, "/") {
		switch part {
		case ".git", "bin", "dist", "vendor":
			return true
		}
	}
	return false
}

// readText returns the content and permissions of a file, or ok == false for
// a file that sync leaves alone: gone, not regular, over maxFileSize or
// binary. Like git, it treats a file with a NUL byte near the start as binary.
func readText(root, rel string) (data []byte, perm fs.FileMode, ok bool, err error) {
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, false, nil // deleted but still in the git index
	}
	if err != nil {
		return nil, 0, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return nil, 0, false, nil
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, 0, false, err
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return nil, 0, false, nil
	}
	return data, info.Mode().Perm(), true, nil
}

// writeIfChanged writes data to path unless the file already holds it, and
// reports whether it wrote.
func writeIfChanged(path string, data []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	return true, writeFileAtomic(path, data, 0o644)
}

// writeFileAtomic replaces path through a rename, so an interrupted sync never
// leaves a half-written file.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".projectsync-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }() // fails harmlessly after the rename
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(perm)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
