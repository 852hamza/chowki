package buildinfo

import "testing"

func TestIdentityDerivedValues(t *testing.T) {
	tests := []struct {
		name                       string
		id                         Identity
		module, fullName, repoURL  string
		issues                     string
		image, websiteURL, docsURL string
	}{
		{
			name:       "defaults",
			id:         Identity{Domain: "example.org", Owner: "acme", Repo: "widget"},
			module:     "github.com/acme/widget",
			fullName:   "acme/widget",
			repoURL:    "https://github.com/acme/widget",
			issues:     "https://github.com/acme/widget/issues",
			image:      "ghcr.io/acme/widget",
			websiteURL: "https://example.org",
			docsURL:    "https://example.org/docs",
		},
		{
			name:       "website override moves docs",
			id:         Identity{Domain: "example.org", Owner: "acme", Repo: "widget", Website: "https://acme.github.io/widget/"},
			module:     "github.com/acme/widget",
			fullName:   "acme/widget",
			repoURL:    "https://github.com/acme/widget",
			issues:     "https://github.com/acme/widget/issues",
			image:      "ghcr.io/acme/widget",
			websiteURL: "https://acme.github.io/widget/",
			docsURL:    "https://acme.github.io/widget/docs",
		},
		{
			name:       "docs override",
			id:         Identity{Domain: "example.org", Owner: "acme", Repo: "widget", Docs: "https://docs.example.net"},
			module:     "github.com/acme/widget",
			fullName:   "acme/widget",
			repoURL:    "https://github.com/acme/widget",
			issues:     "https://github.com/acme/widget/issues",
			image:      "ghcr.io/acme/widget",
			websiteURL: "https://example.org",
			docsURL:    "https://docs.example.net",
		},
		{
			name:       "image is lowercase",
			id:         Identity{Domain: "example.org", Owner: "AcMe", Repo: "Widget"},
			module:     "github.com/AcMe/Widget",
			fullName:   "AcMe/Widget",
			repoURL:    "https://github.com/AcMe/Widget",
			issues:     "https://github.com/AcMe/Widget/issues",
			image:      "ghcr.io/acme/widget",
			websiteURL: "https://example.org",
			docsURL:    "https://example.org/docs",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string][2]string{
				"Module":     {tt.id.Module(), tt.module},
				"FullName":   {tt.id.FullName(), tt.fullName},
				"RepoURL":    {tt.id.RepoURL(), tt.repoURL},
				"IssuesURL":  {tt.id.IssuesURL(), tt.issues},
				"Image":      {tt.id.Image(), tt.image},
				"WebsiteURL": {tt.id.WebsiteURL(), tt.websiteURL},
				"DocsURL":    {tt.id.DocsURL(), tt.docsURL},
			}
			for name, v := range got {
				if v[0] != v[1] {
					t.Errorf("%s() = %q, want %q", name, v[0], v[1])
				}
			}
		})
	}
}

func TestProjectDefaults(t *testing.T) {
	id := Project()
	if id.Domain == "" || id.Owner == "" || id.Repo == "" {
		t.Fatalf("Project() = %+v: generated defaults are missing; run make sync", id)
	}
}

func TestLinkTimeValuesWin(t *testing.T) {
	saved := [...]string{version, commit, date, owner}
	t.Cleanup(func() { version, commit, date, owner = saved[0], saved[1], saved[2], saved[3] })

	version, commit, date, owner = "v9.9.9", "abc123", "2026-01-02T03:04:05Z", "someone"
	if got := Version(); got != "v9.9.9" {
		t.Errorf("Version() = %q, want v9.9.9", got)
	}
	if got := Commit(); got != "abc123" {
		t.Errorf("Commit() = %q, want abc123", got)
	}
	if got := Date(); got != "2026-01-02T03:04:05Z" {
		t.Errorf("Date() = %q, want 2026-01-02T03:04:05Z", got)
	}
	if got := Project().Owner; got != "someone" {
		t.Errorf("Project().Owner = %q, want someone", got)
	}
}

func TestVersionWithoutLinkTimeValue(t *testing.T) {
	saved := version
	t.Cleanup(func() { version = saved })

	version = ""
	// Test binaries carry no module version, so the fallback is "dev".
	if got := Version(); got != "dev" {
		t.Errorf("Version() = %q, want dev", got)
	}
}
