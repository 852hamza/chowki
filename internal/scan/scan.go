package scan

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/852hamza/chowki/internal/redact"
)

// Finding is a secret, or personal data, in a file. It never holds the
// value.
type Finding struct {
	// Path is relative to the scanned directory, with slashes; for a file
	// given by name, it's that name.
	Path string
	// Line and Column are where the value starts, from 1; Column counts
	// characters. EndLine and EndColumn are just past its end.
	Line, Column, EndLine, EndColumn int
	Rule                             *Rule
	// Occurrence counts the findings of the rule before this one in the
	// file, from 0: a fingerprint that doesn't move with the lines.
	Occurrence int
}

// Result is what a scan found.
type Result struct {
	Findings []Finding
	// Files counts the files scanned.
	Files int
}

// Options change what a scan reads and reports.
type Options struct {
	// PII reports personal data, such as email addresses, too.
	PII bool
	// MaxFileSize skips larger files; 0 means DefaultMaxFileSize.
	MaxFileSize int64
}

// DefaultMaxFileSize is the size above which files are skipped: such files
// are data or builds rather than configuration or code.
const DefaultMaxFileSize = 5 << 20

// skipped are directories that hold other people's code or tools' output.
var skipped = map[string]bool{".git": true, "node_modules": true, ".venv": true, "venv": true,
	"__pycache__": true, ".terraform": true}

// Scan finds secrets in path: a file, or a directory. In a git work tree it
// reads the files that git tracks or would track, which leaves out ignored
// files such as local .env files; elsewhere it reads every file, but not
// the directories in skipped. A file named explicitly is always read.
func Scan(ctx context.Context, path string, opts Options) (*Result, error) {
	if opts.MaxFileSize == 0 {
		opts.MaxFileSize = DefaultMaxFileSize
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	res := &Result{}
	if !info.IsDir() {
		return res, res.file(path, filepath.ToSlash(path), opts)
	}
	files, err := gitFiles(ctx, path)
	if err != nil {
		if files, err = walk(path); err != nil {
			return nil, err
		}
	}
	for _, rel := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := res.file(filepath.Join(path, filepath.FromSlash(rel)), rel, opts); err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(res.Findings, func(a, b Finding) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
	})
	return res, nil
}

// gitFiles lists the files of a git work tree, relative to dir, when dir is
// in one and git is installed.
func gitFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "ls-files", "-z", "--cached", "--others",
		"--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("list git files: %w", err)
	}
	var files []string
	for f := range strings.SplitSeq(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

func walk(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && p != dir && skipped[d.Name()]:
			return filepath.SkipDir
		case d.Type().IsRegular():
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files, err
}

// file scans one file, unless it's too large, binary or gone.
func (res *Result) file(path, rel string, opts Options) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil // git lists deleted files that aren't committed yet
	case err != nil:
		return err
	case !info.Mode().IsRegular() || info.Size() > opts.MaxFileSize:
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return nil // binary
	}
	res.Files++
	counts := map[string]int{}
	for _, span := range find(rel, data, opts.PII) {
		f := Finding{Path: rel, Rule: span.rule, Occurrence: counts[span.rule.ID]}
		counts[span.rule.ID]++
		f.Line, f.Column = position(data, span.start)
		f.EndLine, f.EndColumn = position(data, span.end)
		res.Findings = append(res.Findings, f)
	}
	return nil
}

// span is a finding in a file's bytes.
type span struct {
	start, end int
	rule       *Rule
}

// find runs the redaction detectors on a file, and the rules for .env files
// and MCP configurations on files of those kinds.
func find(name string, data []byte, pii bool) []span {
	text := string(data)
	var out []span
	for _, f := range redact.Find(text) {
		if r := rules[f.Type]; r != nil && (!r.PII || pii) && (r.PII || !placeholder(f.Type, text[f.Start:f.End])) {
			out = append(out, span{f.Start, f.End, r})
		}
	}
	var extra []span
	switch {
	case isEnvFile(name):
		extra = envSecrets(text)
	case strings.HasSuffix(name, ".json"):
		extra = mcpSecrets(data)
	}
	// A value that the detectors found already isn't reported twice.
	for _, e := range extra {
		if !slices.ContainsFunc(out, func(s span) bool { return s.start < e.end && e.start < s.end }) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b span) int { return cmp.Compare(a.start, b.start) })
	return out
}

// keyBody matches the base64 body of a PEM block.
var keyBody = regexp.MustCompile(`[A-Za-z0-9+/=]{64}`)

// placeholder reports whether a secret that the detectors found stands for
// one in documentation: it says example, as AWS's documented example key
// does, or it's the header of a private key without the key.
func placeholder(typ, value string) bool {
	if strings.Contains(strings.ToLower(value), "example") {
		return true
	}
	return typ == redact.TypePrivateKey && !keyBody.MatchString(value)
}

// position returns the line and the column, both from 1, of a byte offset.
func position(data []byte, offset int) (line, column int) {
	before := data[:offset]
	line = bytes.Count(before, []byte("\n")) + 1
	start := bytes.LastIndexByte(before, '\n') + 1
	return line, utf8.RuneCount(before[start:]) + 1
}
