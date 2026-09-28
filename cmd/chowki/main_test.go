package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/buildinfo"
)

type runCase struct {
	name       string
	args       []string
	wantCode   int
	wantStdout []string
	wantStderr []string
}

func TestRun(t *testing.T) {
	id := buildinfo.Project()
	tests := []runCase{
		{
			name:       "version",
			args:       []string{"version"},
			wantCode:   exitOK,
			wantStdout: []string{"chowki " + buildinfo.Version() + "\n", "repo:   " + id.RepoURL() + "\n"},
		},
		{
			name:       "version with arguments",
			args:       []string{"version", "extra"},
			wantCode:   exitUsage,
			wantStderr: []string{"takes no arguments"},
		},
		{
			name:     "help",
			args:     []string{"help"},
			wantCode: exitOK,
			wantStdout: []string{"serve", "init", "provider", "key", "usage", "scan", "setup", "doctor", "version",
				id.DocsURL(), id.IssuesURL()},
		},
		{name: "help flag", args: []string{"--help"}, wantCode: exitOK, wantStdout: []string{"Usage:"}},
		{name: "no arguments", args: nil, wantCode: exitUsage, wantStderr: []string{"Usage:"}},
		{
			name:       "unknown command",
			args:       []string{"bogus"},
			wantCode:   exitUsage,
			wantStderr: []string{`unknown command "bogus"`},
		},
	}
	for _, name := range []string{"provider"} {
		tests = append(tests, runCase{
			name:       name + " stub",
			args:       []string{name, "--flag"},
			wantCode:   exitError,
			wantStderr: []string{"chowki " + name + ": not implemented yet"},
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != tt.wantCode {
				t.Errorf("run(%q) = %d, want %d\nstderr: %s", tt.args, code, tt.wantCode, stderr.String())
			}
			for _, w := range tt.wantStdout {
				if !strings.Contains(stdout.String(), w) {
					t.Errorf("stdout doesn't contain %q:\n%s", w, stdout.String())
				}
			}
			for _, w := range tt.wantStderr {
				if !strings.Contains(stderr.String(), w) {
					t.Errorf("stderr doesn't contain %q:\n%s", w, stderr.String())
				}
			}
		})
	}
}
