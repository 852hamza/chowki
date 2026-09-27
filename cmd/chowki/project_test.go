package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestProjectCommands(t *testing.T) {
	initDir(t)
	if out := runOK(t, "project", "list"); !strings.Contains(out, "No projects yet") {
		t.Errorf("empty list = %q", out)
	}
	runOK(t, "key", "create", "--name", "alice", "--project", "team")
	runOK(t, "key", "create", "--name", "bob", "--project", "team")
	out := runOK(t, "key", "create", "--name", "carol", "--project", "team")
	runOK(t, "key", "revoke", printedKeyRE.FindStringSubmatch(out)[1])

	if out := runOK(t, "project", "update", "--budget-usd", "200", "team"); !strings.Contains(out,
		`Project "team" now has a monthly budget of $200.00, shared by its keys.`) {
		t.Errorf("update output = %q", out)
	}
	list := runOK(t, "project", "list")
	lines := strings.Split(strings.TrimSpace(list), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "NAME") || strings.Join(strings.Fields(lines[1]), " ") !=
		"team 2 $0.00 $200.00" {
		t.Errorf("list = %q; want team with 2 active keys, $0.00 spent and a $200.00 budget", list)
	}
	if out := runOK(t, "project", "update", "--budget-usd", "0", "team"); !strings.Contains(out, "now has no monthly budget") {
		t.Errorf("update to 0 output = %q", out)
	}
	if list := runOK(t, "project", "list"); !strings.Contains(list, "none") {
		t.Errorf("list after removing the budget:\n%s", list)
	}
	if n := countAudit(t, "project.update", "team"); n != 2 {
		t.Errorf("audit log has %d project.update entries, want 2", n)
	}
}

func TestProjectErrors(t *testing.T) {
	initDir(t)
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"project"}, exitUsage, "Usage:"},
		{[]string{"project", "delete"}, exitUsage, `unknown command "delete"`},
		{[]string{"project", "list", "extra"}, exitUsage, ""},
		{[]string{"project", "update", "team"}, exitUsage, "nothing to change"},
		{[]string{"project", "update", "--budget-usd", "5"}, exitUsage, "Usage:"},
		{[]string{"project", "update", "--budget-usd", "-5", "team"}, exitUsage, "must be an amount"},
		{[]string{"project", "update", "--budget-usd", "5", "nope"}, exitError, `no project is named "nope"`},
		{[]string{"project", "list", "--config", "missing.yaml"}, exitError, "read config"},
	}
	for _, tt := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(tt.args, &stdout, &stderr); code != tt.code {
			t.Errorf("chowki %q = %d, want %d\nstderr: %s", tt.args, code, tt.code, stderr.String())
		}
		if !strings.Contains(stderr.String(), tt.want) {
			t.Errorf("chowki %q stderr doesn't contain %q:\n%s", tt.args, tt.want, stderr.String())
		}
	}
	if out := runOK(t, "project", "help"); !strings.Contains(out, "chowki project update") {
		t.Errorf("project help = %q", out)
	}
}
