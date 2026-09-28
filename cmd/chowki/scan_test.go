package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/testutil"
)

func TestScan(t *testing.T) {
	clean, dirty := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(clean, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	aws := testutil.Secrets()[1].Value
	if err := os.WriteFile(filepath.Join(dirty, "config.yaml"), []byte("key: "+aws+"\nmail: jane.doe@company.io\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(t.TempDir(), "results.sarif")
	tests := []runCase{
		{name: "clean", args: []string{"scan", clean}, wantCode: exitOK,
			wantStdout: []string{"No secrets found in 1 file."}},
		{name: "a secret", args: []string{"scan", dirty}, wantCode: exitError,
			wantStdout: []string{"config.yaml:1:6: aws_access_key: An AWS access key ID.", "Found 1 secret in 1 file"}},
		{name: "personal data too", args: []string{"scan", "--pii", dirty}, wantCode: exitError,
			wantStdout: []string{"config.yaml:2:7: email", "Found 2 secrets"}},
		{name: "exit zero", args: []string{"scan", "--exit-zero", dirty}, wantCode: exitOK,
			wantStdout: []string{"aws_access_key"}},
		{name: "two paths", args: []string{"scan", clean, dirty}, wantCode: exitError,
			wantStdout: []string{"(2 files scanned)"}},
		{name: "SARIF to a file", args: []string{"scan", "--format", "sarif", "--output", report, dirty},
			wantCode: exitError, wantStderr: []string{"found 1 secret; see " + report}},
		{name: "unknown format", args: []string{"scan", "--format", "xml", clean}, wantCode: exitUsage,
			wantStderr: []string{"--format must be text, json or sarif"}},
		{name: "missing path", args: []string{"scan", filepath.Join(clean, "nope")}, wantCode: exitScanFailed,
			wantStderr: []string{"no such file"}},
		{name: "unknown flag", args: []string{"scan", "--fast"}, wantCode: exitUsage, wantStderr: []string{"Usage:"}},
		{name: "help", args: []string{"scan", "--help"}, wantCode: exitOK, wantStdout: []string{"--exit-zero"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != tt.wantCode {
				t.Errorf("run(%q) = %d, want %d\nstdout: %s\nstderr: %s", tt.args, code, tt.wantCode, stdout.String(),
					stderr.String())
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
			if strings.Contains(stdout.String()+stderr.String(), aws) {
				t.Error("the output shows the secret")
			}
		})
	}
	data, err := os.ReadFile(report)
	var log struct {
		Runs []struct {
			Tool struct {
				Driver struct{ Name, Version, InformationURI string } `json:"driver"`
			} `json:"tool"`
			Results []json.RawMessage `json:"results"`
		} `json:"runs"`
	}
	if err != nil || json.Unmarshal(data, &log) != nil || len(log.Runs) != 1 || len(log.Runs[0].Results) != 1 ||
		log.Runs[0].Tool.Driver.Name != "chowki" || !strings.HasPrefix(log.Runs[0].Tool.Driver.InformationURI, "https://") {
		t.Errorf("SARIF report = %s, %v", data, err)
	}
	var stdout bytes.Buffer
	if code := run([]string{"scan", "--format", "json", dirty}, &stdout, &bytes.Buffer{}); code != exitError ||
		!json.Valid(stdout.Bytes()) {
		t.Errorf("JSON report: %d\n%s", code, stdout.String())
	}
}
