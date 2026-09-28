package pipeline_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

const backupKey = "sk-EXAMPLE-backup"

// withProviders adds OpenAI-type providers served by fake servers, and the
// aliases.
func withProviders(servers map[string]*testutil.Server, aliases map[string][]string) option {
	return func(h *harness, cfgs *[]config.Provider) {
		for name, s := range servers {
			*cfgs = append(*cfgs, config.Provider{Name: name, Type: config.TypeOpenAI, BaseURL: s.URL + "/v1",
				APIKeyEnv: "BACKUP_KEY", APIKey: backupKey})
		}
		h.aliases = aliases
	}
}

func withTimeout(d time.Duration) option {
	return func(h *harness, _ *[]config.Provider) {
		if d > 0 {
			h.gw.Timeout = d
		}
	}
}

var fastAlias = map[string][]string{"fast": {"openai/gpt-test", "backup/gpt-backup"}}

const fastBody = `{"model":"fast","messages":[{"role":"user","content":"hi"}]}`

// AC: fallback happens only on 429, 5xx and timeouts.
func TestFallback(t *testing.T) {
	tests := []struct {
		name    string
		primary testutil.Config
		timeout time.Duration
		status  int // what the client gets
		backup  int // requests that reach the backup
	}{
		{"rate limit", testutil.Config{FailStatus: 429}, 0, 200, 1},
		{"server error", testutil.Config{FailStatus: 500}, 0, 200, 1},
		{"unavailable", testutil.Config{FailStatus: 503}, 0, 200, 1},
		{"timeout", testutil.Config{Delay: 2 * time.Second}, 200 * time.Millisecond, 200, 1},
		{"bad request", testutil.Config{FailStatus: 400}, 0, 400, 0},
		{"authentication", testutil.Config{FailStatus: 401}, 0, 401, 0},
		{"not found", testutil.Config{FailStatus: 404}, 0, 404, 0},
		{"success", testutil.Config{}, 0, 200, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backup := testutil.NewOpenAI(t, testutil.Config{APIKey: backupKey})
			h := newHarness(t, tt.primary, testutil.Config{},
				withProviders(map[string]*testutil.Server{"backup": backup}, fastAlias), withTimeout(tt.timeout))
			resp := h.post(t.Context(), "/v1/chat/completions", fastBody)
			if body := readBody(t, resp); resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d\n%s", resp.StatusCode, tt.status, body)
			}
			if n := len(h.openai.Requests()); n != 1 {
				t.Errorf("the first target got %d requests, want 1", n)
			}
			reqs := backup.Requests()
			if len(reqs) != tt.backup {
				t.Fatalf("the backup got %d requests, want %d", len(reqs), tt.backup)
			}
			if tt.backup == 0 {
				return
			}
			if !strings.Contains(string(reqs[0].Body), `"model":"gpt-backup"`) {
				t.Errorf("the backup got the model of another target: %s", reqs[0].Body)
			}
			if recs := h.records(); len(recs) != 1 || recs[0].Provider != "backup" || recs[0].Model != "gpt-backup" {
				t.Errorf("records = %+v; want the backup that answered", recs)
			}
			if !strings.Contains(h.logs.String(), `"fallbacks":1`) {
				t.Error("the log doesn't count the fallback")
			}
		})
	}
	t.Run("connection error", func(t *testing.T) {
		gone := httptest.NewServer(http.NotFoundHandler())
		gone.Close()
		backup := testutil.NewOpenAI(t, testutil.Config{APIKey: backupKey})
		h := newHarness(t, testutil.Config{}, testutil.Config{},
			withProviders(map[string]*testutil.Server{"backup": backup}, fastAlias),
			func(_ *harness, cfgs *[]config.Provider) { (*cfgs)[0].BaseURL = gone.URL + "/v1" })
		if resp := h.post(t.Context(), "/v1/chat/completions", fastBody); resp.StatusCode != http.StatusOK ||
			len(backup.Requests()) != 1 {
			t.Errorf("status %d, %d backup requests; want 200 and 1", resp.StatusCode, len(backup.Requests()))
		}
	})
}

