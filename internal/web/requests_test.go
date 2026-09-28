package web

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

// alicePrefix returns the prefix of the key alice that seed creates.
func alicePrefix(t *testing.T, st store.Store) string {
	t.Helper()
	keys, err := st.ListKeys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if k.Name == "alice" {
			return k.Prefix
		}
	}
	t.Fatal("no key alice")
	return ""
}

func TestRequestsPage(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.do(http.MethodGet, "/ui/requests", nil); resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/ui/login" {
		t.Fatalf("GET /ui/requests signed out = %d %s, want the sign-in page", resp.StatusCode,
			resp.Header.Get("Location"))
	}
	seed(t, h.st)
	h.signIn()
	alice := alicePrefix(t, h.st)

	_, body := h.do(http.MethodGet, "/ui/requests", nil)
	has(t, "the requests page", body,
		`<a href="/ui/requests" aria-current="page">Requests</a>`,
		`<th scope="row" role="rowheader">2026-09-27 11:00:00</th>`,
		`<span class="name">alice</span><span class="detail">`+alice+`</span>`,
		`<span class="name">gpt-x</span><span class="detail">openai</span>`,
		`<td role="cell" data-label="Tokens">1,000 in · 200 out</td><td role="cell" data-label="Cost">$8.50</td>`,
		`<td role="cell" class="text" data-label="Cache">miss</td>`,
		`<span class="flag critical">`, `<span>500</span></span><span class="detail">upstream_500</span>`,
		`<span class="name">No valid key</span>`, `<td role="cell" class="text" data-label="Redactions">email 2</td>`,
		`&lt;script&gt;alert(1)&lt;/script&gt;`,
		`<a class="button quiet" href="/ui/requests.csv" download>Download CSV</a>`,
	)
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("the requests page shows a model name unescaped")
	}

	for name, tt := range map[string]struct {
		query    url.Values
		has, not []string
	}{
		"key": {url.Values{"key": {alice}}, []string{`<option value="` + alice + `" selected>alice · ` + alice + `</option>`},
			[]string{`<span class="name">claude</span>`}},
		"model": {url.Values{"model": {"anthropic/claude"}}, []string{`<span class="name">claude</span>`,
			`<option value="anthropic/claude" selected>anthropic/claude</option>`}, []string{"gpt-x</span>"}},
		"failed": {url.Values{"status": {"failed"}}, []string{"upstream_500", "No valid key"}, []string{"gpt-x</span>"}},
		"provider": {url.Values{"provider": {"ollama"}}, []string{`Only the provider <strong>ollama</strong>`,
			`<input type="hidden" name="provider" value="ollama">`, `<a href="/ui/requests">Show every provider</a>`},
			[]string{"gpt-x</span>"}},
		"unknown key": {url.Values{"key": {"chowki_nothing"}}, []string{"No request matches these filters."}, nil},
		"bad model":   {url.Values{"model": {"no-provider"}}, []string{"No request matches these filters."}, nil},
	} {
		_, body := h.do(http.MethodGet, "/ui/requests?"+tt.query.Encode(), nil)
		has(t, name, body, tt.has...)
		for _, not := range tt.not {
			if strings.Contains(body, not) {
				t.Errorf("%s: the page has %q", name, not)
			}
		}
	}
}

// Pages of requests link to the next one, which goes on where the first
// ended, until the last.
func TestRequestsPages(t *testing.T) {
	h := newHarness(t)
	h.signIn()
	var rs []store.Request
	for i := range pageSize + 5 {
		rs = append(rs, store.Request{ID: fmt.Sprintf("r%03d", i), Time: now.Add(-time.Duration(i) * time.Second),
			KeyID: 1, Provider: "openai", Model: "gpt-x", Status: 200})
	}
	if err := h.st.InsertRequests(t.Context(), rs); err != nil {
		t.Fatal(err)
	}
	_, first := h.do(http.MethodGet, "/ui/requests", nil)
	rows := strings.Count(first, `<th scope="row" role="rowheader">`)
	i := strings.Index(first, `<p class="pager"><a class="button quiet" href="`)
	if rows != pageSize || i < 0 {
		t.Fatalf("the first page has %d rows and pager at %d, want %d and a link", rows, i, pageSize)
	}
	older := first[i+len(`<p class="pager"><a class="button quiet" href="`):]
	older = strings.ReplaceAll(older[:strings.Index(older, `"`)], "&amp;", "&")
	_, second := h.do(http.MethodGet, older, nil)
	if n := strings.Count(second, `<th scope="row" role="rowheader">`); n != 5 || strings.Contains(second, `class="pager"`) {
		t.Errorf("the second page (%s) has %d rows, want the last 5 and no pager", older, n)
	}
}

func TestRequestsCSV(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.do(http.MethodGet, "/ui/requests.csv", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET /ui/requests.csv signed out = %d, want a redirect", resp.StatusCode)
	}
	seed(t, h.st)
	// A client names the model, which a spreadsheet mustn't run as a formula.
	if err := h.st.InsertRequests(t.Context(), []store.Request{{ID: "evil", Time: now.Add(-time.Minute), KeyID: 1,
		Provider: "openai", Model: `=HYPERLINK("https://evil.example","x")`, Status: 200}}); err != nil {
		t.Fatal(err)
	}
	h.signIn()
	resp, body := h.do(http.MethodGet, "/ui/requests.csv?provider=openai", nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/csv; charset=utf-8" ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), `attachment; filename="chowki-requests-`) {
		t.Fatalf("GET /ui/requests.csv = %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"),
			resp.Header.Get("Content-Disposition"))
	}
	records, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 5 || records[0][0] != "time" || records[0][5] != "model" || len(records[0]) != 22 {
		t.Fatalf("CSV =\n%s\nwant a header and the 4 openai requests of this month and last", body)
	}
	got := map[string][]string{}
	for _, r := range records[1:] {
		got[r[1]] = r
	}
	if r := got["evil"]; r == nil || r[5] != `'=HYPERLINK("https://evil.example","x")` {
		t.Errorf("the formula model is %q, want it prefixed with an apostrophe", r)
	}
	if r := got["r1"]; r == nil || r[2] != "alice" || r[8] != "200" || r[12] != "1000" || r[17] != "8.5" ||
		r[20] != "miss" {
		t.Errorf("r1 = %q", r)
	}
}

func TestProvidersCard(t *testing.T) {
	h := newHarness(t)
	seed(t, h.st)
	h.signIn()
	_, body := h.do(http.MethodGet, "/ui/", nil)
	has(t, "the dashboard", body,
		`<h2 id="providers-title">Providers</h2>`,
		`<a href="/ui/requests?provider=openai">openai</a></th><td>1</td>`,
		`<a class="flag critical" href="/ui/requests?provider=ollama&amp;status=failed">`,
		`<span>1 · 100.0%</span>`,
	)
}
