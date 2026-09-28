package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"go.yaml.in/yaml/v3"

	"github.com/852hamza/chowki/internal/buildinfo"
)

// The GitHub Action loads as YAML, pins every action it uses to a commit,
// and passes its inputs to scripts through the environment, never inline,
// where a crafted input could inject commands.
func TestAction(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Runs struct {
			Using string `yaml:"using"`
			Steps []struct {
				Uses string `yaml:"uses"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &action); err != nil || action.Runs.Using != "composite" || len(action.Runs.Steps) == 0 {
		t.Fatalf("action.yml = %+v, %v", action, err)
	}
	pinned := regexp.MustCompile(`^[\w-]+/[\w/-]+@[0-9a-f]{40}$`)
	for _, s := range action.Runs.Steps {
		if s.Uses != "" && !pinned.MatchString(s.Uses) {
			t.Errorf("%s isn't pinned to a commit", s.Uses)
		}
		if regexp.MustCompile(`\$\{\{\s*inputs\.`).MatchString(s.Run) {
			t.Errorf("a script reads an input inline:\n%s", s.Run)
		}
	}
}

// A release takes its notes from CHANGELOG.md, which changelog.disable
// would drop, and ends them with instructions for its own version; a re-run
// of the workflow replaces its draft.
func TestGoReleaserConfig(t *testing.T) {
	var cfg struct {
		Changelog struct {
			Disable bool `yaml:"disable"`
		} `yaml:"changelog"`
		Release struct {
			Draft                bool   `yaml:"draft"`
			ReplaceExistingDraft bool   `yaml:"replace_existing_draft"`
			Footer               string `yaml:"footer"`
		} `yaml:"release"`
	}
	if err := yaml.Unmarshal([]byte(readDeploy(t, ".goreleaser.yaml")), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Changelog.Disable {
		t.Error("changelog.disable is set, which drops the notes that the workflow passes with --release-notes")
	}
	if !strings.Contains(readDeploy(t, ".github/workflows/release.yml"), "--release-notes") {
		t.Error("the Release workflow doesn't pass CHANGELOG.md's section with --release-notes")
	}
	if cfg.Release.Draft && !cfg.Release.ReplaceExistingDraft {
		t.Error("a re-run of the Release workflow would add a second draft; set release.replace_existing_draft")
	}
	footer, err := template.New("footer").Parse(cfg.Release.Footer)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := footer.Execute(&b, struct{ Tag, Version string }{"v1.2.3", "1.2.3"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CHOWKI_VERSION=v1.2.3 sh", "docker pull " + buildinfo.Project().Image() + ":1.2.3",
		"chowki_1.2.3_linux_amd64.tar.gz --repo " + buildinfo.Project().Owner + "/" + buildinfo.Project().Repo} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("the release footer for v1.2.3 lacks %q:\n%s", want, b.String())
		}
	}
}

// Every workflow pins its actions to commits, and passes values to its
// scripts through the environment, never inline, where a crafted tag or
// branch name could inject commands.
func TestWorkflows(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows: %v", err)
	}
	pinned := regexp.MustCompile(`^[\w-]+/[\w/-]+@[0-9a-f]{40}$`)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var wf struct {
			Jobs map[string]struct {
				Steps []struct {
					Uses string `yaml:"uses"`
					Run  string `yaml:"run"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &wf); err != nil || len(wf.Jobs) == 0 {
			t.Fatalf("%s: %+v, %v", f, wf, err)
		}
		for name, job := range wf.Jobs {
			for _, s := range job.Steps {
				if s.Uses != "" && !pinned.MatchString(s.Uses) {
					t.Errorf("%s, job %s: %s isn't pinned to a commit", filepath.Base(f), name, s.Uses)
				}
				if strings.Contains(s.Run, "${{") {
					t.Errorf("%s, job %s: a script reads an expression inline:\n%s", filepath.Base(f), name, s.Run)
				}
			}
		}
	}
}
