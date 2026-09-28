package store

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestListRequests(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p, _ := s.EnsureProject(ctx, "team")
	alice, err := s.CreateKey(ctx, Key{ProjectID: p.ID, Name: "alice", Prefix: "chowki_alice", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.CreateKey(ctx, Key{ProjectID: p.ID, Name: "bob", Prefix: "chowki_bob", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	// Seven requests in the same millisecond, so that paging must order them
	// by ID too, and three older ones.
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var rs []Request
	for i := range 7 {
		rs = append(rs, Request{ID: fmt.Sprintf("same-%d", i), Time: at, KeyID: alice.ID, Provider: "gemini",
			Model: "gemini-2.5-flash", Status: 200})
	}
	rs = append(rs,
		Request{ID: "old-1", Time: at.Add(-time.Minute), KeyID: bob.ID, Provider: "gemini", Model: "gemini-3.6-flash",
			Status: 503, ErrorType: "upstream_503"},
		Request{ID: "old-2", Time: at.Add(-2 * time.Minute), KeyID: bob.ID, Provider: "openai", Model: "gpt-6-luna",
			Status: 200},
		Request{ID: "old-3", Time: at.Add(-3 * time.Minute), KeyID: alice.ID, Status: 400, ErrorType: "invalid_request"},
	)
	if err := s.InsertRequests(ctx, rs); err != nil {
		t.Fatal(err)
	}
	ids := func(f RequestFilter) []string {
		t.Helper()
		got, err := s.ListRequests(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range got {
			out = append(out, r.ID)
		}
		return out
	}

	// Pages of three walk every request once, newest first.
	var all []string
	f := RequestFilter{Limit: 3}
	for range 10 {
		page, err := s.ListRequests(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range page {
			all = append(all, r.ID)
		}
		if len(page) < f.Limit {
			break
		}
		last := page[len(page)-1]
		f.Before = &RequestCursor{Time: last.Time, ID: last.ID}
	}
	want := []string{"same-6", "same-5", "same-4", "same-3", "same-2", "same-1", "same-0", "old-1", "old-2", "old-3"}
	if !slices.Equal(all, want) {
		t.Errorf("pages = %v, want %v", all, want)
	}

	for name, tt := range map[string]struct {
		f    RequestFilter
		want []string
	}{
		"key":       {RequestFilter{KeyID: bob.ID, Limit: 10}, []string{"old-1", "old-2"}},
		"model":     {RequestFilter{Provider: "gemini", Model: "gemini-3.6-flash", Limit: 10}, []string{"old-1"}},
		"failed":    {RequestFilter{Status: StatusFailed, Limit: 10}, []string{"old-1", "old-3"}},
		"succeeded": {RequestFilter{KeyID: bob.ID, Status: StatusSucceeded, Limit: 10}, []string{"old-2"}},
		"limit":     {RequestFilter{Limit: 2}, []string{"same-6", "same-5"}},
	} {
		if got := ids(tt.f); !slices.Equal(got, tt.want) {
			t.Errorf("%s: ListRequests = %v, want %v", name, got, tt.want)
		}
	}
}

func TestProviderStats(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var rs []Request
	add := func(provider string, status int, errType string, latency time.Duration) {
		rs = append(rs, Request{ID: fmt.Sprintf("r%d", len(rs)), Time: at, KeyID: 1, Provider: provider,
			Status: status, ErrorType: errType, Latency: latency})
	}
	// gemini: 20 successes of 100 ms to 2 s, and a failure of each kind.
	for i := 1; i <= 20; i++ {
		add("gemini", 200, "", time.Duration(i)*100*time.Millisecond)
	}
	add("gemini", 503, "upstream_503", 3750*time.Millisecond)
	add("gemini", 429, "upstream_429", time.Second)
	add("gemini", 502, "upstream_unavailable", time.Second)
	add("gemini", 200, "upstream_stream_error", 5*time.Second)
	// Not a failure of the provider: a request that it rejected.
	add("gemini", 400, "upstream_400", time.Second)
	// Never reached it: the gateway's own refusal, and an answer from its cache.
	add("gemini", 402, "budget_exceeded", 0)
	rs = append(rs, Request{ID: "hit", Time: at, KeyID: 1, Provider: "gemini", Status: 200, CacheStatus: "hit"})
	// openai: one success; a request without a provider, rejected before routing, is left out.
	add("openai", 200, "", 80*time.Millisecond)
	add("", 401, "invalid_api_key", 0)
	// Outside the range.
	rs = append(rs, Request{ID: "late", Time: at.Add(48 * time.Hour), KeyID: 1, Provider: "openai", Status: 200})
	if err := s.InsertRequests(ctx, rs); err != nil {
		t.Fatal(err)
	}
	got, err := s.ProviderStats(ctx, at.Add(-time.Hour), at.Add(time.Hour))
	want := []ProviderStat{
		{Provider: "gemini", Requests: 25, Failed: 4, P50: time.Second, P95: 1900 * time.Millisecond},
		{Provider: "openai", Requests: 1, P50: 80 * time.Millisecond, P95: 80 * time.Millisecond},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ProviderStats() =\n%+v, %v\nwant\n%+v", got, err, want)
	}
	if got, err := s.ProviderStats(ctx, at.Add(time.Hour), at.Add(2*time.Hour)); err != nil || len(got) != 0 {
		t.Errorf("ProviderStats() of an empty range = %+v, %v", got, err)
	}
}
