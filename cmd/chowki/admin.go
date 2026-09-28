package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
)

const adminUsage = `Usage:
  chowki admin create --name <NAME> [--config <FILE>]
  chowki admin list [--config <FILE>]
  chowki admin revoke [--config <FILE>] <PREFIX>

An admin token authorizes the admin API and the dashboard. It's shown once,
when you create it. Revoke a token by its prefix, the first 18 characters,
as "chowki admin list" shows them.
`

func runAdmin(args []string, stdout, stderr io.Writer) int {
	return runSubcommand("admin", adminUsage, map[string]subcommand{
		"create": adminCreate,
		"list":   adminList,
		"revoke": adminRevoke,
	}, args, stdout, stderr)
}

func adminCreate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki admin create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
	name := flags.String("name", "", "who uses the token, such as ops or dashboard (required)")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return exitUsage
	}
	if err := store.CheckName(*name); err != nil {
		fmt.Fprintf(stderr, "chowki admin create: --name %v\n", err)
		return exitUsage
	}
	return withStore(ctx, *configPath, stderr, "chowki admin create", func(st store.Store) error {
		now := time.Now().UTC()
		token, t, err := auth.CreateAdmin(ctx, st, *name, now)
		if err != nil {
			return err
		}
		if err := st.AddAudit(ctx, store.AuditEvent{Time: now, Actor: "cli", Action: "admin.create", Target: t.Prefix,
			Details: map[string]string{"name": t.Name}}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Created admin token %q:\n\n  %s\n\n"+
			"Copy it now. Chowki stores only a hash of it and can't show it again.\n"+
			"It can read and change every key and project; keep it as safe as the master key.\n", t.Name, token)
		return nil
	})
}

func adminList(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki admin list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return exitUsage
	}
	return withStore(ctx, *configPath, stderr, "chowki admin list", func(st store.Store) error {
		tokens, err := st.ListAdminTokens(ctx)
		if err != nil {
			return err
		}
		if len(tokens) == 0 {
			fmt.Fprintln(stdout, "No admin tokens yet. Create one with: chowki admin create --name <NAME>")
			return nil
		}
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PREFIX\tNAME\tCREATED\tSTATUS")
		for _, t := range tokens {
			status := "active"
			if t.Revoked() {
				status = "revoked " + formatTime(t.RevokedAt)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t.Prefix, t.Name, formatTime(t.CreatedAt), status)
		}
		return tw.Flush()
	})
}

func adminRevoke(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki admin revoke", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		if err == nil {
			fmt.Fprint(stderr, adminUsage)
		}
		return exitUsage
	}
	prefix := auth.AdminPrefixOf(flags.Arg(0))
	return withStore(ctx, *configPath, stderr, "chowki admin revoke", func(st store.Store) error {
		now := time.Now().UTC()
		t, err := st.RevokeAdminToken(ctx, prefix, now)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no admin token has the prefix %q; see chowki admin list", prefix)
		}
		if err != nil {
			return err
		}
		if !t.RevokedAt.Equal(now.Truncate(time.Millisecond)) {
			fmt.Fprintf(stdout, "Admin token %s (%q) was already revoked on %s.\n", t.Prefix, t.Name, formatTime(t.RevokedAt))
			return nil
		}
		if err := st.AddAudit(ctx, store.AuditEvent{Time: now, Actor: "cli", Action: "admin.revoke", Target: t.Prefix,
			Details: map[string]string{"name": t.Name}}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Revoked admin token %s (%q). Requests with it now fail.\n", t.Prefix, t.Name)
		return nil
	})
}
