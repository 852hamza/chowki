package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/store"
)

const backupUsage = `Usage:
  chowki backup [--config <FILE>] <BACKUP>

Writes a consistent copy of the database to BACKUP, a new file that only
you can read, or to stdout when BACKUP is -. It works while chowki serve
runs, and doesn't upgrade the database, so back up before you upgrade
Chowki.

The copy holds stored provider keys and cached answers, which open only
with the master key: keep the two together, and apart from the gateway.

Flags:
  --config  the configuration file; by default $CHOWKI_CONFIG, or chowki.yaml
`

const restoreUsage = `Usage:
  chowki restore [--config <FILE>] <BACKUP>

Replaces the database with BACKUP, a file that chowki backup wrote, or
stdin when BACKUP is -, after checking it. Stop chowki serve first, and
start it again after. The replaced database stays next to it, as
<NAME>.before-restore-<TIME>.

Flags:
  --config  the configuration file; by default $CHOWKI_CONFIG, or chowki.yaml
`

func runBackup(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki backup", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfig(), "")
	file, code, ok := parseOneArgument(flags, args, backupUsage, stdout, stderr)
	if !ok {
		return code
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cfg, env, err := loadConfig(*configPath)
	var size int64
	if err == nil {
		size, err = backup(ctx, cfg.Storage.DSN, file, stdout)
	}
	if err != nil {
		fmt.Fprintln(stderr, "chowki backup:", err)
		return exitError
	}
	masterKey := cfg.Security.MasterKeyFile
	if v, ok := env(secretbox.MasterKeyEnv); ok && v != "" {
		masterKey = "the value of " + secretbox.MasterKeyEnv
	}
	note := stdout
	if file == "-" {
		note, file = stderr, "stdout" // the backup itself went to stdout
	}
	fmt.Fprintf(note, "Backed up the database to %s (%.1f MiB).\nKeep it with the master key, %s: the stored "+
		"provider keys and cached answers in it open only with that key.\n", file, float64(size)/(1<<20), masterKey)
	return exitOK
}

// backup writes the backup to file, or to stdout when file is -, through
// a temporary file, and returns its size.
func backup(ctx context.Context, dsn, file string, stdout io.Writer) (int64, error) {
	if file != "-" {
		if err := store.BackupSQLite(ctx, dsn, file); err != nil {
			return 0, err
		}
		info, err := os.Stat(file)
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	tmp, err := os.CreateTemp("", "chowki-backup-*.db")
	if err != nil {
		return 0, err
	}
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmp.Name()) }()
	// BackupSQLite writes only to a new file; CreateTemp made it private.
	if err := os.Remove(tmp.Name()); err != nil {
		return 0, err
	}
	if err := store.BackupSQLite(ctx, dsn, tmp.Name()); err != nil {
		return 0, err
	}
	f, err := os.Open(tmp.Name())
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	return io.Copy(stdout, f)
}

func runRestore(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki restore", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfig(), "")
	file, code, ok := parseOneArgument(flags, args, restoreUsage, stdout, stderr)
	if !ok {
		return code
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	from := file
	if file == "-" {
		from = "stdin"
	}
	cfg, _, err := loadConfig(*configPath)
	var kept string
	if err == nil && file == "-" {
		file, err = saveStdin()
		defer func() { _ = os.Remove(file) }()
	}
	if err == nil {
		kept, err = store.RestoreSQLite(ctx, cfg.Storage.DSN, file, time.Now())
	}
	if err != nil {
		fmt.Fprintln(stderr, "chowki restore:", err)
		return exitError
	}
	fmt.Fprintf(stdout, "Restored the database from %s.\n", from)
	if kept != "" {
		fmt.Fprintf(stdout, "The database that it replaced is in %s.\n", kept)
	}
	fmt.Fprintln(stdout, "Check it with chowki doctor, then start chowki serve.")
	return exitOK
}

// saveStdin saves stdin, a backup, to a private temporary file.
func saveStdin() (string, error) {
	tmp, err := os.CreateTemp("", "chowki-restore-*.db")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, stdin); err != nil {
		_ = tmp.Close()
		return tmp.Name(), fmt.Errorf("read the backup: %w", err)
	}
	return tmp.Name(), tmp.Close()
}

// parseOneArgument parses the flags of a command that takes one argument.
func parseOneArgument(flags *flag.FlagSet, args []string, usage string, stdout, stderr io.Writer) (arg string,
	code int, ok bool) {
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return "", exitOK, false
		}
		fmt.Fprintf(stderr, "%s: %v\n\n%s", flags.Name(), err, usage)
		return "", exitUsage, false
	}
	if flags.NArg() != 1 {
		fmt.Fprint(stderr, usage)
		return "", exitUsage, false
	}
	return flags.Arg(0), exitOK, true
}

// loadConfig loads the configuration and the environment, as chowki serve
// does.
func loadConfig(path string) (*config.Config, func(string) (string, bool), error) {
	env, err := config.DotEnv(".env")
	if err != nil {
		return nil, nil, err
	}
	cfg, err := config.Load(path, env)
	return cfg, env, err
}
