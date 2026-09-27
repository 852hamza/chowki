package main

import (
	"bytes"
	"fmt"
	"go/format"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/852hamza/chowki/internal/buildinfo"
)

const (
	envFile      = "project.env"
	lockFile     = ".project.lock"
	defaultsFile = "internal/buildinfo/project.go"
)

// lockState is the content of .project.lock: the identity that sync applied
// last, and the values of earlier identities that must not reappear.
type lockState struct {
	id      buildinfo.Identity
	retired []string
}

var (
	keyRE    = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	domainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	ownerRE  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	repoRE   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

func readIdentity(root string) (buildinfo.Identity, error) {
	data, err := os.ReadFile(filepath.Join(root, envFile))
	if err != nil {
		return buildinfo.Identity{}, err
	}
	st, err := parseEnv(envFile, data, false)
	return st.id, err
}

func readLock(root string) (lockState, error) {
	data, err := os.ReadFile(filepath.Join(root, lockFile))
	if err != nil {
		return lockState{}, err
	}
	return parseEnv(lockFile, data, true)
}

// parseEnv parses the format of project.env and .project.lock: one KEY=VALUE
// per line, with blank lines and "#" comments. The Makefile includes
// project.env, so a value must be a plain word: no whitespace, quotes, "$",
// "#" or backslashes. Only the lock may contain RETIRED lines.
func parseEnv(name string, data []byte, isLock bool) (lockState, error) {
	var st lockState
	fields := map[string]*string{
		"PROJECT_DOMAIN": &st.id.Domain,
		"GITHUB_OWNER":   &st.id.Owner,
		"GITHUB_REPO":    &st.id.Repo,
		"WEBSITE_URL":    &st.id.Website,
		"DOCS_URL":       &st.id.Docs,
	}
	seen := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !keyRE.MatchString(key) {
			return st, fmt.Errorf("%s:%d: want KEY=VALUE without spaces", name, i+1)
		}
		if strings.IndexFunc(value, notPlain) >= 0 {
			return st, fmt.Errorf("%s:%d: %s must be a plain value without spaces, quotes, $, # or backslashes (use LF line endings)",
				name, i+1, key)
		}
		if isLock && key == "RETIRED" {
			if value == "" {
				return st, fmt.Errorf("%s:%d: RETIRED needs a value", name, i+1)
			}
			st.retired = append(st.retired, value)
			continue
		}
		field, ok := fields[key]
		if !ok {
			return st, fmt.Errorf("%s:%d: unknown key %s", name, i+1, key)
		}
		if seen[key] {
			return st, fmt.Errorf("%s:%d: %s is set twice", name, i+1, key)
		}
		seen[key] = true
		*field = value
	}
	if err := validate(st.id); err != nil {
		return st, fmt.Errorf("%s: %w", name, err)
	}
	return st, nil
}

func notPlain(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("\"'`$#\\", r) }

func validate(id buildinfo.Identity) error {
	switch {
	case !domainRE.MatchString(id.Domain):
		return fmt.Errorf("PROJECT_DOMAIN %q is not a lowercase domain name such as example.com", id.Domain)
	case !ownerRE.MatchString(id.Owner):
		return fmt.Errorf("GITHUB_OWNER %q is not a valid GitHub user or organization name", id.Owner)
	case !repoRE.MatchString(id.Repo) || id.Repo == "." || id.Repo == "..":
		return fmt.Errorf("GITHUB_REPO %q is not a valid GitHub repository name", id.Repo)
	}
	for _, kv := range [][2]string{{"WEBSITE_URL", id.Website}, {"DOCS_URL", id.Docs}} {
		if kv[1] == "" {
			continue
		}
		u, err := url.Parse(kv[1])
		if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%s %q must be empty or an https URL without query or fragment", kv[0], kv[1])
		}
	}
	return nil
}

// identityValues returns the strings by which files refer to id.
func identityValues(id buildinfo.Identity) []string {
	return []string{id.Domain, id.Module(), id.RepoURL(), id.Image(), id.WebsiteURL(), id.DocsURL()}
}

// hardcodedValues returns the values that Go code outside internal/buildinfo
// must not contain. The module path is allowed because imports need it.
func hardcodedValues(id buildinfo.Identity) []string {
	return []string{id.Domain, id.RepoURL(), id.Image(), id.WebsiteURL(), id.DocsURL()}
}

// replacements returns a matcher that rewrites the values of old to those of
// cur.
func replacements(old, cur buildinfo.Identity) (*matcher, error) {
	olds, news := identityValues(old), identityValues(cur)
	to := map[string]string{}
	var from, into []string
	for i, o := range olds {
		if o == news[i] {
			continue
		}
		if prev, ok := to[o]; ok {
			if prev != news[i] {
				return nil, fmt.Errorf("cannot rename %q: it would become both %q and %q", o, prev, news[i])
			}
			continue
		}
		to[o] = news[i]
		from, into = append(from, o), append(into, news[i])
	}
	return newMatcher(from, into), nil
}

func formatLock(st lockState) []byte {
	var b bytes.Buffer
	b.WriteString("# Written by `make sync`: the project identity it applied last.\n")
	b.WriteString("# Don't edit this file. Change project.env and run `make sync` instead.\n")
	fmt.Fprintf(&b, "PROJECT_DOMAIN=%s\nGITHUB_OWNER=%s\nGITHUB_REPO=%s\nWEBSITE_URL=%s\nDOCS_URL=%s\n",
		st.id.Domain, st.id.Owner, st.id.Repo, st.id.Website, st.id.Docs)
	if len(st.retired) > 0 {
		b.WriteString("# Values of earlier identities. `make sync-check` fails if one appears again.\n")
		for _, v := range st.retired {
			fmt.Fprintf(&b, "RETIRED=%s\n", v)
		}
	}
	return b.Bytes()
}

// writeLock writes the lock file and reports whether its content changed.
func writeLock(root string, st lockState) (bool, error) {
	return writeIfChanged(filepath.Join(root, lockFile), formatLock(st))
}

// defaultsTemplate is the generated part of internal/buildinfo. Project lives
// here too, so that the rest of the package, which this tool imports, compiles
// even when the generated file is missing or stale.
const defaultsTemplate = `// Code generated by tools/projectsync from project.env. DO NOT EDIT.

package buildinfo

// Identity defaults, equal to project.env. The Makefile overrides them at
// link time with -ldflags -X.
var (
	domain = %q
	owner = %q
	repo = %q
	website = %q
	docs = %q
)

// Project returns the identity this binary was built with.
func Project() Identity {
	return Identity{Domain: domain, Owner: owner, Repo: repo, Website: website, Docs: docs}
}
`

// defaultsSource returns the Go source of the buildinfo defaults for id.
func defaultsSource(id buildinfo.Identity) ([]byte, error) {
	src := fmt.Sprintf(defaultsTemplate, id.Domain, id.Owner, id.Repo, id.Website, id.Docs)
	out, err := format.Source([]byte(src))
	if err != nil {
		return nil, fmt.Errorf("generate %s: %w", defaultsFile, err)
	}
	return out, nil
}

// writeDefaults regenerates the buildinfo defaults and reports whether they
// changed.
func writeDefaults(root string, id buildinfo.Identity) (bool, error) {
	src, err := defaultsSource(id)
	if err != nil {
		return false, err
	}
	path := filepath.Join(root, filepath.FromSlash(defaultsFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return writeIfChanged(path, src)
}
