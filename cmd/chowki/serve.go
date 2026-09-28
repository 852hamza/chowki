package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/852hamza/chowki/internal/admin"
	"github.com/852hamza/chowki/internal/budget"
	"github.com/852hamza/chowki/internal/buildinfo"
	"github.com/852hamza/chowki/internal/cache"
	"github.com/852hamza/chowki/internal/catalog"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/metrics"
	"github.com/852hamza/chowki/internal/netguard"
	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/promptcache"
	"github.com/852hamza/chowki/internal/providerkeys"
	"github.com/852hamza/chowki/internal/providers"
	"github.com/852hamza/chowki/internal/ratelimit"
	"github.com/852hamza/chowki/internal/redact"
	"github.com/852hamza/chowki/internal/router"
	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/server"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/translate"
	"github.com/852hamza/chowki/internal/web"
)

const serveUsage = `Usage:
  chowki serve [--config <FILE>]

Runs the gateway. It listens on server.listen and relays requests to the
providers, until it gets an interrupt or SIGTERM; then it stops taking
requests and waits up to 30 seconds for those in flight. It reads .env from
the current folder, and writes its log to stderr, in JSON.

Flags:
  --config  the configuration file; by default $CHOWKI_CONFIG, or chowki.yaml
`

func runServe(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki serve", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfig(), "")
	if code, ok := parseCommand(flags, args, serveUsage, stdout, stderr); !ok {
		return code
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
	masterKey, err := secretbox.LoadKey(cfg.Security.MasterKeyFile, env)
	if err != nil {
		return err
	}
	cacheBox, err := secretbox.New(masterKey, "cache")
	if err != nil {
		return err
	}
	redactionKey, err := secretbox.DeriveKey(masterKey, "redaction")
	if err != nil {
		return err
	}
	cat, err := catalog.Default()
	if err != nil {
		return err
	}
	keys, err := providerkeys.New(masterKey)
	if err != nil {
		return err
	}

	// Listen before starting anything else, so a taken port fails fast.
	if ln == nil {
		if ln, err = (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Server.Listen); err != nil {
			return fmt.Errorf("%w; set server.listen or %s to use another address", err, "CHOWKI_SERVER_LISTEN")
		}
	}
	defer func() { _ = ln.Close() }() // no-op once the server has closed it

	// Opening the database applies pending migrations; the log says so, as
	// an upgrade's record.
	before, _ := store.InspectSQLite(ctx, cfg.Storage.DSN) // a new database has nothing to upgrade
	st, err := store.OpenSQLite(ctx, cfg.Storage.DSN)
	if err != nil {
		return err
	}
	if before.Path != "" && before.Version > 0 && before.Version < before.Latest {
		logger.Info("upgraded the database", "path", before.Path, "from_schema", before.Version,
			"to_schema", before.Latest)
	}
	ps, names, err := setUpProviders(ctx, cfg, st, keys, logger)
	if err != nil {
		_ = st.Close() // the setup error is the one to report
		return err
	}
	routes, err := router.New(ps, cat, cfg.Aliases)
	if err != nil {
		_ = st.Close()
		return err
	}
	budgets, err := budget.Load(ctx, st, time.Now(), logger)
	if err != nil {
		_ = st.Close() // the load error is the one to report
		return err
	}
	responses, err := cache.New(ctx, st, cacheBox, cache.Options{Default: cfg.Defaults.Cache,
		TTL: cfg.Defaults.CacheTTL, MaxBytes: int64(cfg.Storage.CacheMaxMB) << 20}, logger)
	if err != nil {
		_ = st.Close()
		return err
	}
	requests := store.NewRequestLog(st, logger)
	retentionCtx, stopRetention := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { store.RunRetention(retentionCtx, st, cfg.RetentionDays, 24*time.Hour, logger) })
	// Shut down in order: requests are done when server.Run returns; then
	// the last records and cache entries are saved, and only then the
	// database closes.
	defer func() {
		stopRetention()
		wg.Wait()
		requests.Close()
		responses.Close()
		if err := st.Close(); err != nil {
			logger.Error("close database", "error", err)
		}
	}()

	gw := &pipeline.Gateway{
		Store: st, Requests: requests, Router: routes, Metrics: metrics.New(),
		Redactor: redact.New(redactionKey, cfg.Defaults.Redaction),
		Cache:    responses, Limits: ratelimit.New(), Budgets: budgets,
		Providers: ps, Catalog: cat, Memory: translate.NewMemory(), Logger: logger,
		MaxBody: int64(cfg.Server.MaxBodyMB) << 20, Timeout: cfg.Server.UpstreamTimeout,
	}
	if cfg.Defaults.PromptCache == "auto" {
		gw.PromptCache = promptcache.New()
	}
	logger.Info("chowki started", "version", buildinfo.Version(), "providers", names, "priced_models", cat.Len())
	api := &admin.API{Store: st, Logger: logger}
	ui := &web.UI{Store: st, Logger: logger}
	return server.Run(ctx, ln, server.Routes(gw, api.Handler(), ui.Handler()), logger, server.Options{
		ReadTimeout: cfg.Server.ReadTimeout, CertFile: cfg.Server.TLSCertFile, KeyFile: cfg.Server.TLSKeyFile})
}

// setUpProviders gives the providers their stored keys where the
// environment gives none, and returns them with their names in order. A
// provider without a key gets a warning: its requests fail until it has
// one.
func setUpProviders(ctx context.Context, cfg *config.Config, st store.Store, keys *providerkeys.Keys,
	logger *slog.Logger) (map[string]*providers.Provider, []string, error) {
	used, errs, err := providerkeys.Apply(ctx, st, keys, cfg.Providers)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range errs {
		logger.Warn("stored provider key can't be used", "error", e)
	}
	if len(used) > 0 {
		logger.Info("using stored provider keys", "providers", used)
	}
	ps, err := providers.New(cfg.Providers, netguard.Policy{AllowPrivate: cfg.Security.AllowPrivateUpstreams})
	if err != nil {
		return nil, nil, err
	}
	names := slices.Sorted(maps.Keys(ps))
	for _, name := range names {
		if env := ps[name].MissingKey(); env != "" {
			logger.Warn("provider key isn't set; requests to this provider fail until it is",
				"provider", name, "variable", env)
		}
	}
	return ps, names, nil
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
