package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/testutil"
)

// writeConfig replaces chowki.yaml with a configuration whose only provider
// is the fake at baseURL.
func writeConfig(t *testing.T, baseURL string) {
	t.Helper()
	cfg := "providers:\n  - name: openai\n    type: openai\n    base_url: " + baseURL + "/v1\n" +
		"    api_key_env: TEST_UPSTREAM_KEY\n"
	if err := os.WriteFile("chowki.yaml", []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestServe(t *testing.T) {
	initDir(t)
	fake := testutil.NewOpenAI(t, testutil.Config{APIKey: "sk-EXAMPLE-upstream", Usage: testutil.Usage{Input: 10, Output: 5}})
	writeConfig(t, fake.URL)
	t.Setenv("TEST_UPSTREAM_KEY", "sk-EXAMPLE-upstream")
	key := printedKeyRE.FindStringSubmatch(runOK(t, "key", "create", "--name", "e2e"))[1]

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer // slog's handler serializes writes
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, "chowki.yaml", ln, &logs) }()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+ln.Addr().String()+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-x","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), testutil.DefaultText) {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("serve() = %v", err)
	}
	for _, want := range []string{`"msg":"chowki started"`, `"msg":"listening"`, `"msg":"request"`, `"status":200`,
		`"msg":"shutting down"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs don't contain %s:\n%s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), key) || strings.Contains(logs.String(), "sk-EXAMPLE-upstream") {
		t.Errorf("logs contain a key:\n%s", logs.String())
	}
}

func TestServeErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T)
		want  string
	}{
		{"no providers", func(t *testing.T) {
			if err := os.WriteFile("chowki.yaml", []byte("log:\n  level: info\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "no providers"},
		{"no master key", func(t *testing.T) {
			writeConfig(t, "http://127.0.0.1:1")
			if err := os.Remove(".chowki/master.key"); err != nil {
				t.Fatal(err)
			}
		}, "run chowki init"},
		{"address in use", func(t *testing.T) {
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ln.Close() })
			writeConfig(t, "http://127.0.0.1:1")
			t.Setenv("CHOWKI_SERVER_LISTEN", ln.Addr().String())
		}, "address already in use"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initDir(t)
			tt.setup(t)
			var stdout, stderr bytes.Buffer
			if code := run([]string{"serve"}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("chowki serve = %d, stderr %q; want %q", code, stderr.String(), tt.want)
			}
		})
	}
}
