package budget

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

type spendReader struct {
	spend []store.KeySpend
	err   error
	asked string
}

func (r *spendReader) SpendByKey(_ context.Context, period string) ([]store.KeySpend, error) {
	r.asked = period
	return r.spend, r.err
}

var sept = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// newTracker returns an empty tracker whose logs go to logs, when not nil.
func newTracker(t *testing.T, logs *strings.Builder) *Tracker {
	t.Helper()
	if logs == nil {
		logs = &strings.Builder{}
	}
	tr, err := Load(t.Context(), &spendReader{}, sept, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// exceeded returns the scope of the budget that Admit reports as used up,
// or "" when it admits the request.
func exceeded(t *testing.T, tr *Tracker, k store.Key, now time.Time) string {
	t.Helper()
	tk, err := tr.Admit(k, 0, now)
	var e *ExceededError
	switch {
	case errors.As(err, &e):
		return e.Scope
	case err != nil || tk == nil:
		t.Fatalf("Admit() = %v, %v", tk, err)
	}
	tr.Settle(tk, 0, now)
	return ""
}

func TestBudgets(t *testing.T) {
	tests := []struct {
		name               string
		keyBudget, project float64
		spent              float64
		want               string
	}{
		{"no budgets", 0, 0, 1000, ""},
		{"under the key budget", 10, 0, 9.99, ""},
		{"key budget used up", 10, 0, 10, ScopeKey},
		{"project budget used up", 0, 5, 5.5, ScopeProject},
		{"the tighter budget wins", 100, 5, 6, ScopeProject},
		{"both used up reports the key", 5, 5, 6, ScopeKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := newTracker(t, nil)
			k := store.Key{ID: 1, ProjectID: 2, Project: "team", BudgetUSD: tt.keyBudget, ProjectBudgetUSD: tt.project}
			tk, err := tr.Admit(k, 0, sept)
			if err != nil {
				t.Fatalf("first Admit() error = %v", err)
			}
			tr.Settle(tk, tt.spent, sept)
			if got := exceeded(t, tr, k, sept); got != tt.want {
				t.Errorf("exceeded budget = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProjectSpendAddsUpItsKeys(t *testing.T) {
	tr := newTracker(t, nil)
	alice := store.Key{ID: 1, ProjectID: 9, ProjectBudgetUSD: 10}
	bob := store.Key{ID: 2, ProjectID: 9, ProjectBudgetUSD: 10}
	other := store.Key{ID: 3, ProjectID: 8, ProjectBudgetUSD: 10}
	for _, k := range []store.Key{alice, bob} {
		tk, err := tr.Admit(k, 0, sept)
		if err != nil {
			t.Fatal(err)
		}
		tr.Settle(tk, 5, sept)
	}
	if got := exceeded(t, tr, alice, sept); got != ScopeProject {
		t.Errorf("after 2 × $5 in a $10 project: exceeded = %q, want project", got)
	}
	if got := exceeded(t, tr, other, sept); got != "" {
		t.Errorf("another project: exceeded = %q, want none", got)
	}
}

// Requests in progress hold their estimated cost, so parallel requests
// can't all slip under a budget that is nearly used up.
func TestHolds(t *testing.T) {
	tr := newTracker(t, nil)
	k := store.Key{ID: 1, ProjectID: 1, BudgetUSD: 1}
	first, err := tr.Admit(k, 0.6, sept)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Admit(k, 0.6, sept); err != nil {
		t.Fatalf("second Admit() error = %v; $0.60 held of $1 leaves room", err)
	}
	_, err = tr.Admit(k, 0.6, sept)
	var e *ExceededError
	if !errors.As(err, &e) || e.SpentUSD != 0 || e.HeldUSD != 1.2 {
		t.Fatalf("third Admit() error = %#v; want exceeded with $1.20 held", err)
	}
	// The actual cost replaces the estimate.
	tr.Settle(first, 0.1, sept)
	if got := exceeded(t, tr, k, sept); got != "" {
		t.Errorf("after settling at $0.10: exceeded = %q, want none ($0.10 spent, $0.60 held)", got)
	}
	tr.Settle(nil, 1, sept) // a request that was never admitted
}

func TestNewMonth(t *testing.T) {
	tr := newTracker(t, nil)
	k := store.Key{ID: 1, ProjectID: 1, BudgetUSD: 1}
	tk, err := tr.Admit(k, 0, sept)
	if err != nil {
		t.Fatal(err)
	}
	tr.Settle(tk, 2, sept)
	inFlight, err := tr.Admit(store.Key{ID: 1, ProjectID: 1}, 0, sept) // no budget, to get a ticket
	if err != nil {
		t.Fatal(err)
	}
	if got := exceeded(t, tr, k, sept); got != ScopeKey {
		t.Fatalf("September: exceeded = %q, want key", got)
	}

	oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if got := exceeded(t, tr, k, oct); got != "" {
		t.Fatalf("October: exceeded = %q, want none", got)
	}
	// A request from September that ends in October counts in September,
	// where the store saves its cost, and not in October.
	tr.Settle(inFlight, 5, oct)
	// A request that started in September but reaches the budget check
	// after an October request is admitted without a check.
	late, err := tr.Admit(k, 3, sept)
	if err != nil {
		t.Fatalf("Admit() of a September request in October: error = %v", err)
	}
	tr.Settle(late, 3, oct)
	if got := exceeded(t, tr, k, oct); got != "" {
		t.Errorf("October after settling September requests: exceeded = %q, want none", got)
	}
}

func TestLoad(t *testing.T) {
	r := &spendReader{spend: []store.KeySpend{{KeyID: 1, ProjectID: 7, USD: 3}, {KeyID: 2, ProjectID: 7, USD: 4}}}
	tr, err := Load(t.Context(), r, sept, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if r.asked != "2026-09" {
		t.Errorf("Load() read the spend of %q, want 2026-09", r.asked)
	}
	if got := exceeded(t, tr, store.Key{ID: 1, ProjectID: 7, BudgetUSD: 3}, sept); got != ScopeKey {
		t.Errorf("key with $3 of $3 spent: exceeded = %q, want key", got)
	}
	if got := exceeded(t, tr, store.Key{ID: 2, ProjectID: 7, ProjectBudgetUSD: 7}, sept); got != ScopeProject {
		t.Errorf("project with $7 of $7 spent: exceeded = %q, want project", got)
	}

	if _, err := Load(t.Context(), &spendReader{err: errors.New("disk on fire")}, sept, nil); err == nil ||
		!strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("Load() with a failing store: error = %v", err)
	}
}

func TestWarnings(t *testing.T) {
	var logs strings.Builder
	tr := newTracker(t, &logs)
	k := store.Key{ID: 4, ProjectID: 1, BudgetUSD: 10}
	spend := func(usd float64) {
		t.Helper()
		tk, err := tr.Admit(k, 0, sept)
		if err != nil {
			t.Fatal(err)
		}
		tr.Settle(tk, usd, sept)
	}
	count := func(msg string) int { return strings.Count(logs.String(), msg) }

	spend(7.9)
	if logs.Len() != 0 {
		t.Errorf("warned below 80%%:\n%s", logs.String())
	}
	spend(0.1)
	spend(1)
	if count("budget 80% used") != 1 || !strings.Contains(logs.String(), "key_id=4") {
		t.Errorf("want one 80%% warning for key 4:\n%s", logs.String())
	}
	spend(1)
	if count("budget used up") != 1 {
		t.Errorf("want one warning at 100%%:\n%s", logs.String())
	}
	// A raised budget has its own thresholds.
	k.BudgetUSD = 12
	spend(0)
	if count("budget 80% used") != 2 || count("budget used up") != 1 {
		t.Errorf("want a second 80%% warning after the budget rose to $12:\n%s", logs.String())
	}
}

func TestExceededErrorMessage(t *testing.T) {
	tests := []struct {
		err  ExceededError
		want string
	}{
		{ExceededError{Scope: ScopeKey, BudgetUSD: 50, SpentUSD: 50.123, Period: "2026-09"},
			"The monthly budget of this key, $50.00, is used up: $50.12 spent in September 2026. " +
				"It resets on 2026-10-01 at 00:00 UTC; the gateway's admin can raise it."},
		{ExceededError{Scope: ScopeProject, Project: "team", BudgetUSD: 5, SpentUSD: 4.9, HeldUSD: 0.2, Period: "2026-12"},
			`The monthly budget of the project "team", $5.00, is used up: $4.90 spent in December 2026, ` +
				"plus $0.20 held for requests in progress. It resets on 2027-01-01 at 00:00 UTC; " +
				"the gateway's admin can raise it."},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("Error() =\n%s\nwant\n%s", got, tt.want)
		}
	}
}

func TestFormatUSD(t *testing.T) {
	for v, want := range map[float64]string{0: "$0.00", 0.004: "<$0.01", 0.01: "$0.01", 12.345: "$12.35", 1500: "$1500.00"} {
		if got := FormatUSD(v); got != want {
			t.Errorf("FormatUSD(%v) = %s, want %s", v, got, want)
		}
	}
}

func TestConcurrentRequests(t *testing.T) {
	tr := newTracker(t, nil)
	k := store.Key{ID: 1, ProjectID: 1, BudgetUSD: 1000}
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			for range 20 {
				tk, err := tr.Admit(k, 0.5, sept)
				if err != nil {
					t.Error(err)
					return
				}
				tr.Settle(tk, 0.25, sept)
			}
		})
	}
	wg.Wait()
	a := tr.keys[1]
	if a.spent != 250 || a.reserved != 0 {
		t.Errorf("after 1000 requests of $0.25: spent %v, held %v; want 250 and 0", a.spent, a.reserved)
	}
}
