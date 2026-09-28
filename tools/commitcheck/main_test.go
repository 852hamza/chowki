package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProblems(t *testing.T) {
	const signOff = "\n\nSigned-off-by: Ada Lovelace <ada@example.com>\n"
	tests := []struct {
		name    string
		message string
		want    []string // substrings, one per expected problem
	}{
		{"valid", "feat(auth): add virtual key revocation" + signOff, nil},
		{"valid without scope", "docs: fix a typo" + signOff, nil},
		{"breaking change", "feat(api)!: rename the usage endpoint" + signOff, nil},
		{"scope with slash and dot", "fix(providers/openai.v2): retry on 429" + signOff, nil},
		{"revert type", "revert: undo the cache change" + signOff, nil},
		{"git revert subject", `Revert "feat(auth): add virtual key revocation"` + signOff, nil},
		{"sign-off case and extra trailers", "fix(sse): relay chunks\n\nCo-Authored-By: X <x@example.org>\n" +
			"signed-off-by: Ada Lovelace <ADA@example.com>\n", nil},
		{"unknown type", "feature(auth): add revocation" + signOff, []string{"Conventional Commit"}},
		{"uppercase scope", "feat(Auth): add revocation" + signOff, []string{"Conventional Commit"}},
		{"missing space", "feat(auth):add revocation" + signOff, []string{"Conventional Commit"}},
		{"too long", "feat: " + strings.Repeat("x", 73) + signOff, []string{"Conventional Commit"}},
		{"72 characters", "feat: " + strings.Repeat("x", 72) + signOff, nil},
		{"fixup", "fixup! feat(auth): add revocation" + signOff, []string{"squash fixup commits"}},
		{"no sign-off", "feat(auth): add revocation\n", []string{"Signed-off-by: Ada Lovelace <ada@example.com>"}},
		{"sign-off by someone else", "feat(auth): add revocation\n\nSigned-off-by: Bob <bob@example.com>\n",
			[]string{"no \"Signed-off-by"}},
		{"both wrong", "Add revocation\n", []string{"Conventional Commit", "Signed-off-by"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := problems(commit{hash: "abc", name: "Ada Lovelace", email: "ada@example.com", message: tt.message})
			if len(got) != len(tt.want) {
				t.Fatalf("problems() = %q, want %d problem(s) matching %q", got, len(tt.want), tt.want)
			}
			for i, w := range tt.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("problem %d = %q, want it to contain %q", i, got[i], w)
				}
			}
		})
	}
}

// Dependabot signs off with GitHub's support address, which only its own
// commits may use. The subjects are the ones that .github/dependabot.yml
// makes, for one update and for a group.
func TestDependabotSignOff(t *testing.T) {
	for _, subject := range []string{
		"build(deps): bump modernc.org/sqlite from 1.59.0 to 1.60.0",
		"ci(deps): bump the actions group across 1 directory with 6 updates",
	} {
		message := subject + "\n\nBumps ...\n\nSigned-off-by: dependabot[bot] <support@github.com>\n"
		bot := commit{hash: "abc", name: "dependabot[bot]", email: "49699333+dependabot[bot]@users.noreply.github.com",
			message: message}
		if got := problems(bot); len(got) != 0 {
			t.Errorf("Dependabot's %q: problems() = %q, want none", subject, got)
		}
		person := commit{hash: "abc", name: "Ada Lovelace", email: "ada@example.com", message: message}
		if got := problems(person); len(got) != 1 || !strings.Contains(got[0], "Signed-off-by: Ada Lovelace") {
			t.Errorf("a person's commit with Dependabot's sign-off: problems() = %q, want a missing sign-off", got)
		}
	}
}

func TestRun(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not in PATH")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.name=Ada Lovelace",
			"-c", "user.email=ada@example.com", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commitFile := func(name, message string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		git("add", name)
		git("commit", "-q", "-m", message)
		return git("rev-parse", "HEAD")
	}
	git("init", "-q")
	base := commitFile("a", "chore: start\n\nSigned-off-by: Ada Lovelace <ada@example.com>")
	commitFile("b", "feat(x): add b\n\nSigned-off-by: Ada Lovelace <ada@example.com>")
	head := commitFile("c", "add c without the rules")

	tests := []struct {
		name, base string
		wantOK     bool
		wantOut    []string
	}{
		{"range", base, false, []string{`"add c without the rules": the subject`, `"add c without the rules": no "Signed-off-by`}},
		{"new branch", strings.Repeat("0", 40), false, []string{`"add c without the rules"`}},
		{"unknown base", strings.Repeat("1", 40), false, []string{"is not in the history", `"add c without the rules"`}},
		{"only good commits", base, true, []string{"1 commit(s) follow the rules"}},
	}
	if _, err := run(t.Context(), dir, "--output=x", head, &bytes.Buffer{}); err == nil {
		t.Error("run() accepted a base that git would read as an option")
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := head
			if tt.wantOK {
				h = head + "~1"
			}
			var out bytes.Buffer
			ok, err := run(t.Context(), dir, tt.base, h, &out)
			if err != nil {
				t.Fatalf("run() error = %v", err)
			}
			if ok != tt.wantOK {
				t.Errorf("run() = %v, want %v\n%s", ok, tt.wantOK, out.String())
			}
			for _, w := range tt.wantOut {
				if !strings.Contains(out.String(), w) {
					t.Errorf("output doesn't contain %q:\n%s", w, out.String())
				}
			}
		})
	}
}
