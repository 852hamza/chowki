package web

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
)

// seedMany adds n requests over the last 90 days, from 20 keys to 6 models,
// in order of time, as the gateway saves them.
func seedMany(t testing.TB, st store.Store, n int) {
	t.Helper()
	var keys []int64
	for i := range 20 {
		_, k, err := auth.Create(t.Context(), st, fmt.Sprintf("team-%d", i%4),
			store.Key{Name: fmt.Sprintf("key-%d", i), BudgetUSD: float64(10 * (i%3 + 1))}, now)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.ID)
	}
	models := []string{"gpt-a", "gpt-b", "claude-a", "claude-b", "llama", "gemini"}
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // test data, the same on every run
	batch := make([]store.Request, 0, 5000)
	for i := range n {
		req := store.Request{ID: fmt.Sprintf("req_%08d", i), Time: now.Add(-90 * 24 * time.Hour).Add(time.Duration(i) * 90 * 24 * time.Hour / time.Duration(n)),
			KeyID: keys[r.IntN(len(keys))], Provider: "p", Model: models[r.IntN(len(models))], Status: 200,
			Tokens: &store.Tokens{Input: r.Int64N(5000), Output: r.Int64N(1000)}, CacheStatus: "miss"}
		if req.Model != "llama" && req.Model != "gemini" {
			req.CostUSD = new(float64)
			*req.CostUSD = float64(req.Tokens.Input)*3e-6 + float64(req.Tokens.Output)*15e-6
		}
		switch r.IntN(20) {
		case 0:
			req.Status = 429
		case 1:
			req.Redactions = map[string]int{"email": 1}
		case 2, 3:
			req.CacheStatus, req.SavingsUSD, req.SavingsMethod = "hit", 0.01, "exact_cache"
		}
		if batch = append(batch, req); len(batch) == cap(batch) || i == n-1 {
			if err := st.InsertRequests(t.Context(), batch); err != nil {
				t.Fatal(err)
			}
			batch = batch[:0]
		}
	}
}

// links finds the URLs that a page loads or links to.
var links = regexp.MustCompile(`(?i)\b(?:src|href|action)\s*=\s*"([^"]*)"`)

// TestLoadsFast is the dashboard's acceptance test: with 100k requests it
// loads in under 500 ms, and everything it loads or links to is its own.
func TestLoadsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("seeds 100k requests")
	}
	h := newHarness(t)
	n := 100_000
	if raceEnabled {
		n = 10_000 // the race detector slows saving too much, and the timing isn't checked then
	}
	seedMany(t, h.st, n)
	h.signIn()
	for _, path := range []string{"/ui/", "/ui/?range=90d&metric=tokens", "/ui/requests",
		"/ui/requests?model=p/llama&status=failed"} {
		var times []time.Duration
		var body string
		for range 3 {
			start := time.Now()
			resp, b := h.do(http.MethodGet, path, nil)
			times = append(times, time.Since(start))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s = %d", path, resp.StatusCode)
			}
			body = b
		}
		slices.Sort(times)
		t.Logf("GET %s: %v", path, times)
		if times[1] > 500*time.Millisecond && !raceEnabled {
			t.Errorf("GET %s took %v, the median of 3; want under 500 ms", path, times[1])
		}
		for _, m := range links.FindAllStringSubmatch(body, -1) {
			if !strings.HasPrefix(m[1], "/ui/") {
				t.Errorf("GET %s: the page refers to %q, outside the dashboard", path, m[1])
			}
		}
	}
	// The largest download, 10,000 requests, streams in batches.
	start := time.Now()
	resp, body := h.do(http.MethodGet, "/ui/requests.csv", nil)
	took := time.Since(start)
	t.Logf("GET /ui/requests.csv: %v, %d lines", took, strings.Count(body, "\n"))
	if resp.StatusCode != http.StatusOK || strings.Count(body, "\n") != min(n, csvLimit)+1 {
		t.Errorf("GET /ui/requests.csv = %d with %d lines, want %d", resp.StatusCode, strings.Count(body, "\n"),
			min(n, csvLimit)+1)
	}
	if took > 2*time.Second && !raceEnabled {
		t.Errorf("GET /ui/requests.csv took %v; want under 2 s", took)
	}
	for _, path := range []string{"/ui/static/style.css", "/ui/static/dashboard.js"} {
		if _, body := h.do(http.MethodGet, path, nil); strings.Contains(body, "//") && strings.Contains(body, "http") ||
			strings.Contains(body, "@import") || strings.Contains(body, "url(") {
			t.Errorf("%s may load something from elsewhere", path)
		}
	}
}
