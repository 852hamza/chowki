package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/852hamza/chowki/internal/budget"
	"github.com/852hamza/chowki/internal/store"
)

const projectUsage = `Usage:
  chowki project list [--config <FILE>]
  chowki project update [--config <FILE>] --budget-usd <USD> <NAME>

A project groups virtual keys; "chowki key create --project" creates it.
--budget-usd sets a monthly budget in US dollars that all the project's keys
share; 0 means no budget.
`

func runProject(args []string, stdout, stderr io.Writer) int {
	return runSubcommand("project", projectUsage, map[string]subcommand{
		"list":   projectList,
		"update": projectUpdate,
	}, args, stdout, stderr)
}

func projectList(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki project list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return exitUsage
	}
	return withStore(ctx, *configPath, stderr, "chowki project list", func(st store.Store) error {
		projects, err := st.ListProjects(ctx)
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			fmt.Fprintln(stdout, "No projects yet. Creating a key creates its project: chowki key create --name <NAME> --project <PROJECT>")
			return nil
		}
		keys, err := st.ListKeys(ctx)
		if err != nil {
			return err
		}
		active := map[int64]int{}
		for _, k := range keys {
			if !k.Revoked() {
				active[k.ProjectID]++
			}
		}
		period := store.Period(time.Now())
		spend, err := st.SpendByKey(ctx, period)
		if err != nil {
			return err
		}
		spent := map[int64]float64{}
		for _, s := range spend {
			spent[s.ProjectID] += s.USD
		}
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "NAME\tACTIVE KEYS\tSPENT %s\tBUDGET\n", period)
		for _, p := range projects {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", p.Name, active[p.ID], budget.FormatUSD(spent[p.ID]),
				formatBudget(p.BudgetUSD))
		}
		return tw.Flush()
	})
}

func projectUpdate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki project update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
	budgetUSD := flags.Float64("budget-usd", 0, "monthly budget in US dollars; 0 removes it")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		if err == nil {
			fmt.Fprint(stderr, projectUsage)
		}
		return exitUsage
	}
	var u store.ProjectUpdate
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "budget-usd" {
			u.BudgetUSD = budgetUSD
		}
	})
	if u.BudgetUSD == nil {
		fmt.Fprint(stderr, "chowki project update: nothing to change; set --budget-usd\n")
		return exitUsage
	}
	if err := checkBudget(*u.BudgetUSD); err != nil {
		fmt.Fprintf(stderr, "chowki project update: %v\n", err)
		return exitUsage
	}
	name := flags.Arg(0)
	return withStore(ctx, *configPath, stderr, "chowki project update", func(st store.Store) error {
		p, err := st.UpdateProject(ctx, name, u)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no project is named %q; see chowki project list", name)
		}
		if err != nil {
			return err
		}
		if err := st.AddAudit(ctx, store.AuditEvent{Time: time.Now().UTC(), Actor: "cli", Action: "project.update",
			Target: p.Name, Details: map[string]string{"monthly_budget_usd": formatAmount(p.BudgetUSD)}}); err != nil {
			return err
		}
		if p.BudgetUSD == 0 {
			fmt.Fprintf(stdout, "Project %q now has no monthly budget.\n", p.Name)
		} else {
			fmt.Fprintf(stdout, "Project %q now has a monthly budget of %s, shared by its keys.\n", p.Name,
				budget.FormatUSD(p.BudgetUSD))
		}
		return nil
	})
}
