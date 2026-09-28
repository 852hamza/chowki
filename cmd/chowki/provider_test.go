package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

// withStdin makes set-key read input.
func withStdin(t *testing.T, input string) {
	t.Helper()
	old := stdin
	stdin = strings.NewReader(input)
	t.Cleanup(func() { stdin = old })
}

func TestProviderKeys(t *testing.T) {
	initDir(t)
	for _, v := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY"} {
		t.Setenv(v, "")
	}
	if out := runOK(t, "provider", "list"); !strings.Contains(out, "https://api.openai.com/v1") ||
		!strings.Contains(out, "missing: set OPENAI_API_KEY, or run chowki provider set-key openai") {
		t.Errorf("list before a key:\n%s", out)
	}

	secret := "sk-EXAMPLE-" + "stored-key"
	withStdin(t, secret+"\n")
	if out := runOK(t, "provider", "set-key", "openai"); !strings.Contains(out, "Stored the key of openai") ||
		strings.Contains(out, "wins over") {
		t.Errorf("set-key:\n%s", out)
	}
	if out := runOK(t, "provider", "list"); !strings.Contains(out, "stored "+time.Now().UTC().Format("2006-01-02")) {
		t.Errorf("list after set-key:\n%s", out)
	}
	if countAudit(t, "provider.set_key", "openai") != 1 {
		t.Error("set-key wasn't audited")
	}
	files, _ := filepath.Glob("data/chowki.db*")
	for _, f := range files {
		if data, _ := os.ReadFile(f); bytes.Contains(data, []byte(secret)) {
			t.Errorf("%s holds the key in the clear", f)
		}
	}

	t.Setenv("OPENAI_API_KEY", "sk-EXAMPLE-env")
	if out := runOK(t, "provider", "list"); !strings.Contains(out, "OPENAI_API_KEY, which wins over the stored key") {
		t.Errorf("list with the variable set:\n%s", out)
	}
	withStdin(t, secret)
	if out := runOK(t, "provider", "set-key", "openai"); !strings.Contains(out, "OPENAI_API_KEY is set too") {
		t.Errorf("set-key with the variable set:\n%s", out)
	}

	st, err := store.OpenSQLite(context.Background(), "file:data/chowki.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetProviderKey(context.Background(), store.ProviderKey{Provider: "gone", Sealed: []byte{1},
		UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if out := runOK(t, "provider", "list"); !strings.Contains(out, "doesn't list: gone.") {
		t.Errorf("list with an orphaned key:\n%s", out)
	}

	if out := runOK(t, "provider", "remove-key", "openai"); !strings.Contains(out, "Removed the stored key of openai") {
		t.Errorf("remove-key:\n%s", out)
	}
	if countAudit(t, "provider.remove_key", "openai") != 1 {
		t.Error("remove-key wasn't audited")
	}

	for _, tc := range []struct {
		args  []string
		input string
		code  int
		want  string
	}{
		{[]string{"provider", "remove-key", "openai"}, "", exitError, `provider "openai" has no stored key`},
		{[]string{"provider", "set-key", "mistral"}, "key", exitError, `the configuration has no provider "mistral"`},
		{[]string{"provider", "set-key", "openai"}, "\n", exitUsage, "the key is empty"},
		{[]string{"provider", "set-key", "openai"}, "two keys", exitUsage, "spaces or control characters"},
		{[]string{"provider", "set-key"}, "", exitUsage, "Usage:"},
		{[]string{"provider", "list", "extra"}, "", exitUsage, ""},
		{[]string{"provider", "bogus"}, "", exitUsage, `unknown command "bogus"`},
	} {
		withStdin(t, tc.input)
		var stdout, stderr bytes.Buffer
		if code := run(tc.args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("run(%q) = %d, stderr %q; want %d with %q", tc.args, code, stderr.String(), tc.code, tc.want)
		}
	}
}

// A stored key reaches the provider when the environment gives none.
func TestServeStoredKey(t *testing.T) {
	initDir(t)
	upstreamKey := "sk-EXAMPLE-" + "stored-upstream"
	fake := testutil.NewOpenAI(t, testutil.Config{APIKey: upstreamKey})
	writeConfig(t, fake.URL)
	t.Setenv("TEST_UPSTREAM_KEY", "")
	withStdin(t, upstreamKey)
	runOK(t, "provider", "set-key", "openai")
	key := printedKeyRE.FindStringSubmatch(runOK(t, "key", "create", "--name", "e2e"))[1]

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, "chowki.yaml", ln, &logs) }()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+ln.Addr().String()+
		"/v1/chat/completions", strings.NewReader(`{"model":"gpt-x","messages":[{"role":"user","content":"hi"}]}`))
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
	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("serve() = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}
	if !strings.Contains(logs.String(), `"msg":"using stored provider keys","providers":["openai"]`) ||
		strings.Contains(logs.String(), "provider key isn't set") {
		t.Errorf("logs:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), upstreamKey) {
		t.Error("the logs hold the stored key")
	}
}
