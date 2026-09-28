package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"

	"github.com/852hamza/chowki/internal/buildinfo"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/store"
)

// configEnv names the environment variable that sets the configuration
// file of every command, as the container image does.
const configEnv = "CHOWKI_CONFIG"

// defaultConfig returns the configuration file that commands use without
// --config: $CHOWKI_CONFIG, or chowki.yaml.
func defaultConfig() string {
	if path := os.Getenv(configEnv); path != "" {
		return path
	}
	return "chowki.yaml"
}

const initUsage = `Usage:
  chowki init [--config <FILE>]

Creates what the gateway needs, and keeps what exists: the configuration
file, a master key that only you can read, where security.master_key_file
says, and the database. Run it in the folder where you run chowki serve.

Flags:
  --config  the configuration file to create or use; by default
            $CHOWKI_CONFIG, or chowki.yaml
`

func runInit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki init", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfig(), "")
	if code, ok := parseCommand(flags, args, initUsage, stdout, stderr); !ok {
		return code
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := initialize(ctx, *configPath, stdout); err != nil {
		fmt.Fprintln(stderr, "chowki init:", err)
		return exitError
	}
	return exitOK
}

// initialize creates whatever is missing of the configuration file, the
// master key and the database. It never replaces an existing file.
func initialize(ctx context.Context, configPath string, out io.Writer) error {
	created, err := createFile(configPath, config.Starter(buildinfo.Project().DocsURL()))
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(out, "Created %s.\n", configPath)
	} else {
		fmt.Fprintf(out, "Using the existing %s.\n", configPath)
	}

	env, err := config.DotEnv(".env")
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath, env)
	if err != nil {
		return err
	}

	keyFile := cfg.Security.MasterKeyFile
	switch _, statErr := os.Stat(keyFile); {
	case isSet(env, secretbox.MasterKeyEnv):
		fmt.Fprintf(out, "Using the master key from %s.\n", secretbox.MasterKeyEnv)
	case errors.Is(statErr, fs.ErrNotExist):
		if err := secretbox.CreateKeyFile(keyFile); err != nil {
			return err
		}
		fmt.Fprintf(out, "Created the master key in %s. Keep it private and back it up.\n", keyFile)
	default:
		fmt.Fprintf(out, "Using the existing master key in %s.\n", keyFile)
	}
	if _, err := secretbox.LoadKey(keyFile, env); err != nil {
		return err
	}

	st, err := store.OpenSQLite(ctx, cfg.Storage.DSN)
	if err != nil {
		return err
	}
	if err := st.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	fmt.Fprintf(out, "The database is ready: %s.\n", cfg.Storage.DSN)
	fmt.Fprint(out, "\nNext steps:\n"+
		"  1. Put your provider keys in .env or the environment, as named by api_key_env.\n"+
		"  2. Create a virtual key: chowki key create --name <NAME>\n"+
		"  3. Start the gateway:    chowki serve\n")
	return nil
}

// createFile writes data to a new file readable only by the current user,
// and reports false without writing if the file exists.
func createFile(path string, data []byte) (bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return true, err
}

func isSet(env func(string) (string, bool), name string) bool {
	v, ok := env(name)
	return ok && v != ""
}
