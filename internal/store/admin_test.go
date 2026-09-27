package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestAdminTokens(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	created := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	tok, err := s.CreateAdminToken(ctx, AdminToken{Name: "ops", Prefix: "chowki_admin_abcde", Hash: [32]byte{7},
		CreatedAt: created})
	if err != nil || tok.ID == 0 || tok.Name != "ops" || tok.Hash != [32]byte{7} || !tok.CreatedAt.Equal(created) {
		t.Fatalf("CreateAdminToken() = %+v, %v", tok, err)
	}
	if _, err := s.CreateAdminToken(ctx, AdminToken{Name: "x", Prefix: "chowki_admin_abcde"}); !errors.Is(err, ErrExists) {
		t.Errorf("CreateAdminToken() with a used prefix: error = %v, want ErrExists", err)
	}
	if _, err := s.AdminTokenByPrefix(ctx, "chowki_admin_zzzzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("AdminTokenByPrefix() of an unknown token: error = %v", err)
	}
	r, err := s.RevokeAdminToken(ctx, "chowki_admin_abcde", created.Add(time.Hour))
	if err != nil || !r.Revoked() {
		t.Errorf("RevokeAdminToken() = %+v, %v", r, err)
	}
	if list, err := s.ListAdminTokens(ctx); err != nil || len(list) != 1 || !list[0].Revoked() {
		t.Errorf("ListAdminTokens() = %+v, %v", list, err)
	}
}

func TestReports(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p, _ := s.EnsureProject(ctx, "team")
	k, err := s.CreateKey(ctx, Key{ProjectID: p.ID, Name: "alice", Prefix: "chowki_alice", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	day1 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	rs := []Request{
		{ID: "r1", Time: day1, KeyID: k.ID, Provider: "openai", Model: "a", Status: 200, CostUSD: usd(1),
			Tokens: &Tokens{Input: 100, Output: 10}, CacheStatus: "miss", Redactions: map[string]int{"email": 2}},
		{ID: "r2", Time: day2, KeyID: k.ID, Provider: "openai", Model: "a", Status: 200, CostUSD: usd(0),
			SavingsUSD: 1, SavingsMethod: "exact_cache", CacheStatus: "hit"},
		{ID: "r3", Time: day2, KeyID: k.ID, Provider: "anthropic", Model: "b", Status: 200, CostUSD: usd(3),
			Tokens: &Tokens{Input: 50, Output: 5, CacheRead: 40}, SavingsUSD: -0.5, SavingsMethod: "prompt_cache",
			Redactions: map[string]int{"email": 1, "phone": 1}},
		{ID: "r4", Time: day2, KeyID: 99, Status: 401, ErrorType: "invalid_api_key"},
		{ID: "old", Time: day1.AddDate(0, -1, 0), KeyID: k.ID, Status: 200, CostUSD: usd(100)},
	}
	if err := s.InsertRequests(ctx, rs); err != nil {
		t.Fatal(err)
	}
	from, to := day1, day2.Add(time.Hour)

	got, err := s.Totals(ctx, from, to)
	want := Totals{Requests: 4, Errors: 1, CostUSD: 4, SavingsUSD: map[string]float64{"exact_cache": 1, "prompt_cache": -0.5},
		Tokens: Tokens{Input: 150, Output: 15, CacheRead: 40}, CacheHits: 1, CacheMisses: 1,
		Redactions: map[string]int64{"email": 3, "phone": 1}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Totals() =\n%+v, %v\nwant\n%+v", got, err, want)
	}

	for by, want := range map[string][]Group{
		ByKey: {{ID: "chowki_alice", Label: "alice", Requests: 3, CostUSD: 4, SavingsUSD: 0.5, InputTokens: 150,
			OutputTokens: 15}, {Requests: 1}},
		ByModel: {{ID: "anthropic/b", Requests: 1, CostUSD: 3, SavingsUSD: -0.5, InputTokens: 50, OutputTokens: 5},
			{ID: "openai/a", Requests: 2, CostUSD: 1, SavingsUSD: 1, InputTokens: 100, OutputTokens: 10},
			{Requests: 1}},
		ByDay: {{ID: "2026-09-01", Requests: 1, CostUSD: 1, InputTokens: 100, OutputTokens: 10},
			{ID: "2026-09-02", Requests: 3, CostUSD: 3, SavingsUSD: 0.5, InputTokens: 50, OutputTokens: 5}},
	} {
		if got, err := s.Breakdown(ctx, by, from, to); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Breakdown(%s) =\n%+v, %v\nwant\n%+v", by, got, err, want)
		}
	}
	if _, err := s.Breakdown(ctx, "project", from, to); err == nil {
		t.Error("Breakdown() accepted an unknown grouping")
	}

	recent, err := s.RecentRequests(ctx, to, 2)
	if err != nil || len(recent) != 2 || recent[0].Time.Before(recent[1].Time) {
		t.Fatalf("RecentRequests() = %+v, %v", recent, err)
	}
	var r3 Request
	for _, r := range recent {
		if r.ID == "r3" {
			r3 = r
		}
	}
	if r3.Tokens == nil || r3.Tokens.CacheRead != 40 || r3.CostUSD == nil || *r3.CostUSD != 3 ||
		!reflect.DeepEqual(r3.Redactions, map[string]int{"email": 1, "phone": 1}) || r3.SavingsMethod != "prompt_cache" {
		t.Errorf("RecentRequests() read r3 as %+v", r3)
	}
}
