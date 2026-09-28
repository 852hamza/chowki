package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/doctor"
)

func TestDoctor(t *testing.T) {
	t.Chdir(t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := run([]string{"doctor"}, &stdout, &stderr); code != exitError {
		t.Errorf("doctor without a configuration = %d, want %d", code, exitError)
	}
	for _, w := range []string{"fail  config", "run chowki init", "1 check failed. Fix what failed"} {
		if !strings.Contains(stdout.String(), w) {
			t.Errorf("the output lacks %q:\n%s", w, stdout.String())
		}
	}

	stdout.Reset()
	if code := run([]string{"init"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("init = %d\n%s", code, stderr.String())
	}
	t.Setenv("CHOWKI_SERVER_LISTEN", "127.0.0.1:0")
	t.Setenv("OPENAI_API_KEY", "not-a-real-key")
	stdout.Reset()
	if code := run([]string{"doctor", "--config", "chowki.yaml"}, &stdout, &stderr); code != exitOK {
		t.Errorf("doctor after init = %d, want %d\n%s", code, exitOK, stdout.String())
	}
	for _, w := range []string{"ok    provider openai     key in OPENAI_API_KEY",
		"warn  provider anthropic  ANTHROPIC_API_KEY isn't set", "warn  keys", "checks warned; none failed."} {
		if !strings.Contains(stdout.String(), w) {
			t.Errorf("the output lacks %q:\n%s", w, stdout.String())
		}
	}

	for _, args := range [][]string{{"doctor", "extra"}, {"doctor", "--bogus"}} {
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("run(%q) = %d, want %d", args, code, exitUsage)
		}
	}
	stdout.Reset()
	if code := run([]string{"doctor", "--help"}, &stdout, &stderr); code != exitOK ||
		!strings.Contains(stdout.String(), "chowki doctor [--config <FILE>]") {
		t.Errorf("doctor --help = %d:\n%s", code, stdout.String())
	}
}

func TestPrintDoctor(t *testing.T) {
	for _, tc := range []struct {
		results []doctor.Result
		code    int
		want    string
	}{
		{[]doctor.Result{{Check: "config", Status: doctor.OK, Message: "fine"}}, exitOK, "Every check passed."},
		{[]doctor.Result{{Check: "keys", Status: doctor.Warn, Message: "none"}}, exitOK, "1 check warned; none failed."},
		{[]doctor.Result{{Check: "config", Status: doctor.Fail, Message: "line one\nline two"},
			{Check: "keys", Status: doctor.Warn, Message: "none"}}, exitError,
			"fail  config  line one\n              line two\nwarn  keys    none\n\n1 check failed and 1 warned."},
	} {
		var out bytes.Buffer
		if code := printDoctor(&out, tc.results); code != tc.code || !strings.Contains(out.String(), tc.want) {
			t.Errorf("printDoctor() = %d:\n%s\nwant %d with %q", code, out.String(), tc.code, tc.want)
		}
	}
}
