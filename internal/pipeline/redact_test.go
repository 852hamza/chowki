package pipeline_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

func setKeyRedaction(t *testing.T, h *harness, mode string) {
	t.Helper()
	if _, err := h.st.UpdateKey(t.Context(), h.key[:auth.PrefixLen], store.KeyUpdate{RedactionMode: &mode}); err != nil {
		t.Fatal(err)
	}
}

// secretsBody returns a request of each API family whose text holds every
// fixture secret, and the number of secrets.
func secretsBody(path string) (string, int) {
	var lines []string
	secrets := testutil.Secrets()
	for _, s := range secrets {
		lines = append(lines, "Line with "+s.Context+s.Value+" in it.")
	}
	text, _ := json.Marshal(strings.Join(lines, "\n"))
	if strings.HasPrefix(path, "/anthropic/") {
		return `{"model":"claude-test","max_tokens":64,"system":` + string(text) +
			`,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":` +
			string(text) + `}]}]}`, 2 * len(secrets)
	}
	return `{"model":"gpt-test","messages":[{"role":"user","content":[{"type":"text","text":` + string(text) + `}]}]}`,
		len(secrets)
}

// checkNoSecrets fails when the text holds any fixture secret.
func checkNoSecrets(t *testing.T, where, text string) {
	t.Helper()
	for _, s := range testutil.Secrets() {
		if strings.Contains(text, s.Value) {
			t.Errorf("%s holds the %s %q", where, s.Type, s.Value)
		}
	}
}

// AC: in mask mode, no fixture secret reaches the fake provider.
func TestRedactionMask(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/anthropic/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			h := newHarness(t, testutil.Config{}, testutil.Config{})
			body, n := secretsBody(path)
			resp := h.post(t.Context(), path, body)
			if readBody(t, resp); resp.StatusCode != http.StatusOK ||
				resp.Header.Get(pipeline.RedactionsHeader) != strconv.Itoa(n) {
				t.Fatalf("status %d, %s %q; want 200 and %d", resp.StatusCode, pipeline.RedactionsHeader,
					resp.Header.Get(pipeline.RedactionsHeader), n)
			}
			reqs := append(h.openai.Requests(), h.anthropic.Requests()...)
			if len(reqs) != 1 {
				t.Fatalf("the providers got %d requests, want 1", len(reqs))
			}
			var sent any // decoded, so that escaped characters don't hide a secret
			if err := json.Unmarshal(reqs[0].Body, &sent); err != nil {
				t.Fatal(err)
			}
			decoded, _ := json.Marshal(sent)
			checkNoSecrets(t, "the request the provider got", string(reqs[0].Body)+strings.ReplaceAll(string(decoded), `\n`, "\n"))
			if !strings.Contains(string(reqs[0].Body), "[REDACTED:email:") {
				t.Errorf("the provider's request has no placeholder:\n%s", reqs[0].Body)
			}
			recs := h.records()
			if len(recs) != 1 || recs[0].Redactions["email"] == 0 || recs[0].Redactions["cnic"] == 0 {
				t.Errorf("record = %+v; want the counts by type", recs)
			}
			checkNoSecrets(t, "the log", h.logs.String())
		})
	}
}

// AC: in block mode, no fixture secret reaches the fake provider.
func TestRedactionBlock(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/anthropic/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			h := newHarness(t, testutil.Config{}, testutil.Config{})
			setKeyRedaction(t, h, "block")
			body := `{"model":"gpt-test","max_tokens":64,"messages":[{"role":"user","content":"mail jane.doe@company.io"}]}`
			if strings.HasPrefix(path, "/anthropic/") {
				body = strings.Replace(body, "gpt-test", "claude-test", 1)
			}
			resp := h.post(t.Context(), path, body)
			got := readBody(t, resp)
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(got, "may not send: email.") ||
				strings.Contains(got, "jane.doe") {
				t.Errorf("status %d, body %s; want 400 that names only the type", resp.StatusCode, got)
			}
			if !strings.HasPrefix(path, "/anthropic/") && !strings.Contains(got, `"code":"sensitive_data_blocked"`) {
				t.Errorf("OpenAI error = %s; want the code sensitive_data_blocked", got)
			}
			if n := len(h.openai.Requests()) + len(h.anthropic.Requests()); n != 0 {
				t.Errorf("the providers got %d requests, want none", n)
			}
		})
	}
}

func TestRedactionAlertAndOff(t *testing.T) {
	body := `{"model":"gpt-test","messages":[{"role":"user","content":"mail jane.doe@company.io"}]}`
	for mode, header := range map[string]string{"alert": "1", "off": ""} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t, testutil.Config{}, testutil.Config{})
			setKeyRedaction(t, h, mode)
			resp := h.post(t.Context(), "/v1/chat/completions", body)
			if readBody(t, resp); resp.StatusCode != http.StatusOK || resp.Header.Get(pipeline.RedactionsHeader) != header {
				t.Errorf("status %d, %s %q; want 200 and %q", resp.StatusCode, pipeline.RedactionsHeader,
					resp.Header.Get(pipeline.RedactionsHeader), header)
			}
			if reqs := h.openai.Requests(); len(reqs) != 1 || string(reqs[0].Body) != body {
				t.Errorf("the provider didn't get the request unchanged")
			}
			if logs := h.logs.String(); strings.Contains(logs, "jane.doe") ||
				(mode == "alert") != strings.Contains(logs, `"msg":"sensitive data in a request"`) {
				t.Errorf("logs:\n%s\nwant a warning with counts only in alert mode", logs)
			}
		})
	}
}

// A request for a model that the key may not use is refused before
// redaction, so its findings are neither reported nor counted.
func TestModelCheckedBeforeRedaction(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{})
	setKeyRedaction(t, h, "block")
	models := []string{"claude-*"}
	if _, err := h.st.UpdateKey(t.Context(), h.key[:auth.PrefixLen], store.KeyUpdate{AllowedModels: &models}); err != nil {
		t.Fatal(err)
	}
	resp := h.post(t.Context(), "/v1/chat/completions",
		`{"model":"gpt-test","messages":[{"role":"user","content":"mail jane.doe@company.io"}]}`)
	if got := readBody(t, resp); resp.StatusCode != http.StatusForbidden ||
		!strings.Contains(got, `"code":"model_not_allowed"`) || resp.Header.Get(pipeline.RedactionsHeader) != "" {
		t.Errorf("status %d, %s %q, body %s; want 403 model_not_allowed and no redactions", resp.StatusCode,
			pipeline.RedactionsHeader, resp.Header.Get(pipeline.RedactionsHeader), got)
	}
	for _, r := range h.records() {
		if len(r.Redactions) != 0 {
			t.Errorf("record = %+v; want no redactions", r)
		}
	}
}
