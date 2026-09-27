package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/buildinfo"
)

func TestParseEnv(t *testing.T) {
	const valid = "# comment\n\nPROJECT_DOMAIN=widget.example\nGITHUB_OWNER=acme\nGITHUB_REPO=widget\nWEBSITE_URL=\nDOCS_URL=\n"
	tests := []struct {
		name    string
		data    string
		isLock  bool
		want    lockState
		wantErr string
	}{
		{name: "valid", data: valid,
			want: lockState{id: buildinfo.Identity{Domain: "widget.example", Owner: "acme", Repo: "widget"}}},
		{name: "overrides",
			data: strings.NewReplacer("WEBSITE_URL=", "WEBSITE_URL=https://acme.github.io/widget",
				"DOCS_URL=", "DOCS_URL=https://docs.widget.example").Replace(valid),
			want: lockState{id: buildinfo.Identity{Domain: "widget.example", Owner: "acme", Repo: "widget",
				Website: "https://acme.github.io/widget", Docs: "https://docs.widget.example"}}},
		{name: "retired in lock", data: valid + "RETIRED=old.example\nRETIRED=github.com/old/widget\n", isLock: true,
			want: lockState{
				id:      buildinfo.Identity{Domain: "widget.example", Owner: "acme", Repo: "widget"},
				retired: []string{"old.example", "github.com/old/widget"},
			}},
		{name: "retired in project.env", data: valid + "RETIRED=old.example\n", wantErr: "unknown key RETIRED"},
		{name: "empty retired", data: valid + "RETIRED=\n", isLock: true, wantErr: "RETIRED needs a value"},
		{name: "missing equals", data: "PROJECT_DOMAIN\n", wantErr: "want KEY=VALUE"},
		{name: "space around equals", data: "PROJECT_DOMAIN = widget.example\n", wantErr: "want KEY=VALUE"},
		{name: "lowercase key", data: "project_domain=widget.example\n", wantErr: "want KEY=VALUE"},
		{name: "unknown key", data: valid + "GITHUB_REPOSITORY=widget\n", wantErr: "unknown key GITHUB_REPOSITORY"},
		{name: "duplicate key", data: valid + "GITHUB_OWNER=other\n", wantErr: "GITHUB_OWNER is set twice"},
		{name: "quoted value", data: `PROJECT_DOMAIN="widget.example"` + "\n", wantErr: "plain value"},
		{name: "dollar", data: "WEBSITE_URL=https://$PROJECT_DOMAIN\n", wantErr: "plain value"},
		{name: "CRLF", data: strings.ReplaceAll(valid, "\n", "\r\n"), wantErr: "LF line endings"},
		{name: "missing owner", data: "PROJECT_DOMAIN=widget.example\nGITHUB_REPO=widget\n", wantErr: `GITHUB_OWNER ""`},
		{name: "uppercase domain", data: strings.Replace(valid, "widget.example", "Widget.example", 1), wantErr: "PROJECT_DOMAIN"},
		{name: "domain without dot", data: strings.Replace(valid, "widget.example", "localhost", 1), wantErr: "PROJECT_DOMAIN"},
		{name: "owner with leading hyphen", data: strings.Replace(valid, "acme", "-acme", 1), wantErr: "GITHUB_OWNER"},
		{name: "repo with slash", data: strings.Replace(valid, "GITHUB_REPO=widget", "GITHUB_REPO=a/b", 1), wantErr: "GITHUB_REPO"},
		{name: "http website", data: strings.Replace(valid, "WEBSITE_URL=", "WEBSITE_URL=http://widget.example", 1),
			wantErr: "WEBSITE_URL"},
		{name: "docs with query", data: strings.Replace(valid, "DOCS_URL=", "DOCS_URL=https://widget.example/docs?x=1", 1),
			wantErr: "DOCS_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEnv("test.env", []byte(tt.data), tt.isLock)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseEnv() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEnv() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseEnv() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLockRoundTrip(t *testing.T) {
	st := lockState{
		id:      buildinfo.Identity{Domain: "widget.example", Owner: "acme", Repo: "widget", Website: "https://acme.github.io/widget"},
		retired: []string{"github.com/old/widget", "old.example"},
	}
	got, err := parseEnv(lockFile, formatLock(st), true)
	if err != nil {
		t.Fatalf("parseEnv(formatLock()) error = %v", err)
	}
	if !reflect.DeepEqual(got, st) {
		t.Errorf("round trip = %+v, want %+v", got, st)
	}
}

func TestReplacementsConflict(t *testing.T) {
	// The old website equals the old repository URL, but the two values
	// change differently, so there is no correct rewrite.
	old := buildinfo.Identity{Domain: "widget.example", Owner: "acme", Repo: "widget", Website: "https://github.com/acme/widget"}
	cur := buildinfo.Identity{Domain: "widget.example", Owner: "zeta", Repo: "widget", Website: "https://widget.example"}
	if _, err := replacements(old, cur); err == nil || !strings.Contains(err.Error(), "cannot rename") {
		t.Fatalf("replacements() error = %v, want a conflict", err)
	}
}
