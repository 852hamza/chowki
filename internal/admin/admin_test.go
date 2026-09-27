package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
)

var now = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

type harness struct {
	t     *testing.T
	path  string
	st    *store.SQLite
	url   string
	token string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chowki.db")
	st, err := store.OpenSQLite(t.Context(), "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	token, _, err := auth.CreateAdmin(t.Context(), st, "ops", now)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer((&API{Store: st, Logger: slog.New(slog.DiscardHandler), Now: func() time.Time { return now }}).Handler())
	t.Cleanup(srv.Close)
	return &harness{t: t, path: path, st: st, url: srv.URL, token: token}
}

// do sends a request with the harness's admin token and returns the status
// and the decoded JSON body.
func (h *harness) do(method, path, body string) (int, map[string]any) {
	h.t.Helper()
	return h.doWith("Bearer "+h.token, method, path, body)
}

func (h *harness) doWith(authorization, method, path, body string) (int, map[string]any) {
	h.t.Helper()
	req, err := http.NewRequestWithContext(h.t.Context(), method, h.url+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		h.t.Fatalf("%s %s: body %s isn't JSON: %v", method, path, data, err)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		h.t.Errorf("%s %s: Content-Type %q", method, path, resp.Header.Get("Content-Type"))
	}
	return resp.StatusCode, out
}

func errorCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestAuthentication(t *testing.T) {
	h := newHarness(t)
	key, _, err := auth.Create(t.Context(), h.st, "p", store.Key{Name: "k"}, now)
	if err != nil {
		t.Fatal(err)
	}
	revoked, stored, err := auth.CreateAdmin(t.Context(), h.st, "old", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.RevokeAdminToken(t.Context(), stored.Prefix, now); err != nil {
		t.Fatal(err)
	}
	for authorization, code := range map[string]string{
		"":                  "missing_admin_token",
		"Basic x":           "missing_admin_token",
		"Bearer " + key:     "invalid_admin_token",
		"Bearer " + revoked: "invalid_admin_token",
	} {
		if status, body := h.doWith(authorization, http.MethodGet, "/admin/v1/keys", ""); status != http.StatusUnauthorized ||
			errorCode(body) != code {
			t.Errorf("Authorization %q: %d %v; want 401 %s", authorization, status, body, code)
		}
	}
	if status, body := h.do(http.MethodGet, "/admin/v1/nothing", ""); status != http.StatusNotFound ||
		errorCode(body) != "not_found" {
		t.Errorf("unknown path: %d %v", status, body)
	}
}

func TestKeys(t *testing.T) {
	h := newHarness(t)
	status, body := h.do(http.MethodPost, "/admin/v1/keys", `{"name":"alice","project":"team","budget_usd":50,`+
		`"rpm":60,"cache":"exact","redaction":"block","models":["fast"]}`)
	secret, _ := body["key"].(string)
	if status != http.StatusCreated || !strings.HasPrefix(secret, auth.KeyPrefix) || body["budget_usd"] != 50.0 ||
		body["cache"] != "exact" || body["redaction"] != "block" || body["project"] != "team" {
		t.Fatalf("POST /admin/v1/keys = %d %v", status, body)
	}
	k, err := auth.Verify(t.Context(), h.st, secret)
	if err != nil || k.RPM != 60 || len(k.AllowedModels) != 1 {
		t.Fatalf("the created key = %+v, %v", k, err)
	}
	prefix := k.Prefix

	for _, bad := range []string{`{"name":""}`, `{"name":"a","rpm":-1}`, `{"name":"a","cache":"always"}`,
		`{"name":"a","models":["["]}`, `{"name":"a","unknown":1}`, `not JSON`} {
		if status, body := h.do(http.MethodPost, "/admin/v1/keys", bad); status != http.StatusBadRequest ||
			errorCode(body) != "invalid_request" {
			t.Errorf("POST %s: %d %v; want 400", bad, status, body)
		}
	}

	status, body = h.do(http.MethodPatch, "/admin/v1/keys/"+prefix, `{"budget_usd":0,"cache":"default","models":[]}`)
	if status != http.StatusOK || body["budget_usd"] != 0.0 || body["cache"] != "default" || body["rpm"] != 60.0 {
		t.Errorf("PATCH = %d %v", status, body)
	}
	if status, body := h.do(http.MethodPatch, "/admin/v1/keys/chowki_nope1", `{"rpm":1}`); status != http.StatusNotFound ||
		errorCode(body) != "key_not_found" {
		t.Errorf("PATCH an unknown key: %d %v", status, body)
	}

	cost := 1.5
	if err := h.st.InsertRequests(t.Context(), []store.Request{{ID: "r", Time: now, KeyID: k.ID, CostUSD: &cost}}); err != nil {
		t.Fatal(err)
	}
	status, body = h.do(http.MethodGet, "/admin/v1/keys", "")
	keys, _ := body["keys"].([]any)
	first, _ := keys[0].(map[string]any)
	if status != http.StatusOK || len(keys) != 1 || first["spent_usd"] != 1.5 || first["prefix"] != prefix ||
		first["revoked_at"] != nil {
		t.Errorf("GET /admin/v1/keys = %d %v", status, body)
	}

	status, body = h.do(http.MethodDelete, "/admin/v1/keys/"+secret, "")
	if status != http.StatusOK || body["revoked_at"] == nil {
		t.Errorf("DELETE = %d %v", status, body)
	}
	if _, err := auth.Verify(t.Context(), h.st, secret); !errors.Is(err, auth.ErrRevoked) {
		t.Errorf("Verify() after DELETE: %v, want ErrRevoked", err)
	}
	db, err := sql.Open("sqlite", "file:"+h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_log WHERE actor = ? AND target = ?`,
		"admin:"+h.token[:auth.AdminPrefixLen], prefix).Scan(&n); err != nil || n != 3 {
		t.Errorf("audit log has %d entries by the admin token, %v; want create, update and revoke", n, err)
	}
}

func TestProjects(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"a", "b"} {
		if _, _, err := auth.Create(t.Context(), h.st, "team", store.Key{Name: name}, now); err != nil {
			t.Fatal(err)
		}
	}
	status, body := h.do(http.MethodPatch, "/admin/v1/projects/team", `{"budget_usd":200}`)
	if status != http.StatusOK || body["budget_usd"] != 200.0 {
		t.Errorf("PATCH project = %d %v", status, body)
	}
	status, body = h.do(http.MethodGet, "/admin/v1/projects", "")
	projects, _ := body["projects"].([]any)
	p, _ := projects[0].(map[string]any)
	if status != http.StatusOK || len(projects) != 1 || p["active_keys"] != 2.0 || p["budget_usd"] != 200.0 {
		t.Errorf("GET projects = %d %v", status, body)
	}
	for path, want := range map[string]int{"/admin/v1/projects/nope": 404, "/admin/v1/projects/team": 400} {
		body := `{"budget_usd":5}`
		if want == 400 {
			body = `{"budget_usd":-5}`
		}
		if status, _ := h.do(http.MethodPatch, path, body); status != want {
			t.Errorf("PATCH %s %s = %d, want %d", path, body, status, want)
		}
	}
}

func TestReports(t *testing.T) {
	h := newHarness(t)
	k, _, err := auth.Create(t.Context(), h.st, "team", store.Key{Name: "alice"}, now)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := h.st.KeyByPrefix(t.Context(), k[:auth.PrefixLen])
	if err != nil {
		t.Fatal(err)
	}
	cost := 2.0
	if err := h.st.InsertRequests(t.Context(), []store.Request{
		{ID: "r1", Time: now.Add(-2 * time.Hour), KeyID: stored.ID, Provider: "openai", Model: "a", Status: 200,
			CostUSD: &cost, Tokens: &store.Tokens{Input: 10, Output: 5}, CacheStatus: "miss"},
		{ID: "r2", Time: now.Add(-time.Hour), KeyID: stored.ID, Provider: "openai", Model: "a", Status: 200,
			SavingsUSD: 2, SavingsMethod: "exact_cache", CacheStatus: "hit", Redactions: map[string]int{"email": 1}},
		{ID: "last-month", Time: now.AddDate(0, -1, 0), KeyID: stored.ID, Status: 200, CostUSD: &cost},
	}); err != nil {
		t.Fatal(err)
	}

	status, body := h.do(http.MethodGet, "/admin/v1/summary", "")
	cache, _ := body["cache"].(map[string]any)
	if status != http.StatusOK || body["requests"] != 2.0 || body["cost_usd"] != 2.0 || cache["hit_rate"] != 0.5 ||
		body["from"] != "2026-09-01T00:00:00Z" {
		t.Errorf("summary = %d %v", status, body)
	}
	if _, body := h.do(http.MethodGet, "/admin/v1/summary?from=2026-08-01&to=2026-09-26", ""); body["requests"] != 1.0 {
		t.Errorf("summary of August = %v", body)
	}
	status, body = h.do(http.MethodGet, "/admin/v1/breakdown?by=model", "")
	groups, _ := body["groups"].([]any)
	g, _ := groups[0].(map[string]any)
	if status != http.StatusOK || len(groups) != 1 || g["id"] != "openai/a" || g["requests"] != 2.0 {
		t.Errorf("breakdown = %d %v", status, body)
	}
	status, body = h.do(http.MethodGet, "/admin/v1/requests?limit=1", "")
	reqs, _ := body["requests"].([]any)
	r, _ := reqs[0].(map[string]any)
	if status != http.StatusOK || len(reqs) != 1 || r["id"] != "r2" || r["cache"] != "hit" {
		t.Errorf("requests = %d %v", status, body)
	}
	for _, path := range []string{"/admin/v1/breakdown?by=project", "/admin/v1/summary?from=yesterday",
		"/admin/v1/summary?from=2026-09-27&to=2026-09-01", "/admin/v1/requests?limit=0", "/admin/v1/requests?before=x"} {
		if status, body := h.do(http.MethodGet, path, ""); status != http.StatusBadRequest {
			t.Errorf("GET %s = %d %v; want 400", path, status, body)
		}
	}
}
