// Command commitcheck checks that commits follow the contribution rules: a
// Conventional Commits subject, and a Developer Certificate of Origin sign-off
// with the commit author's email. CI runs it on every pull request and push:
//
//	go run ./tools/commitcheck <base> <head>
//
// It checks the commits reachable from head but not from base, except merges.
// When base is empty, all zeros (a new branch) or unknown (a force push), it
// checks head alone.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"slices"
	"strings"
)

// subjectRE is the rule of the commit-msg hook in the contributing guide:
// type(optional scope)!: description of at most 72 characters.
var subjectRE = regexp.MustCompile(`^(feat|fix|docs|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9._/-]+\))?!?: [^ ].{0,71}$`)

type commit struct {
	hash, name, email, message string
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: commitcheck <base> <head>")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	ok, err := run(ctx, ".", os.Args[1], os.Args[2], os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "commitcheck:", err)
		os.Exit(1)
	}
	if !ok {
		os.Exit(1)
	}
}

// run checks the commits in base..head of the repository in dir, reports to
// out, and returns whether all of them pass.
func run(ctx context.Context, dir, base, head string, out io.Writer) (bool, error) {
	// git would read an argument starting with "-" as an option.
	if strings.HasPrefix(base, "-") || strings.HasPrefix(head, "-") || head == "" {
		return false, fmt.Errorf("invalid revisions %q and %q", base, head)
	}
	if base != "" && strings.Trim(base, "0") != "" && !exists(ctx, dir, base) {
		fmt.Fprintf(out, "base %s is not in the history (force push?); checking %s only\n", base, head)
		base = ""
	}
	commits, err := listCommits(ctx, dir, base, head)
	if err != nil {
		return false, err
	}
	ok := true
	for _, c := range commits {
		for _, p := range problems(c) {
			fmt.Fprintf(out, "%.12s %q: %s\n", c.hash, subject(c.message), p)
			ok = false
		}
	}
	if ok {
		fmt.Fprintf(out, "%d commit(s) follow the rules\n", len(commits))
	}
	return ok, nil
}

// listCommits returns the non-merge commits in base..head, oldest first, or
// head alone when base is empty or all zeros.
func listCommits(ctx context.Context, dir, base, head string) ([]commit, error) {
	args := []string{"log", "--no-merges", "--reverse", "--format=%H%x00%an%x00%ae%x00%B%x1e"}
	if base == "" || strings.Trim(base, "0") == "" {
		args = append(args, "-1", head)
	} else {
		args = append(args, base+".."+head)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	outb, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	var commits []commit
	for rec := range strings.SplitSeq(string(outb), "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x00", 4)
		if len(f) != 4 {
			return nil, fmt.Errorf("unexpected git log record %q", rec)
		}
		commits = append(commits, commit{hash: f[0], name: f[1], email: f[2], message: f[3]})
	}
	return commits, nil
}

func exists(ctx context.Context, dir, rev string) bool {
	cmd := exec.CommandContext(ctx, "git", "cat-file", "-e", rev+"^{commit}")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// problems returns what is wrong with c, if anything.
func problems(c commit) []string {
	var out []string
	s := subject(c.message)
	switch {
	case strings.HasPrefix(s, `Revert "`):
		// The subject that git revert writes; it names the reverted commit.
	case strings.HasPrefix(s, "fixup! "), strings.HasPrefix(s, "squash! "), strings.HasPrefix(s, "amend! "):
		out = append(out, "squash fixup commits before merging")
	case !subjectRE.MatchString(s):
		out = append(out, `the subject must be a Conventional Commit such as "fix(sse): relay chunks without buffering": `+
			"type feat, fix, docs, refactor, perf, test, build, ci, chore or revert, an optional scope, "+
			"and a description of at most 72 characters")
	}
	if !signedOff(c) {
		out = append(out, fmt.Sprintf("no \"Signed-off-by: %s <%s>\" line; add it with git commit -s", c.name, c.email))
	}
	return out
}

func subject(message string) string {
	s, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	return strings.TrimSpace(s)
}

// Dependabot commits as its GitHub account but signs off with GitHub's
// support address.
const (
	dependabotEmail   = "49699333+dependabot[bot]@users.noreply.github.com"
	dependabotSignOff = "support@github.com"
)

// signedOff reports whether the message has a sign-off with the author's
// email address, or with Dependabot's on a Dependabot commit.
func signedOff(c commit) bool {
	emails := []string{c.email}
	if strings.EqualFold(c.email, dependabotEmail) {
		emails = append(emails, dependabotSignOff)
	}
	for line := range strings.SplitSeq(c.message, "\n") {
		line = strings.TrimSpace(line)
		const prefix = "signed-off-by: "
		if len(line) < len(prefix) || !strings.EqualFold(line[:len(prefix)], prefix) || !strings.HasSuffix(line, ">") {
			continue
		}
		i := strings.LastIndexByte(line, '<')
		if i < 0 {
			continue
		}
		email := line[i+1 : len(line)-1]
		if slices.ContainsFunc(emails, func(e string) bool { return strings.EqualFold(email, e) }) {
			return true
		}
	}
	return false
}
