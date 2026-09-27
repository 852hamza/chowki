package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/budget"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/store"
)

const keyUsage = `Usage:
  chowki key create --name <NAME> [--project <PROJECT>] [--budget-usd <USD>] [--config <FILE>]
  chowki key list [--config <FILE>]
  chowki key update [--config <FILE>] --budget-usd <USD> <PREFIX>
  chowki key revoke [--config <FILE>] <PREFIX>

A virtual key is shown once, when you create it. Update or revoke a key by
its prefix, the first 12 characters, as "chowki key list" shows them.
--budget-usd sets a monthly budget in US dollars; 0 means no budget.
`

func runKey(args []string, stdout, stderr io.Writer) int {
	return runSubcommand("key", keyUsage, map[string]subcommand{
		"create": keyCreate,
		"list":   keyList,
		"update": keyUpdate,
		"revoke": keyRevoke,
	}, args, stdout, stderr)
}

func keyCreate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki key create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig, "configuration file")
	name := flags.String("name", "", "who or what uses the key, such as alice or ci-bot (required)")
	project := flags.String("project", "default", "project that the key belongs to")
	budgetUSD := flags.Float64("budget-usd", 0, "monthly budget in US dollars; 0 means none")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return exitUsage
	}
	for label, v := range map[string]string{"--name": *name, "--project": *project} {
		if err := checkLabel(v); err != nil {
			fmt.Fprintf(stderr, "chowki key create: %s %v\n", label, err)
			return exitUsage
		}
	}
	if err := checkBudget(*budgetUSD); err != nil {
		fmt.Fprintf(stderr, "chowki key create: %v\n", err)
		return exitUsage
	}
	return withStore(ctx, *configPath, stderr, "chowki key create", func(st store.Store) error {
		now := time.Now().UTC()
		key, k, err := auth.Create(ctx, st, *project, store.Key{Name: *name, BudgetUSD: *budgetUSD}, now)
		if err != nil {
			return err
		}
		details := map[string]string{"name": k.Name, "project": k.Project}
		withBudget := ""
		if k.BudgetUSD > 0 {
			details["monthly_budget_usd"] = formatAmount(k.BudgetUSD)
			withBudget = " with a monthly budget of " + budget.FormatUSD(k.BudgetUSD)
		}
		if err := st.AddAudit(ctx, store.AuditEvent{Time: now, Actor: "cli", Action: "key.create", Target: k.Prefix,
			Details: details}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Created virtual key %q in project %q%s:\n\n  %s\n\n"+
			"Copy it now. Chowki stores only a hash of it and can't show it again.\n", k.Name, k.Project, withBudget, key)
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
		period := store.Period(time.Now())
		spend, err := st.SpendByKey(ctx, period)
		if err != nil {
			return err
		}
		spent := map[int64]float64{}
		for _, s := range spend {
			spent[s.KeyID] = s.USD
		}
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "PREFIX\tNAME\tPROJECT\tSPENT %s\tBUDGET\tCREATED\tSTATUS\n", period)
		for _, k := range keys {
			status := "active"
			if k.Revoked() {
				status = "revoked " + formatTime(k.RevokedAt)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", k.Prefix, k.Name, k.Project,
				budget.FormatUSD(spent[k.ID]), formatBudget(k.BudgetUSD), formatTime(k.CreatedAt), status)
		}
		return tw.Flush()
	})
}

func keyUpdate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki key update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig, "configuration file")
	budgetUSD := flags.Float64("budget-usd", 0, "monthly budget in US dollars; 0 removes it")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		if err == nil {
			fmt.Fprint(stderr, keyUsage)
		}
		return exitUsage
	}
	var u store.KeyUpdate
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "budget-usd" {
			u.BudgetUSD = budgetUSD
		}
	})
	if u.BudgetUSD == nil {
		fmt.Fprint(stderr, "chowki key update: nothing to change; set --budget-usd\n")
		return exitUsage
	}
	if err := checkBudget(*u.BudgetUSD); err != nil {
		fmt.Fprintf(stderr, "chowki key update: %v\n", err)
		return exitUsage
	}
	prefix := auth.PrefixOf(flags.Arg(0))
	return withStore(ctx, *configPath, stderr, "chowki key update", func(st store.Store) error {
		k, err := st.UpdateKey(ctx, prefix, u)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no virtual key has the prefix %q; see chowki key list", prefix)
		}
		if err != nil {
			return err
		}
		if err := st.AddAudit(ctx, store.AuditEvent{Time: time.Now().UTC(), Actor: "cli", Action: "key.update",
			Target: k.Prefix, Details: map[string]string{"name": k.Name, "project": k.Project,
				"monthly_budget_usd": formatAmount(k.BudgetUSD)}}); err != nil {
			return err
		}
		if k.BudgetUSD == 0 {
			fmt.Fprintf(stdout, "Virtual key %s (%q) now has no monthly budget.\n", k.Prefix, k.Name)
		} else {
			fmt.Fprintf(stdout, "Virtual key %s (%q) now has a monthly budget of %s.\n", k.Prefix, k.Name,
				budget.FormatUSD(k.BudgetUSD))
		}
		return nil
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

// checkBudget validates a --budget-usd value.
func checkBudget(usd float64) error {
	if math.IsNaN(usd) || math.IsInf(usd, 0) || usd < 0 {
		return errors.New("--budget-usd must be an amount of 0 or more, such as 50 or 12.5")
	}
	return nil
}

// formatBudget shows a monthly budget, where 0 means none.
func formatBudget(usd float64) string {
	if usd == 0 {
		return "none"
	}
	return budget.FormatUSD(usd)
}

// formatAmount writes an amount for the audit log, exactly.
func formatAmount(usd float64) string { return strconv.FormatFloat(usd, 'f', -1, 64) }

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
