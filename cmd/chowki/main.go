// Command chowki is the Chowki AI gateway: the HTTP server that relays LLM
// traffic, and the CLI that manages it.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"text/tabwriter"

	"github.com/852hamza/chowki/internal/buildinfo"
)

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// command is a chowki subcommand.
type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

func commands() []command {
	return []command{
		{"serve", "Run the gateway", runServe},
		{"init", "Create a configuration, a master key and the database", runInit},
		{"provider", "Manage provider keys", notImplemented("provider")},
		{"key", "Create, list, update and revoke virtual keys", runKey},
		{"project", "List projects and set their budgets", runProject},
		{"admin", "Create, list and revoke admin tokens", runAdmin},
		{"usage", "Report token usage, cost and savings", notImplemented("usage")},
		{"scan", "Find secrets in a repository, .env files or MCP configurations", runScan},
		{"setup", "Connect an app or coding agent to the gateway", runSetup},
		{"doctor", "Check the installation and configuration", notImplemented("doctor")},
		{"version", "Print version information", runVersion},
	}
}

// subcommand runs a subcommand of a command group, such as "key create".
type subcommand func(ctx context.Context, args []string, stdout, stderr io.Writer) int

// runSubcommand runs the subcommand of group that args[0] names, or prints
// the group's usage.
func runSubcommand(group, usage string, subs map[string]subcommand, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	sub := subs[args[0]]
	switch {
	case args[0] == "help" || args[0] == "-h" || args[0] == "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	case sub == nil:
		fmt.Fprintf(stderr, "chowki %s: unknown command %q\n\n%s", group, args[0], usage)
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return sub(ctx, args[1:], stdout, stderr)
}

// run executes the command line args and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		usage(stdout)
		return exitOK
	}
	for _, c := range commands() {
		if c.name == args[0] {
			return c.run(args[1:], stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "chowki: unknown command %q\nRun 'chowki help' for usage.\n", args[0])
	return exitUsage
}

func usage(w io.Writer) {
	id := buildinfo.Project()
	fmt.Fprint(w, "Chowki is a self-hosted AI gateway.\n\nUsage:\n  chowki <command> [arguments]\n\nCommands:\n")
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	for _, c := range commands() {
		fmt.Fprintf(tw, "  %s\t%s\n", c.name, c.summary)
	}
	_ = tw.Flush() // w is stdout or stderr; there is nothing useful to do on failure
	fmt.Fprintf(w, "\nDocumentation: %s\nReport a bug:  %s\n", id.DocsURL(), id.IssuesURL())
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "chowki version: takes no arguments")
		return exitUsage
	}
	fmt.Fprintf(stdout, "chowki %s\ncommit: %s\ndate:   %s\ngo:     %s %s/%s\nrepo:   %s\n",
		buildinfo.Version(), orUnknown(buildinfo.Commit()), orUnknown(buildinfo.Date()),
		runtime.Version(), runtime.GOOS, runtime.GOARCH, buildinfo.Project().RepoURL())
	return exitOK
}

func notImplemented(name string) func([]string, io.Writer, io.Writer) int {
	return func(_ []string, _, stderr io.Writer) int {
		fmt.Fprintf(stderr, "chowki %s: not implemented yet\n", name)
		return exitError
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
