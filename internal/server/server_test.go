package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRunShutsDownGracefully checks that a request in flight finishes
// after shutdown starts, and that Run then returns without an error.
func TestRunShutsDownGracefully(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "done")
	})
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx, ln, h, slog.New(slog.NewTextHandler(&logs, nil))) }()

	body := make(chan string, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ln.Addr().String(), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			body <- err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		body <- string(b)
	}()
	<-started
	cancel() // shut down while the request is running
	time.Sleep(50 * time.Millisecond)
	close(release)

	if got := <-body; got != "done" {
		t.Errorf("request in flight got %q, want done", got)
	}
	if err := <-errc; err != nil {
		t.Errorf("Run() = %v", err)
	}
	if !strings.Contains(logs.String(), "shutting down") {
		t.Errorf("logs = %s", logs.String())
	}
}

type pinger struct{ err error }

func (p pinger) Ping(context.Context) error { return p.err }

func TestHealth(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		status  int
		body    string
	}{
		{"healthz", http.HandlerFunc(healthz), http.StatusOK, `{"status":"ok"}`},
		{"ready", readyz(pinger{}), http.StatusOK, `{"status":"ready"}`},
		{"not ready", readyz(pinger{errors.New("disk gone")}), http.StatusServiceUnavailable,
			`{"reason":"the database doesn't answer","status":"not ready"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
			if w.Code != tt.status || strings.TrimSpace(w.Body.String()) != tt.body ||
				w.Header().Get("Content-Type") != "application/json" {
				t.Errorf("status %d, body %s; want %d and %s", w.Code, w.Body.String(), tt.status, tt.body)
			}
		})
	}
}

func TestRunReportsServeErrors(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close() // Serve fails at once on a closed listener
	if err := Run(t.Context(), ln, http.NotFoundHandler(), slog.New(slog.DiscardHandler)); err == nil {
		t.Error("Run() on a closed listener succeeded")
	}
}
