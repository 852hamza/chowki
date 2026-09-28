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

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/budget"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/store"
)

const keyUsage = `Usage:
  chowki key create --name <NAME> [--project <PROJECT>] [SETTINGS] [--config <FILE>]
  chowki key list [--config <FILE>]
  chowki key update [--config <FILE>] SETTINGS <PREFIX>
  chowki key revoke [--config <FILE>] <PREFIX>

SETTINGS are one or more of these:
  --budget-usd <USD>  monthly budget in US dollars; 0 means none
  --rpm <N>           limit of requests per minute; 0 means none
  --tpm <N>           limit of input and output tokens per minute; 0 means none
  --cache <MODE>      exact cache: exact, off, or default to follow chowki.yaml
  --redaction <MODE>  secrets and personal data in prompts: mask, block, alert,
                      off, or default to follow chowki.yaml
  --models <LIST>     comma-separated models, aliases and patterns such as
                      openai/* that the key may use; all allows every model

A virtual key is shown once, when you create it. Update or revoke a key by
its prefix, the first 12 characters, as "chowki key list" shows them.
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
	configPath := flags.String("config", defaultConfig(), "configuration file")
	name := flags.String("name", "", "who or what uses the key, such as alice or ci-bot (required)")
	project := flags.String("project", "default", "project that the key belongs to")
	settings := addSettingFlags(flags)
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return exitUsage
	}
	for label, v := range map[string]string{"--name": *name, "--project": *project} {
		if err := store.CheckName(v); err != nil {
			fmt.Fprintf(stderr, "chowki key create: %s %v\n", label, err)
			return exitUsage
		}
	}
	if err := settings.all().Validate(); err != nil {
		fmt.Fprintf(stderr, "chowki key create: %v\n", err)
		return exitUsage
	}
	return withStore(ctx, *configPath, stderr, "chowki key create", func(st store.Store) error {
		now := time.Now().UTC()
		key, k, err := auth.Create(ctx, st, *project, store.Key{Name: *name, BudgetUSD: *settings.budgetUSD,
			RPM: *settings.rpm, TPM: *settings.tpm, CacheMode: settings.cacheMode(),
			RedactionMode: settings.redactionMode(), AllowedModels: settings.models()}, now)
		if err != nil {
			return err
		}
		details := map[string]string{"name": k.Name, "project": k.Project}
		for name, v := range settingDetails(k) {
			if v != "0" && v != "default" { // settings that a new key doesn't have aren't news
				details[name] = v
			}
		}
		if err := st.AddAudit(ctx, store.AuditEvent{Time: now, Actor: "cli", Action: "key.create", Target: k.Prefix,
			Details: details}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Created virtual key %q in project %q:\n\n  %s\n\n"+
			"Copy it now. Chowki stores only a hash of it and can't show it again.\n", k.Name, k.Project, key)
		if k.BudgetUSD > 0 || k.RPM > 0 || k.TPM > 0 || k.CacheMode != "" || k.RedactionMode != "" ||
			len(k.AllowedModels) > 0 {
			fmt.Fprintln(stdout, "\nSettings:")
			printSettings(stdout, k)
		}
		return nil
	})
}

func keyList(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki key list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
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
		fmt.Fprintf(tw, "PREFIX\tNAME\tPROJECT\tSPENT %s\tBUDGET\tRPM\tTPM\tCACHE\tREDACTION\tCREATED\tSTATUS\n",
			period)
		for _, k := range keys {
			status := "active"
			if k.Revoked() {
				status = "revoked " + formatTime(k.RevokedAt)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", k.Prefix, k.Name, k.Project,
				budget.FormatUSD(spent[k.ID]), formatBudget(k.BudgetUSD), formatLimit(k.RPM), formatLimit(k.TPM),
				orDefault(k.CacheMode), orDefault(k.RedactionMode), formatTime(k.CreatedAt), status)
		}
		return tw.Flush()
	})
}

func keyUpdate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki key update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
	settings := addSettingFlags(flags)
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		if err == nil {
			fmt.Fprint(stderr, keyUsage)
		}
		return exitUsage
	}
	u := settings.update(flags)
	if u == (store.KeyUpdate{}) {
		fmt.Fprint(stderr, "chowki key update: nothing to change; set --budget-usd, --rpm, --tpm, --cache, "+
			"--redaction or --models\n")
		return exitUsage
	}
	if err := u.Validate(); err != nil {
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
		details := settingDetails(k)
		details["name"], details["project"] = k.Name, k.Project
		if err := st.AddAudit(ctx, store.AuditEvent{Time: time.Now().UTC(), Actor: "cli", Action: "key.update",
			Target: k.Prefix, Details: details}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Updated the settings of virtual key %s (%q):\n", k.Prefix, k.Name)
		printSettings(stdout, k)
		return nil
	})
}

func keyRevoke(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki key revoke", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
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
	return withConfigStore(ctx, configPath, stderr, command, func(_ *config.Config, _ func(string) (string, bool),
		st store.Store) error {
		return fn(st)
	})
}

// withConfigStore is withStore for commands that need the configuration
// and the environment too.
func withConfigStore(ctx context.Context, configPath string, stderr io.Writer, command string,
	fn func(cfg *config.Config, env func(string) (string, bool), st store.Store) error) int {
	err := func() error {
		cfg, env, err := loadConfig(configPath)
		if err != nil {
			return err
		}
		st, err := store.OpenSQLite(ctx, cfg.Storage.DSN)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }() // read errors surface through fn
		return fn(cfg, env, st)
	}()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", command, err)
		return exitError
	}
	return exitOK
}

// settingFlags are the flags that set a key's limits and modes.
type settingFlags struct {
	budgetUSD                   *float64
	rpm, tpm                    *int64
	cache, redaction, modelList *string
}

func addSettingFlags(flags *flag.FlagSet) settingFlags {
	return settingFlags{
		budgetUSD: flags.Float64("budget-usd", 0, "monthly budget in US dollars; 0 means none"),
		rpm:       flags.Int64("rpm", 0, "limit of requests per minute; 0 means none"),
		tpm:       flags.Int64("tpm", 0, "limit of input and output tokens per minute; 0 means none"),
		cache:     flags.String("cache", "default", "exact cache: exact, off, or default to follow chowki.yaml"),
		redaction: flags.String("redaction", "default", "mask, block, alert, off, or default to follow chowki.yaml"),
		modelList: flags.String("models", "all", "comma-separated models, aliases and patterns that the key may use"),
	}
}

// update returns the settings that flags set, and leaves the others out.
func (s settingFlags) update(flags *flag.FlagSet) store.KeyUpdate {
	var u store.KeyUpdate
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "budget-usd":
			u.BudgetUSD = s.budgetUSD
		case "rpm":
			u.RPM = s.rpm
		case "tpm":
			u.TPM = s.tpm
		case "cache":
			mode := s.cacheMode()
			u.CacheMode = &mode
		case "redaction":
			mode := s.redactionMode()
			u.RedactionMode = &mode
		case "models":
			models := s.models()
			u.AllowedModels = &models
		}
	})
	return u
}

// cacheMode and redactionMode are the key's modes as the store keeps
// them: "" follows the configuration.
func (s settingFlags) cacheMode() string     { return storedMode(*s.cache) }
func (s settingFlags) redactionMode() string { return storedMode(*s.redaction) }

// models is the key's model allowlist: empty allows every model.
func (s settingFlags) models() []string {
	if strings.TrimSpace(*s.modelList) == "all" {
		return nil
	}
	var out []string
	for _, m := range strings.Split(*s.modelList, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

func storedMode(flag string) string {
	if flag == "default" {
		return ""
	}
	return flag
}

// all returns every setting of the flags, for a new key.
func (s settingFlags) all() store.KeyUpdate {
	cacheMode, redactionMode, models := s.cacheMode(), s.redactionMode(), s.models()
	return store.KeyUpdate{BudgetUSD: s.budgetUSD, RPM: s.rpm, TPM: s.tpm, CacheMode: &cacheMode,
		RedactionMode: &redactionMode, AllowedModels: &models}
}

// printSettings shows the settings of key k, indented.
func printSettings(w io.Writer, k store.Key) {
	models := "all"
	if len(k.AllowedModels) > 0 {
		models = strings.Join(k.AllowedModels, ", ")
	}
	fmt.Fprintf(w, "  Monthly budget:       %s\n  Requests per minute:  %s\n  Tokens per minute:    %s\n"+
		"  Exact cache:          %s\n  Redaction:            %s\n  Models:               %s\n",
		formatBudget(k.BudgetUSD), formatLimit(k.RPM), formatLimit(k.TPM), orDefault(k.CacheMode),
		orDefault(k.RedactionMode), models)
}

// settingDetails returns the settings of key k for the audit log, exactly;
// "0" means no limit.
func settingDetails(k store.Key) map[string]string {
	return map[string]string{"monthly_budget_usd": formatAmount(k.BudgetUSD),
		"rpm": strconv.FormatInt(k.RPM, 10), "tpm": strconv.FormatInt(k.TPM, 10),
		"cache": orDefault(k.CacheMode), "redaction": orDefault(k.RedactionMode),
		"models": strings.Join(k.AllowedModels, ",")}
}

// orDefault shows a key's mode, where "" follows the configuration.
func orDefault(mode string) string {
	if mode == "" {
		return "default"
	}
	return mode
}

// formatLimit shows a limit per minute, where 0 means none.
func formatLimit(n int64) string {
	if n == 0 {
		return "none"
	}
	return strconv.FormatInt(n, 10)
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

func formatTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }
