package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
)

var now = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

type harness struct {
	t      *testing.T
	st     *store.SQLite
	ui     *UI
	url    string
	token  string // an admin token
	client *http.Client
	clock  atomic.Int64 // what UI.Now returns, in Unix nanoseconds
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.OpenSQLite(t.Context(), "file:"+filepath.Join(t.TempDir(), "chowki.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	token, _, err := auth.CreateAdmin(t.Context(), st, "ops", now)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, st: st, token: token}
	h.clock.Store(now.UnixNano())
	h.ui = &UI{Store: st, Logger: slog.New(slog.DiscardHandler), Now: func() time.Time { return time.Unix(0, h.clock.Load()) }}
	srv := httptest.NewServer(h.ui.Handler())
	t.Cleanup(srv.Close)
	h.url = srv.URL
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	h.client = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return h
}

// do sends a request, with form as its body unless it's nil, and returns
// the response and its body.
func (h *harness) do(method, path string, form url.Values) (*http.Response, string) {
	h.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(h.t.Context(), method, h.url+path, body)
	if err != nil {
		h.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp, string(data)
}

// csrf returns the CSRF token in the cookie jar.
func (h *harness) csrf() string {
	u, _ := url.Parse(h.url + "/ui/")
	for _, c := range h.client.Jar.Cookies(u) {
		if c.Name == csrfCookie {
			return c.Value
		}
	}
	return ""
}

func (h *harness) signIn() *http.Response {
	h.t.Helper()
	h.do(http.MethodGet, "/ui/login", nil)
	resp, body := h.do(http.MethodPost, "/ui/login", url.Values{"csrf": {h.csrf()}, "token": {h.token}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/ui/" {
		h.t.Fatalf("sign in = %d %s %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	return resp
}

// signedIn reports whether the dashboard shows, rather than redirecting to
// the sign-in page.
func (h *harness) signedIn() bool {
	h.t.Helper()
	resp, _ := h.do(http.MethodGet, "/ui/", nil)
	return resp.StatusCode == http.StatusOK
}

func TestSignIn(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.do(http.MethodGet, "/ui/", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/ui/login" {
		t.Fatalf("GET /ui/ signed out = %d %s, want a redirect to /ui/login", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, body := h.do(http.MethodGet, "/ui/login", nil)
	if resp.StatusCode != http.StatusOK || len(h.csrf()) != 64 || !strings.Contains(body, `value="`+h.csrf()+`"`) {
		t.Fatalf("GET /ui/login = %d, CSRF cookie %q, body %s", resp.StatusCode, h.csrf(), body)
	}
	for name, want := range map[string]string{
		"Content-Security-Policy": "default-src 'none'; script-src 'self'; style-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
		"Content-Type":            "text/html; charset=utf-8",
	} {
		if got := resp.Header.Get(name); !strings.Contains(got, want) {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}

	key, _, err := auth.Create(t.Context(), h.st, "team", store.Key{Name: "k"}, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		form   url.Values
		status int
		says   string
	}{
		{"no CSRF token", url.Values{"token": {h.token}}, http.StatusForbidden, "The form expired"},
		{"another CSRF token", url.Values{"csrf": {strings.Repeat("0", 64)}, "token": {h.token}}, http.StatusForbidden,
			"The form expired"},
		{"a wrong admin token", url.Values{"csrf": {h.csrf()}, "token": {"chowki_admin_nope"}}, http.StatusUnauthorized,
			"That admin token"},
		{"a virtual key", url.Values{"csrf": {h.csrf()}, "token": {key}}, http.StatusUnauthorized, "That admin token"},
	} {
		resp, body := h.do(http.MethodPost, "/ui/login", tc.form)
		if resp.StatusCode != tc.status || !strings.Contains(body, tc.says) || !strings.Contains(body, `role="alert"`) {
			t.Errorf("sign in with %s = %d, want %d saying %q: %s", tc.name, resp.StatusCode, tc.status, tc.says, body)
		}
		if h.signedIn() {
			t.Fatalf("signed in with %s", tc.name)
		}
	}

	resp = h.signIn()
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/ui" ||
		len(session.Value) != 64 {
		t.Fatalf("session cookie = %+v", session)
	}
	if !h.signedIn() {
		t.Fatal("the dashboard doesn't show after signing in")
	}
	if resp, _ := h.do(http.MethodGet, "/ui/login", nil); resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/ui/" {
		t.Errorf("GET /ui/login signed in = %d %s, want a redirect to /ui/", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestSecureCookies(t *testing.T) {
	ui := &UI{Logger: slog.New(slog.DiscardHandler)}
	for header, secure := range map[string]bool{"": false, "http": false, "https": true} {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ui/login", nil)
		if header != "" {
			r.Header.Set("X-Forwarded-Proto", header)
		}
		ui.Handler().ServeHTTP(w, r)
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Secure != secure {
			t.Errorf("X-Forwarded-Proto %q: cookies %+v, want Secure %v", header, cookies, secure)
		}
	}
}

func TestSessionEnds(t *testing.T) {
	t.Run("sign out", func(t *testing.T) {
		h := newHarness(t)
		h.signIn()
		if resp, _ := h.do(http.MethodPost, "/ui/logout", url.Values{}); resp.StatusCode != http.StatusSeeOther ||
			!h.signedIn() {
			t.Fatalf("signing out without the CSRF token = %d; it must not sign out", resp.StatusCode)
		}
		resp, _ := h.do(http.MethodPost, "/ui/logout", url.Values{"csrf": {h.csrf()}})
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/ui/login" || h.signedIn() {
			t.Fatalf("sign out = %d %s; still signed in: %v", resp.StatusCode, resp.Header.Get("Location"), h.signedIn())
		}
	})
	t.Run("revoked admin token", func(t *testing.T) {
		h := newHarness(t)
		h.signIn()
		if _, err := h.st.RevokeAdminToken(t.Context(), h.token[:auth.AdminPrefixLen], now); err != nil {
			t.Fatal(err)
		}
		if h.signedIn() {
			t.Error("the session outlived its admin token")
		}
	})
	t.Run("expiry", func(t *testing.T) {
		h := newHarness(t)
		h.signIn()
		h.clock.Store(now.Add(sessionTTL - time.Second).UnixNano())
		if !h.signedIn() {
			t.Fatal("the session ended early")
		}
		h.clock.Store(now.Add(sessionTTL + time.Second).UnixNano())
		if h.signedIn() {
			t.Error("the session outlived its lifetime")
		}
	})
	t.Run("too many sessions", func(t *testing.T) {
		h := newHarness(t)
		h.ui.sessions = map[string]session{}
		for i := range maxSessions {
			h.ui.sessions[randomID()] = session{admin: "x", expires: now.Add(time.Duration(i) * time.Second)}
		}
		h.signIn()
		if n := len(h.ui.sessions); n > maxSessions || !h.signedIn() {
			t.Errorf("%d sessions after signing in; signed in: %v", n, h.signedIn())
		}
	})
}

func TestStatic(t *testing.T) {
	h := newHarness(t)
	for path, contentType := range map[string]string{
		"/ui/static/style.css":    "text/css",
		"/ui/static/dashboard.js": "text/javascript",
		"/ui/static/icon.svg":     "image/svg+xml",
	} {
		resp, body := h.do(http.MethodGet, path, nil)
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), contentType) ||
			body == "" || resp.Header.Get("Cache-Control") == "no-store" {
			t.Errorf("GET %s = %d %q %q", path, resp.StatusCode, resp.Header.Get("Content-Type"),
				resp.Header.Get("Cache-Control"))
		}
	}
	if resp, _ := h.do(http.MethodGet, "/ui", nil); resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/ui/" {
		t.Errorf("GET /ui = %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := h.do(http.MethodGet, "/ui/nothing", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /ui/nothing = %d, want 404", resp.StatusCode)
	}
}

func usdPtr(v float64) *float64 { return &v }

// seed adds two keys, alice with a budget of $10 and bob, in project team
// with a budget of $100, and requests this month and the last.
func seed(t *testing.T, st store.Store) {
	t.Helper()
	var ids []int64
	for _, k := range []store.Key{{Name: "alice", BudgetUSD: 10}, {Name: "bob"}} {
		_, stored, err := auth.Create(t.Context(), st, "team", k, now)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, stored.ID)
	}
	budget := 100.0
	if _, err := st.UpdateProject(t.Context(), "team", store.ProjectUpdate{BudgetUSD: &budget}); err != nil {
		t.Fatal(err)
	}
	hour := now.Add(-time.Hour)
	if err := st.InsertRequests(t.Context(), []store.Request{
		{ID: "r1", Time: hour, KeyID: ids[0], Provider: "openai", Model: "gpt-x", Status: 200,
			Tokens: &store.Tokens{Input: 1000, Output: 200}, CostUSD: usdPtr(8.5), CacheStatus: "miss"},
		{ID: "r2", Time: hour, KeyID: ids[0], Provider: "openai", Model: "gpt-x", Status: 200,
			SavingsUSD: 1.25, SavingsMethod: "exact_cache", CacheStatus: "hit"},
		{ID: "r3", Time: now.AddDate(0, 0, -2), KeyID: ids[1], Provider: "anthropic", Model: "claude", Status: 200,
			Tokens: &store.Tokens{Input: 3000, Output: 500}, CostUSD: usdPtr(0.5), SavingsUSD: 0.25,
			SavingsMethod: "prompt_cache", Redactions: map[string]int{"email": 2}},
		{ID: "r4", Time: hour, KeyID: ids[1], Provider: "ollama", Model: "<script>alert(1)</script>", Status: 500},
		{ID: "r5", Time: hour, Status: 401},
		{ID: "august", Time: now.AddDate(0, -1, 0), KeyID: ids[1], Provider: "openai", Model: "gpt-x", Status: 200,
			CostUSD: usdPtr(3)},
	}); err != nil {
		t.Fatal(err)
	}
}

// has reports whether body has each of wants.
func has(t *testing.T, name, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("%s doesn't have %q", name, want)
		}
	}
}

func TestDashboard(t *testing.T) {
	h := newHarness(t)
	seed(t, h.st)
	h.signIn()

	_, body := h.do(http.MethodGet, "/ui/", nil)
	has(t, "the dashboard", body,
		`<p class="period">September 1 to September 27, 2026, UTC</p>`,
		`<h2>Spend</h2><p class="value">$9.00</p><p class="note">Every request with usage had a price</p>`,
		`<h2>Requests</h2><p class="value">5</p>`,
		`<h2>Failed</h2><p class="value">2</p><p class="note">40.0% of requests</p>`,
		`<h2>Cache hit rate</h2><p class="value">50%</p><p class="note">1 of 2 lookups</p>`,
		`<h2>Net savings</h2><p class="value">$1.50</p>`,
		`<h2>Redactions</h2><p class="value">2</p>`,
		`<h2 id="chart-title">Spend by day</h2>`,
		`aria-label="Sun, Sep 27: 4 requests, 1.2k tokens, $8.50"`,
		`<span class="name">alice</span>`, `$8.50 of $10.00`, `85% used`, `<li class="warning">`,
		`<span class="name">team</span><span class="detail">Project</span>`, `$9.00 of $100.00`,
		`<span class="name">No valid key</span>`,
		`<th scope="row">Exact cache</th><td>$1.25</td>`, `<th scope="row">Provider prompt caching</th><td>$0.25</td>`,
		`<span class="name">email</span>`,
		`&lt;script&gt;alert(1)&lt;/script&gt;`,
		`<a href="/ui/?range=7d">7 days</a>`, `<a href="/ui/" aria-current="page">Month to date</a>`,
	)
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("the dashboard shows a model name unescaped")
	}
	if strings.Index(body, `<span class="name">alice</span>`) > strings.Index(body, `<span class="name">bob</span>`) {
		t.Error("the top keys by spend don't start with alice")
	}

	_, body = h.do(http.MethodGet, "/ui/?range=30d&metric=requests", nil)
	has(t, "the requests of 30 days", body,
		`<p class="period">August 29 to September 27, 2026, UTC</p>`,
		`<h2>Requests</h2><p class="value">5</p><p class="note">About 0.2 a day</p>`,
		`<h2 id="chart-title">Requests by day</h2>`,
		`<a href="/ui/?metric=requests&amp;range=30d" aria-current="page">Requests</a>`,
		`<a href="/ui/?metric=requests">Month to date</a>`,
		`Top keys by requests`,
	)
	_, body = h.do(http.MethodGet, "/ui/?range=90d&metric=tokens", nil)
	has(t, "the tokens of 90 days", body, `<h2>Requests</h2><p class="value">6</p>`,
		`<h2>Tokens</h2><p class="value">4.7k</p><p class="note">4k in, 700 out</p>`, `Tokens by day`)
	_, body = h.do(http.MethodGet, "/ui/?range=forever&metric=joy", nil)
	has(t, "a page with unknown choices", body, `<h2 id="chart-title">Spend by day</h2>`,
		`<p class="period">September 1 to`)
}

func TestDashboardWithoutPrices(t *testing.T) {
	h := newHarness(t)
	h.signIn()
	_, body := h.do(http.MethodGet, "/ui/", nil)
	has(t, "an empty dashboard", body, `No requests in this range.`, `None in this range.`,
		`No key or project has a budget.`, `<h2>Cache hit rate</h2><p class="value">–</p>`)

	if err := h.st.InsertRequests(t.Context(), []store.Request{{ID: "r", Time: now, Provider: "ollama", Model: "llama",
		Status: 200, Tokens: &store.Tokens{Input: 10, Output: 5}}}); err != nil {
		t.Fatal(err)
	}
	_, body = h.do(http.MethodGet, "/ui/", nil)
	has(t, "a dashboard of unpriced requests", body, `<h2 id="chart-title">Requests by day</h2>`,
		`<h2>Spend</h2><p class="value">$0.00</p><p class="note">1 unpriced request counts as $0</p>`)
	_, body = h.do(http.MethodGet, "/ui/?metric=spend", nil)
	has(t, "the spend of unpriced requests", body, `No priced requests in this range.`)
}
