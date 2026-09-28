package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
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
