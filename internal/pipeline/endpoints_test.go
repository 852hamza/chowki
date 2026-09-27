package pipeline_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
	"github.com/852hamza/chowki/internal/testutil"
)

func TestEmbeddings(t *testing.T) {
	h := newHarness(t, testutil.Config{Usage: testutil.Usage{Input: 2000}}, testutil.Config{})
	setKeyCache(t, h, "exact")
	body := `{"model":"embed-test","input":["mail jane.doe@company.io","plain text"]}`
	first := h.post(t.Context(), "/v1/embeddings", body)
	got := readBody(t, first)
	if first.StatusCode != http.StatusOK || !strings.Contains(got, `"embedding":[0.25,-0.5,0.75]`) ||
		first.Header.Get("x-chowki-redactions") != "1" || first.Header.Get("x-chowki-cache") != "miss" {
		t.Fatalf("status %d, headers %v\n%s", first.StatusCode, first.Header, got)
	}
	sent := string(h.openai.Requests()[0].Body)
	if strings.Contains(sent, "jane.doe") || !strings.Contains(sent, "[REDACTED:email:") ||
		!strings.Contains(sent, `"plain text"`) {
		t.Errorf("the provider got %s; want the email masked in the input", sent)
	}
	waitCached(t, h)
	if second := h.post(t.Context(), "/v1/embeddings", body); second.Header.Get("x-chowki-cache") != "hit" ||
		readBody(t, second) != got {
		t.Errorf("a repeated embedding request wasn't a hit")
	}
	recs := h.records()
	// 2000 tokens at $0.50 per million.
	if len(recs) != 2 || recs[0].Endpoint != "/v1/embeddings" || recs[0].CostUSD == nil || *recs[0].CostUSD != 0.001 ||
		recs[0].Tokens == nil || recs[0].Tokens.Input != 2000 {
		t.Errorf("records = %+v", recs)
	}
}

// Counting tokens is free: it spends no budget, and a key whose budget is
// used up can still count.
func TestCountTokens(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{Usage: testutil.Usage{Input: 14}})
	setKeyBudget(t, h, 0.000001)
	readBody(t, h.post(t.Context(), "/anthropic/v1/messages", anthropicBody)) // uses the budget up
	if resp := h.post(t.Context(), "/anthropic/v1/messages", anthropicBody); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d; want the budget used up", resp.StatusCode)
	}
	body := `{"model":"claude-test","system":"Be brief.","messages":[{"role":"user","content":"Call +14155552671"}]}`
	resp := h.post(t.Context(), "/anthropic/v1/messages/count_tokens", body)
	if got := readBody(t, resp); resp.StatusCode != http.StatusOK || strings.TrimSpace(got) != `{"input_tokens":14}` {
		t.Fatalf("status %d, body %s; want the provider's count", resp.StatusCode, got)
	}
	if sent := string(h.anthropic.Requests()[1].Body); strings.Contains(sent, "4155552671") {
		t.Errorf("the provider got the phone number: %s", sent)
	}
	recs := h.records()
	if len(recs) != 3 || recs[2].Endpoint != "/anthropic/v1/messages/count_tokens" || recs[2].CostUSD == nil ||
		*recs[2].CostUSD != 0 || recs[2].Tokens != nil {
		t.Errorf("records = %+v; want a free request without usage", recs)
	}
}

func TestModels(t *testing.T) {
	h := newHarness(t, testutil.Config{}, testutil.Config{},
		withProviders(map[string]*testutil.Server{"backup": testutil.NewOpenAI(t, testutil.Config{APIKey: backupKey})},
			map[string][]string{"fast": {"openai/gpt-test", "backup/gpt-backup"}, "smart": {"anthropic/claude-test"}}))
	list := func() []string {
		t.Helper()
		resp := h.send(t.Context(), http.MethodGet, h.url+"/v1/models", map[string]string{"Authorization": "Bearer " + h.key}, "")
		var got struct {
			Object string
			Data   []struct {
				ID, Object string
				OwnedBy    string `json:"owned_by"`
			}
		}
		if err := json.Unmarshal([]byte(readBody(t, resp)), &got); err != nil || resp.StatusCode != http.StatusOK ||
			got.Object != "list" {
			t.Fatalf("GET /v1/models = %d, %v", resp.StatusCode, err)
		}
		var ids []string
		for _, m := range got.Data {
			if m.Object != "model" || m.OwnedBy == "" {
				t.Errorf("model %+v", m)
			}
			ids = append(ids, m.ID)
		}
		return ids
	}
	// OpenAI-format clients see the aliases and models they can reach.
	if got := strings.Join(list(), " "); got != "fast openai/embed-test openai/gpt-test" {
		t.Errorf("models = %s", got)
	}
	models := []string{"fast"}
	if _, err := h.st.UpdateKey(t.Context(), h.key[:auth.PrefixLen], store.KeyUpdate{AllowedModels: &models}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(list(), " "); got != "fast" {
		t.Errorf("models of a key limited to fast = %s", got)
	}
	if resp := h.send(t.Context(), http.MethodGet, h.url+"/v1/models", nil, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /v1/models without a key = %d, want 401", resp.StatusCode)
	}
}
