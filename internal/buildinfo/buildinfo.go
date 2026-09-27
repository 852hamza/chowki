package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Set by the Makefile with -ldflags -X. Empty means unknown; the accessors
// then fall back to the build information that the Go toolchain embeds.
var (
	version string
	commit  string
	date    string
)

// Identity is the public identity of the project: where its code, website and
// documentation live. It mirrors project.env, and its methods derive the
// remaining values; tools/projectsync uses the same methods. Project, in the
// generated project.go, returns the identity of this build.
type Identity struct {
	Domain  string // PROJECT_DOMAIN
	Owner   string // GITHUB_OWNER
	Repo    string // GITHUB_REPO
	Website string // WEBSITE_URL; empty means https://<Domain>
	Docs    string // DOCS_URL; empty means <WebsiteURL>/docs
}

// Module returns the Go module path, github.com/<Owner>/<Repo>.
func (id Identity) Module() string { return "github.com/" + id.Owner + "/" + id.Repo }

// RepoURL returns the repository URL, https://github.com/<Owner>/<Repo>.
func (id Identity) RepoURL() string { return "https://" + id.Module() }

// IssuesURL returns the URL of the repository's issue tracker.
func (id Identity) IssuesURL() string { return id.RepoURL() + "/issues" }

// Image returns the container image name. Registries require lowercase names,
// while GitHub owner names may contain capitals.
func (id Identity) Image() string { return strings.ToLower("ghcr.io/" + id.Owner + "/" + id.Repo) }

// WebsiteURL returns the project website: the WEBSITE_URL override, or
// https://<Domain>.
func (id Identity) WebsiteURL() string {
	if id.Website != "" {
		return id.Website
	}
	return "https://" + id.Domain
}

// DocsURL returns the documentation site: the DOCS_URL override, or
// <WebsiteURL>/docs.
func (id Identity) DocsURL() string {
	if id.Docs != "" {
		return id.Docs
	}
	return strings.TrimSuffix(id.WebsiteURL(), "/") + "/docs"
}

// Version returns the release version, such as "v0.1.0", or "dev" when the
// binary was built without one.
func Version() string {
	if version != "" {
		return version
	}
	// Set when the binary was installed with `go install <module>@<version>`.
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// Commit returns the git commit the binary was built from, or "" if unknown.
func Commit() string {
	if commit != "" {
		return commit
	}
	return vcsSetting("vcs.revision")
}

// Date returns the commit time of the build in RFC 3339 format, or "" if
// unknown. It is the commit time rather than the build time so that builds are
// reproducible.
func Date() string {
	if date != "" {
		return date
	}
	return vcsSetting("vcs.time")
}

func vcsSetting(key string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}
