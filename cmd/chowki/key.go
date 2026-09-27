package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/store"
)

const keyUsage = `Usage:
  chowki key create --name <NAME> [--project <PROJECT>] [--config <FILE>]
  chowki key list [--config <FILE>]
  chowki key revoke [--config <FILE>] <PREFIX>

A virtual key is shown once, when you create it. Revoke a key by its prefix,
the first 12 characters, as "chowki key list" shows them.
`

func runKey(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, keyUsage)
		return exitUsage
	}
	sub := map[string]func(context.Context, []string, io.Writer, io.Writer) int{
		"create": keyCreate,
		"list":   keyList,
		"revoke": keyRevoke,
	}[args[0]]
	switch {
	case args[0] == "help" || args[0] == "-h" || args[0] == "--help":
		fmt.Fprint(stdout, keyUsage)
		return exitOK
	case sub == nil:
		fmt.Fprintf(stderr, "chowki key: unknown command %q\n\n%s", args[0], keyUsage)
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return sub(ctx, args[1:], stdout, stderr)
}

func keyCreate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki key create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig, "configuration file")
	name := flags.String("name", "", "who or what uses the key, such as alice or ci-bot (required)")
	project := flags.String("project", "default", "project that the key belongs to")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return exitUsage
	}
	for label, v := range map[string]string{"--name": *name, "--project": *project} {
		if err := checkLabel(v); err != nil {
			fmt.Fprintf(stderr, "chowki key create: %s %v\n", label, err)
			return exitUsage
		}
	}
	return withStore(ctx, *configPath, stderr, "chowki key create", func(st store.Store) error {
		now := time.Now().UTC()
		key, k, err := auth.Create(ctx, st, *project, *name, now)
		if err != nil {
			return err
		}
		if err := st.AddAudit(ctx, store.AuditEvent{Time: now, Actor: "cli", Action: "key.create", Target: k.Prefix,
			Details: map[string]string{"name": k.Name, "project": k.Project}}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Created virtual key %q in project %q:\n\n  %s\n\n"+
			"Copy it now. Chowki stores only a hash of it and can't show it again.\n", k.Name, k.Project, key)
		return nil
	})
}

func keyList(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki key list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig, "configuration file")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return exitUsage
	}
	return withStore(ctx, *configPath, stderr, "chowki key list", func(st store.Store) error {
		keys, err := st.ListKeys(ctx)
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			fmt.Fprintln(stdout, "No virtual keys yet. Create one with: chowki key create --name <NAME>")
			return nil
		}
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PREFIX\tNAME\tPROJECT\tCREATED\tSTATUS")
		for _, k := range keys {
			status := "active"
			if k.Revoked() {
				status = "revoked " + formatTime(k.RevokedAt)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", k.Prefix, k.Name, k.Project, formatTime(k.CreatedAt), status)
		}
		return tw.Flush()
	})
}

func keyRevoke(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki key revoke", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig, "configuration file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		if err == nil {
			fmt.Fprint(stderr, keyUsage)
		}
		return exitUsage
	}
	prefix := auth.PrefixOf(flags.Arg(0))
	return withStore(ctx, *configPath, stderr, "chowki key revoke", func(st store.Store) error {
		now := time.Now().UTC()
		k, err := st.RevokeKey(ctx, prefix, now)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no virtual key has the prefix %q; see chowki key list", prefix)
		}
		if err != nil {
			return err
		}
		if !k.RevokedAt.Equal(now.Truncate(time.Millisecond)) {
			fmt.Fprintf(stdout, "Virtual key %s (%q) was already revoked on %s.\n", k.Prefix, k.Name, formatTime(k.RevokedAt))
			return nil
		}
		if err := st.AddAudit(ctx, store.AuditEvent{Time: now, Actor: "cli", Action: "key.revoke", Target: k.Prefix,
			Details: map[string]string{"name": k.Name, "project": k.Project}}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Revoked virtual key %s (%q). Requests with it now fail.\n", k.Prefix, k.Name)
		return nil
	})
}

// withStore loads the configuration, opens the database, runs fn and
// reports its error as the command's.
func withStore(ctx context.Context, configPath string, stderr io.Writer, command string, fn func(store.Store) error) int {
	err := func() error {
		env, err := config.DotEnv(".env")
		if err != nil {
			return err
		}
		cfg, err := config.Load(configPath, env)
		if err != nil {
			return err
		}
		st, err := store.OpenSQLite(ctx, cfg.Storage.DSN)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }() // read errors surface through fn
		return fn(st)
	}()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", command, err)
		return exitError
	}
	return exitOK
}

// checkLabel validates a key or project name, which appear in lists and
// logs.
func checkLabel(s string) error {
	switch {
	case s == "":
		return errors.New("is required")
	case len(s) > 64:
		return errors.New("must be at most 64 characters")
	case strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0:
		return errors.New("must contain only printable characters")
	}
	return nil
}

func formatTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }
