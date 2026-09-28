package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/testutil"
)

// The CLI reference is each command's usage, as --help prints it.
func TestCLIReference(t *testing.T) {
	help := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitOK || stdout.Len() == 0 {
			t.Fatalf("chowki %s = %d, stdout %q, stderr %q", strings.Join(args, " "), code, stdout.String(),
				stderr.String())
		}
		return stdout.String()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n```text\n$ chowki help\n%s```\n", help("help"))
	for _, c := range commands() {
		fmt.Fprintf(&b, "\n## `chowki %s`\n\n%s.\n\n```text\n%s```\n\n", c.name, c.summary, help(c.name, "--help"))
	}
	testutil.CheckGenerated(t, "docs/reference/cli.md", "commands", b.String())
}