// AC: fallback happens only before the first byte reaches the client.
func TestStreamFallback(t *testing.T) {
	stream := `{"model":"fast","messages":[{"role":"user","content":"hi"}],"stream":true}`
	t.Run("before the first byte", func(t *testing.T) {
		backup := testutil.NewOpenAI(t, testutil.Config{APIKey: backupKey})
		h := newHarness(t, testutil.Config{FailStatus: 429}, testutil.Config{},
			withProviders(map[string]*testutil.Server{"backup": backup}, fastAlias))
		resp := h.post(t.Context(), "/v1/chat/completions", stream)
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK || !strings.Contains(body, "data: [DONE]") {
			t.Errorf("status %d, body %s; want the backup's stream", resp.StatusCode, body)
		}
	})
	t.Run("after the first byte", func(t *testing.T) {
		backup := testutil.NewOpenAI(t, testutil.Config{APIKey: backupKey})
		h := newHarness(t, testutil.Config{Chunks: 3, ChunkDelay: 300 * time.Millisecond}, testutil.Config{},
			withProviders(map[string]*testutil.Server{"backup": backup}, fastAlias), withTimeout(450*time.Millisecond))
		resp := h.post(t.Context(), "/v1/chat/completions", stream)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, "data: ") || strings.Contains(body, "[DONE]") {
			t.Errorf("status %d, body %s; want the start of the first target's stream only", resp.StatusCode, body)
		}
		if n := len(backup.Requests()); n != 0 {
			t.Errorf("the backup got %d requests after the stream started, want none", n)
		}
		if recs := h.records(); len(recs) != 1 || recs[0].ErrorType != "upstream_timeout" {
			t.Errorf("records = %+v; want the stream's timeout", recs)
		}
	})
}

func TestAtMostTwoFallbacks(t *testing.T) {
	failing := testutil.NewOpenAI(t, testutil.Config{APIKey: backupKey, FailStatus: 500})
	servers := map[string]*testutil.Server{"p2": failing, "p3": failing, "p4": failing}
	aliases := map[string][]string{"fast": {"openai/gpt-test", "p2/m", "p3/m", "p4/m"}}
	h := newHarness(t, testutil.Config{FailStatus: 500}, testutil.Config{}, withProviders(servers, aliases))
	resp := h.post(t.Context(), "/v1/chat/completions", fastBody)
	if readBody(t, resp); resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want the last target's 500", resp.StatusCode)
	}
	if n := len(h.openai.Requests()) + len(failing.Requests()); n != 3 {
		t.Errorf("%d targets were tried, want 3: the first and two fallbacks", n)
	}
}

func TestCircuitBreaker(t *testing.T) {
	backup := testutil.NewOpenAI(t, testutil.Config{APIKey: backupKey})
	h := newHarness(t, testutil.Config{FailStatus: 500}, testutil.Config{},
		withProviders(map[string]*testutil.Server{"backup": backup}, fastAlias))
	for i := range 4 {
		if resp := h.post(t.Context(), "/v1/chat/completions", fastBody); resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status %d", i+1, resp.StatusCode)
		}
	}
	// After three failures in a row, the first target is skipped.
	if n := len(h.openai.Requests()); n != 3 {
		t.Errorf("the failing target got %d requests, want 3", n)
	}
}

func TestModelAllowlist(t *testing.T) {
	backup := testutil.NewOpenAI(t, testutil.Config{APIKey: backupKey})
	h := newHarness(t, testutil.Config{}, testutil.Config{},
		withProviders(map[string]*testutil.Server{"backup": backup}, fastAlias))
	models := []string{"fast", "openai/*"}
	if _, err := h.st.UpdateKey(t.Context(), h.key[:auth.PrefixLen], store.KeyUpdate{AllowedModels: &models}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		path, model string
		status      int
	}{
		{"/v1/chat/completions", "fast", 200},
		{"/v1/chat/completions", "openai/gpt-test", 200},
		{"/v1/chat/completions", "gpt-test", 403},
		{"/v1/chat/completions", "backup/gpt-backup", 403},
		{"/anthropic/v1/messages", "claude-test", 403},
	} {
		body, _ := json.Marshal(map[string]any{"model": tt.model, "max_tokens": 16,
			"messages": []map[string]string{{"role": "user", "content": "hi"}}})
		resp := h.post(t.Context(), tt.path, string(body))
		got := readBody(t, resp)
		if resp.StatusCode != tt.status {
			t.Errorf("%s %s: status %d, want %d\n%s", tt.path, tt.model, resp.StatusCode, tt.status, got)
		}
		if tt.status == 403 && !strings.Contains(got, `may use: fast, openai/*.`) {
			t.Errorf("%s: error = %s; want the models that the key may use", tt.model, got)
		}
	}
}
