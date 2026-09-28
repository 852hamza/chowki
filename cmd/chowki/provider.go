package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/providerkeys"
	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/store"
)

const providerUsage = `Usage:
  chowki provider list [--config <FILE>]
  chowki provider set-key [--config <FILE>] <NAME>
  chowki provider remove-key [--config <FILE>] <NAME>

The providers are those of the configuration. Each gets its key from the
environment variable that its api_key_env names, or from a key stored in
the database with set-key, encrypted with the master key. A variable that
is set wins over a stored key.

set-key reads the key from standard input, never from the command line,
which shell history and process lists show:

  read -rs KEY && echo "$KEY" | chowki provider set-key <NAME>

Restart chowki serve after a change, so that it uses the key.
`

// stdin is where set-key reads keys; tests replace it.
var stdin io.Reader = os.Stdin

func runProvider(args []string, stdout, stderr io.Writer) int {
	return runSubcommand("provider", providerUsage, map[string]subcommand{
		"list":       providerList,
		"set-key":    providerSetKey,
		"remove-key": providerRemoveKey,
	}, args, stdout, stderr)
}

func providerList(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki provider list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return exitUsage
	}
	return withConfigStore(ctx, *configPath, stderr, "chowki provider list", func(cfg *config.Config,
		_ func(string) (string, bool), st store.Store) error {
		stored, err := storedKeys(ctx, st)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tTYPE\tBASE URL\tKEY")
		for _, p := range cfg.Providers {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Name, p.Type, p.BaseURL, keySource(p, stored))
			delete(stored, p.Name)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		if len(stored) > 0 {
			names := slices.Sorted(func(yield func(string) bool) {
				for name := range stored {
					if !yield(name) {
						return
					}
				}
			})
			fmt.Fprintf(stdout, "\nStored keys of providers that the configuration doesn't list: %s. Remove them with "+
				"chowki provider remove-key <NAME>.\n", strings.Join(names, ", "))
		}
		return nil
	})
}

// keySource says where a provider's key comes from, as chowki serve
// chooses it.
func keySource(p config.Provider, stored map[string]store.ProviderKey) string {
	s, isStored := stored[p.Name]
	switch {
	case p.APIKey.Reveal() != "" && isStored:
		return p.APIKeyEnv + ", which wins over the stored key"
	case p.APIKey.Reveal() != "":
		return p.APIKeyEnv
	case isStored:
		return "stored " + s.UpdatedAt.UTC().Format("2006-01-02 15:04") + " UTC"
	case p.APIKeyEnv == "":
		return "none needed"
	}
	return "missing: set " + p.APIKeyEnv + ", or run chowki provider set-key " + p.Name
}

func storedKeys(ctx context.Context, st store.Store) (map[string]store.ProviderKey, error) {
	keys, err := st.ProviderKeys(ctx)
	if err != nil {
		return nil, err
	}
	stored := map[string]store.ProviderKey{}
	for _, k := range keys {
		stored[k.Provider] = k
	}
	return stored, nil
}

func providerSetKey(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki provider set-key", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		if err == nil {
			fmt.Fprint(stderr, providerUsage)
		}
		return exitUsage
	}
	name := flags.Arg(0)
	if f, ok := stdin.(*os.File); ok {
		if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			fmt.Fprintf(stderr, "chowki provider set-key: pipe the key in, so that it doesn't show:\n\n"+
				"  read -rs KEY && echo \"$KEY\" | chowki provider set-key %s\n", name)
			return exitUsage
		}
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, providerkeys.MaxLen+2))
	if err != nil {
		fmt.Fprintf(stderr, "chowki provider set-key: read the key: %v\n", err)
		return exitError
	}
	key, err := providerkeys.Clean(string(raw))
	if err != nil {
		fmt.Fprintf(stderr, "chowki provider set-key: %v\n", err)
		return exitUsage
	}
	return withConfigStore(ctx, *configPath, stderr, "chowki provider set-key", func(cfg *config.Config,
		env func(string) (string, bool), st store.Store) error {
		i := slices.IndexFunc(cfg.Providers, func(p config.Provider) bool { return p.Name == name })
		if i < 0 {
			return fmt.Errorf("the configuration has no provider %q; add it under providers first", name)
		}
		masterKey, err := secretbox.LoadKey(cfg.Security.MasterKeyFile, env)
		if err != nil {
			return err
		}
		keys, err := providerkeys.New(masterKey)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := st.SetProviderKey(ctx, store.ProviderKey{Provider: name, Sealed: keys.Seal(name, key),
			UpdatedAt: now}); err != nil {
			return err
		}
		if err := st.AddAudit(ctx, store.AuditEvent{Time: now, Actor: "cli", Action: "provider.set_key",
			Target: name}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Stored the key of %s, encrypted with the master key. Restart chowki serve to use it.\n",
			name)
		if p := cfg.Providers[i]; p.APIKey.Reveal() != "" {
			fmt.Fprintf(stdout, "%s is set too, and wins over the stored key; unset it to use the stored key.\n",
				p.APIKeyEnv)
		}
		return nil
	})
}

func providerRemoveKey(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki provider remove-key", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig(), "configuration file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		if err == nil {
			fmt.Fprint(stderr, providerUsage)
		}
		return exitUsage
	}
	name := flags.Arg(0)
	return withStore(ctx, *configPath, stderr, "chowki provider remove-key", func(st store.Store) error {
		err := st.DeleteProviderKey(ctx, name)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("provider %q has no stored key", name)
		}
		if err != nil {
			return err
		}
		if err := st.AddAudit(ctx, store.AuditEvent{Time: time.Now().UTC(), Actor: "cli",
			Action: "provider.remove_key", Target: name}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Removed the stored key of %s. Restart chowki serve, so that it stops using it.\n", name)
		return nil
	})
}
