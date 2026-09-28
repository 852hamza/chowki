// Command licenses copies the licenses of the modules that chowki is built
// from, and of Go itself, into a folder that releases ship, as those
// licenses ask. It fails when a module has no license file, or has one
// that the project doesn't allow in its binary, such as the GPL.
//
//	go run ./tools/licenses --out build/licenses
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

func main() {
	out := flag.String("out", "build/licenses", "the folder to write, replacing what it holds")
	pkg := flag.String("pkg", "./cmd/chowki", "the package whose modules to list")
	flag.Parse()
	if err := run(context.Background(), *pkg, *out); err != nil {
		fmt.Fprintln(os.Stderr, "licenses:", err)
		os.Exit(1)
	}
}

// module is a module that the binary is built from.
type module struct {
	path, version, dir string
}

// platforms are those of the releases, whose dependencies can differ.
var platforms = []string{"linux", "darwin", "windows"}

func run(ctx context.Context, pkg, out string) error {
	mods := map[string]module{}
	for _, goos := range platforms {
		cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-f",
			"{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}", pkg)
		cmd.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0")
		data, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("list the modules for %s: %w", goos, err)
		}
		for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
			if f := strings.Split(line, "\t"); len(f) == 3 {
				mods[f[0]] = module{f[0], f[1], f[2]}
			}
		}
	}
	goroot, err := exec.CommandContext(ctx, "go", "env", "GOROOT").Output()
	if err != nil {
		return fmt.Errorf("find Go: %w", err)
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	var index strings.Builder
	index.WriteString("Chowki is built with Go and these modules. Each folder here holds the license files of the\n" +
		"module of the same name.\n\n")
	all := append([]module{{"go", "the standard library and runtime", strings.TrimSpace(string(goroot))}},
		sortedModules(mods)...)
	var errs []error
	for _, m := range all {
		kind, err := copyLicenses(m, filepath.Join(out, filepath.FromSlash(m.path)))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.path, err))
			continue
		}
		fmt.Fprintf(&index, "%s %s: %s\n", m.path, m.version, kind)
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "README.txt"), []byte(index.String()), 0o644) //nolint:gosec // G306: public
}

func sortedModules(mods map[string]module) []module {
	out := make([]module, 0, len(mods))
	for _, m := range mods {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b module) int { return strings.Compare(a.path, b.path) })
	return out
}

// noticeFile matches the files that a license asks to ship with the code.
var noticeFile = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice|patents|authors)`)

// licenseFile matches the files that hold a license itself.
var licenseFile = regexp.MustCompile(`(?i)^(licen[cs]e|copying)`)

// copyLicenses copies a module's license and notice files to dir, and
// returns the kind of its license.
func copyLicenses(m module, dir string) (string, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return "", err
	}
	kinds := map[string]bool{}
	copied := 0
	for _, e := range entries {
		if e.IsDir() || !noticeFile.MatchString(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, e.Name()))
		if err != nil {
			return "", err
		}
		if licenseFile.MatchString(e.Name()) {
			found, err := classify(data)
			if err != nil {
				return "", fmt.Errorf("%s: %w", e.Name(), err)
			}
			for _, k := range found {
				kinds[k] = true
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: public
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o644); err != nil { //nolint:gosec // G306: public
			return "", err
		}
		copied++
	}
	if len(kinds) == 0 {
		return "", errors.New("no license file that names a license the project allows")
	}
	names := slices.Sorted(func(yield func(string) bool) {
		for k := range kinds {
			if !yield(k) {
				return
			}
		}
	})
	return strings.Join(names, ", "), nil
}

// allowed are the licenses that the binary may include, by a phrase of
// their text, and forbidden those that it may not.
var (
	allowed = []struct{ kind, phrase string }{
		{"MIT", "Permission is hereby granted, free of charge"},
		{"BSD", "Redistribution and use in source and binary forms"},
		{"Apache-2.0", "Apache License"},
		{"MPL-2.0", "Mozilla Public License"},
		{"ISC", "Permission to use, copy, modify, and/or distribute this software"},
		{"public domain", "dedicated to the public"},
	}
	forbidden = []string{"GNU GENERAL PUBLIC LICENSE", "GNU AFFERO GENERAL PUBLIC LICENSE",
		"GNU LESSER GENERAL PUBLIC LICENSE", "Server Side Public License", "Business Source License"}
)

// classify returns the kinds of license that a text grants, none for a
// file that names no license, such as a list of third-party notices, or an
// error for a license that the binary may not include.
func classify(text []byte) ([]string, error) {
	upper := bytes.ToUpper(text)
	for _, f := range forbidden {
		if bytes.Contains(upper, bytes.ToUpper([]byte(f))) {
			return nil, fmt.Errorf("the %s isn't allowed in the binary", f)
		}
	}
	var kinds []string
	for _, a := range allowed {
		if bytes.Contains(upper, bytes.ToUpper([]byte(a.phrase))) {
			kinds = append(kinds, a.kind)
		}
	}
	return kinds, nil
}
