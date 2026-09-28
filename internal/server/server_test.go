package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
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
