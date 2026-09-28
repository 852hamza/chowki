package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

const usageUsage = `Usage:
  chowki usage [--from <DATE>] [--to <DATE>] [--by key|model|day] [--config <FILE>]

Reports the requests, tokens, cost and savings of the days from --from to
--to, both included, in UTC. By default it reports this month so far.

Flags:
  --from    the first day, such as 2026-09-01
  --to      the last day, such as 2026-09-30
  --by      also break the totals down by key, model or day
  --config  the configuration file; by default $CHOWKI_CONFIG, or chowki.yaml
`

func runUsage(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki usage", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfig(), "")
	fromFlag := flags.String("from", "", "")
	toFlag := flags.String("to", "", "")
	by := flags.String("by", "", "")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usageUsage)
			return exitOK
		}
		fmt.Fprintf(stderr, "chowki usage: %v\n\n%s", err, usageUsage)
		return exitUsage
	}
	from, to, err := usageRange(time.Now(), *fromFlag, *toFlag)
	if err == nil && *by != "" && !slices.Contains([]string{store.ByKey, store.ByModel, store.ByDay}, *by) {
		err = errors.New("--by must be key, model or day")
	}
	if err != nil || flags.NArg() > 0 {
		if err != nil {
			fmt.Fprintf(stderr, "chowki usage: %v\n", err)
		} else {
			fmt.Fprint(stderr, usageUsage)
		}
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return withStore(ctx, *configPath, stderr, "chowki usage", func(st store.Store) error {
		t, err := st.Totals(ctx, from, to)
		if err != nil {
			return err
		}
		var groups []store.Group
		if *by != "" {
			if groups, err = st.Breakdown(ctx, *by, from, to); err != nil {
				return err
			}
		}
		return printUsage(stdout, from, to, t, *by, groups)
	})
}

// usageRange returns [from, to) for the days that the flags name, both
// included: by default, from the first of this month to today, in UTC.
func usageRange(now time.Time, fromFlag, toFlag string) (from, to time.Time, err error) {
	now = now.UTC()
	from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	for name, v := range map[string]string{"from": fromFlag, "to": toFlag} {
		if v == "" {
			continue
		}
		d, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return from, to, fmt.Errorf("--%s must be a date such as 2026-09-01", name)
		}
		if name == "to" {
			to = d.AddDate(0, 0, 1)
		} else {
			from = d
		}
	}
	if !from.Before(to) {
		return from, to, errors.New("--from must be on or before --to")
	}
	return from, to, nil
}

func printUsage(w io.Writer, from, to time.Time, t store.Totals, by string, groups []store.Group) error {
	fmt.Fprintf(w, "Usage from %s to %s, in UTC\n\n", from.Format(time.DateOnly),
		to.AddDate(0, 0, -1).Format(time.DateOnly))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	requests := thousands(t.Requests)
	if t.Errors > 0 || t.Unpriced > 0 {
		requests += fmt.Sprintf(" (%s, %s unpriced)", countOf(t.Errors, "error", "errors"), thousands(t.Unpriced))
	}
	fmt.Fprintf(tw, "Requests\t%s\n", requests)
	fmt.Fprintf(tw, "Cost\t%s\n", usd(t.CostUSD))
	var saved float64
	var methods []string
	for _, m := range slices.Sorted(maps.Keys(t.SavingsUSD)) {
		saved += t.SavingsUSD[m]
		methods = append(methods, strings.ReplaceAll(m, "_", " ")+" "+usd(t.SavingsUSD[m]))
	}
	savings := usd(saved)
	if len(methods) > 0 {
		savings += ": " + strings.Join(methods, ", ")
	}
	fmt.Fprintf(tw, "Savings\t%s\n", savings)
	fmt.Fprintf(tw, "Tokens\tinput %s, output %s, cache read %s, cache write %s, reasoning %s\n",
		thousands(t.Tokens.Input), thousands(t.Tokens.Output), thousands(t.Tokens.CacheRead),
		thousands(t.Tokens.CacheWrite), thousands(t.Tokens.Reasoning))
	cache := countOf(t.CacheHits, "hit", "hits") + ", " + countOf(t.CacheMisses, "miss", "misses")
	if n := t.CacheHits + t.CacheMisses; n > 0 {
		cache += fmt.Sprintf(" (%.0f%% hits)", 100*float64(t.CacheHits)/float64(n))
	}
	fmt.Fprintf(tw, "Exact cache\t%s\n", cache)
	redactions := "none"
	if len(t.Redactions) > 0 {
		var found []string
		for _, typ := range slices.Sorted(maps.Keys(t.Redactions)) {
			found = append(found, typ+" "+thousands(t.Redactions[typ]))
		}
		redactions = strings.Join(found, ", ")
	}
	fmt.Fprintf(tw, "Redactions\t%s\n", redactions)
	if err := tw.Flush(); err != nil {
		return err
	}
	if by == "" {
		return nil
	}
	fmt.Fprintln(w)
	if len(groups) == 0 {
		_, err := fmt.Fprintln(w, "No requests in these days.")
		return err
	}
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\tREQUESTS\tCOST\tSAVINGS\tINPUT TOKENS\tOUTPUT TOKENS\n", strings.ToUpper(by))
	for _, g := range groups {
		id := g.ID
		switch {
		case by == store.ByKey && g.Label != "":
			id = g.Label + " (" + g.ID + ")"
		case id == "":
			id = "none" // rejected before a key or a model was known
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", id, thousands(g.Requests), usd(g.CostUSD),
			usd(g.SavingsUSD), thousands(g.InputTokens), thousands(g.OutputTokens))
	}
	return tw.Flush()
}

// usd formats dollars with cents, and an amount below a cent to eight
// decimals, as the cost header does: small tests cost that little.
func usd(v float64) string {
	if v > 0 && v < 0.005 {
		return "$" + strings.TrimRight(fmt.Sprintf("%.8f", v), "0")
	}
	return fmt.Sprintf("$%.2f", v)
}

// countOf returns n and the noun, one or many as n asks.
func countOf(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return thousands(n) + " " + many
}

// thousands formats n with commas between groups of three digits.
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	sign := ""
	if n < 0 {
		sign, s = "-", s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + s
}
