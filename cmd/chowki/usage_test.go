package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
)

func TestUsage(t *testing.T) {
	initDir(t)
	out := runOK(t, "key", "create", "--name", "alice")
	prefix := printedKeyRE.FindStringSubmatch(out)[1][:auth.PrefixLen]
	ctx := context.Background()
	st, err := store.OpenSQLite(ctx, "file:data/chowki.db")
	if err != nil {
		t.Fatal(err)
	}
	k, err := st.KeyByPrefix(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	cost, cheap := 1.25, 0.004
	if err := st.InsertRequests(ctx, []store.Request{
		{ID: "req_1", Time: day, KeyID: k.ID, ProjectID: k.ProjectID, APIFamily: "openai", Endpoint: "/v1/chat/completions",
			Provider: "openai", Model: "gpt-6-luna", Status: 200, Tokens: &store.Tokens{Input: 1200000, Output: 3000},
			CostUSD: &cost, CacheStatus: "miss", Redactions: map[string]int{"email": 2}},
		{ID: "req_2", Time: day, KeyID: k.ID, ProjectID: k.ProjectID, APIFamily: "openai", Endpoint: "/v1/chat/completions",
			Provider: "openai", Model: "gpt-6-luna", Status: 200, Tokens: &store.Tokens{Input: 10, Output: 2},
			CostUSD: &cheap, SavingsUSD: 1.25, SavingsMethod: "exact_cache", CacheStatus: "hit"},
		{ID: "req_3", Time: day, KeyID: k.ID, ProjectID: k.ProjectID, APIFamily: "openai", Endpoint: "/v1/chat/completions",
			Provider: "ollama", Model: "llama3.2", Status: 502, ErrorType: "upstream_unavailable"},
	}); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	out = runOK(t, "usage", "--from", "2026-09-01", "--to", "2026-09-30", "--by", "model")
	for _, want := range []string{
		"Usage from 2026-09-01 to 2026-09-30, in UTC",
		"Requests     3 (1 error, 0 unpriced)",
		"Cost         $1.25",
		"Savings      $1.25: exact cache $1.25",
		"Tokens       input 1,200,010, output 3,002",
		"Exact cache  1 hit, 1 miss (50% hits)",
		"Redactions   email 2",
		"MODEL              REQUESTS  COST   SAVINGS  INPUT TOKENS  OUTPUT TOKENS",
		"openai/gpt-6-luna  2         $1.25  $1.25    1,200,010     3,002",
		"ollama/llama3.2    1         $0.00  $0.00    0             0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	if out := runOK(t, "usage", "--from", "2026-09-01", "--to", "2026-09-30", "--by", "key"); !strings.Contains(out,
		"alice ("+prefix+")  3") {
		t.Errorf("the report by key lacks alice:\n%s", out)
	}
	if out := runOK(t, "usage", "--from", "2026-10-01", "--to", "2026-10-02", "--by", "day"); !strings.Contains(out,
		"Requests     0") || !strings.Contains(out, "No requests in these days.") {
		t.Errorf("an empty report:\n%s", out)
	}
	if out := runOK(t, "usage"); !strings.Contains(out, "Usage from "+time.Now().UTC().Format("2006-01")+"-01 to ") {
		t.Errorf("the default report isn't this month:\n%s", out)
	}

	for _, args := range [][]string{{"usage", "--from", "Sept"}, {"usage", "--from", "2026-09-02", "--to", "2026-09-01"},
		{"usage", "--by", "provider"}, {"usage", "extra"}, {"usage", "--bogus"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("run(%q) = %d, want %d", args, code, exitUsage)
		}
	}
}

func TestUSD(t *testing.T) {
	for v, want := range map[float64]string{0: "$0.00", 0.0000027: "$0.0000027", 0.00499: "$0.00499",
		0.005: "$0.01", 1.254: "$1.25", 1234.5: "$1234.50"} {
		if got := usd(v); got != want {
			t.Errorf("usd(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -45678: "-45,678"} {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %q, want %q", n, got, want)
		}
	}
}
