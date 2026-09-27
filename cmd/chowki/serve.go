package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/852hamza/chowki/internal/buildinfo"
	"github.com/852hamza/chowki/internal/catalog"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/netguard"
	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/providers"
	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/server"
	"github.com/852hamza/chowki/internal/store"
)

func runServe(args []string, _, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfig, "configuration file")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, *configPath, nil, stderr); err != nil {
		fmt.Fprintln(stderr, "chowki serve:", err)
		return exitError
	}
	return exitOK
}

// serve runs the gateway until ctx ends. It listens on ln, or on the
// configured address when ln is nil, and writes JSON logs to logOut.
func serve(ctx context.Context, configPath string, ln net.Listener, logOut io.Writer) error {
	env, err := config.DotEnv(".env")
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath, env)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(logOut, &slog.HandlerOptions{Level: logLevel(cfg.Log.Level)}))
	if len(cfg.Providers) == 0 {
		return errors.New("the configuration has no providers; add one under providers")
	}
	// The master key isn't used by M1 features yet, but a missing or
	// exposed key should stop the gateway now, not when it's first needed.
	if _, err := secretbox.LoadKey(cfg.Security.MasterKeyFile, env); err != nil {
		return err
	}
	cat, err := catalog.Default()
	if err != nil {
		return err
	}
	ps, err := providers.New(cfg.Providers, netguard.Policy{AllowPrivate: cfg.Security.AllowPrivateUpstreams})
	if err != nil {
		return err
	}
	names := make([]string, 0, len(ps))
	for name := range ps {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if env := ps[name].MissingKey(); env != "" {
			logger.Warn("provider key isn't set; requests to this provider fail until it is",
				"provider", name, "variable", env)
		}
	}

	st, err := store.OpenSQLite(ctx, cfg.Storage.DSN)
	if err != nil {
		return err
	}
	requests := store.NewRequestLog(st, logger)
	retentionCtx, stopRetention := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { store.RunRetention(retentionCtx, st, cfg.RetentionDays, 24*time.Hour, logger) })
	// Shut down in order: requests are done when server.Run returns; then
	// the last records are saved, and only then the database closes.
	defer func() {
		stopRetention()
		wg.Wait()
		requests.Close()
		if err := st.Close(); err != nil {
			logger.Error("close database", "error", err)
		}
	}()

	if ln == nil {
		if ln, err = (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Server.Listen); err != nil {
			return err
		}
	}
	gw := &pipeline.Gateway{
		Store: st, Requests: requests, Providers: ps, Catalog: cat, Logger: logger,
		MaxBody: int64(cfg.Server.MaxBodyMB) << 20, Timeout: cfg.Server.UpstreamTimeout,
	}
	logger.Info("chowki started", "version", buildinfo.Version(), "providers", names, "priced_models", cat.Len())
	return server.Run(ctx, ln, server.Routes(gw), logger)
}

func logLevel(name string) slog.Level {
	switch name {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}
