package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/usage"
)

// ShutdownTimeout is how long a shutdown waits for requests in flight.
const ShutdownTimeout = 30 * time.Second

// Routes returns the gateway's HTTP handler: the API endpoints, health
// checks, metrics, and admin, the admin API, and ui, the dashboard, when
// they aren't nil. Unknown paths get a 404 in the error format of the API
// family their path belongs to.
func Routes(gw *pipeline.Gateway, admin, ui http.Handler) http.Handler {
	mux := http.NewServeMux()
	for _, ep := range pipeline.Endpoints {
		mux.Handle("POST "+ep.Path, gw.Handler(ep))
	}
	mux.Handle("POST /gemini/v1beta/models/", gw.Gemini())
	mux.Handle("GET /v1/models", gw.Models())
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("GET /readyz", readyz(gw.Store))
	mux.Handle("GET /metrics", gw.Metrics.Handler())
	if admin != nil {
		mux.Handle("/admin/", admin)
	}
	if ui != nil {
		mux.Handle("/ui", ui)
		mux.Handle("/ui/", ui)
	}
	mux.Handle("/anthropic/", gw.NotFound(usage.Anthropic))
	mux.Handle("/gemini/", gw.NotFound(usage.Gemini))
	mux.Handle("/", gw.NotFound(usage.OpenAI))
	return mux
}

// healthz answers while the process runs, for liveness probes.
func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz answers 200 when the gateway can serve requests, and 503 when its
// database doesn't answer, for readiness probes and load balancers.
func readyz(db interface{ Ping(context.Context) error }) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready",
				"reason": "the database doesn't answer"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the client may be gone; nothing to do then
}

// Run serves h on ln until ctx ends, then shuts down gracefully: it stops
// accepting connections and waits up to ShutdownTimeout for requests in
// flight, then closes the rest.
func Run(ctx context.Context, ln net.Listener, h http.Handler, logger *slog.Logger) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		// No write timeout: a stream may last as long as the upstream
		// timeout allows.
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	logger.Info("listening", "addr", ln.Addr().String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	err := srv.Shutdown(shutdownCtx)
	if errors.Is(err, context.DeadlineExceeded) {
		logger.Warn("requests still running after the shutdown timeout; closing them")
		err = srv.Close()
	}
	if serveErr := <-errc; !errors.Is(serveErr, http.ErrServerClosed) && err == nil {
		err = serveErr
	}
	return err
}
