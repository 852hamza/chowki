package budget

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

// Scopes of a budget.
const (
	ScopeKey     = "key"
	ScopeProject = "project"
)

// SpendReader reads saved spend; store.Store implements it.
type SpendReader interface {
	SpendByKey(ctx context.Context, period string) ([]store.KeySpend, error)
}

// Tracker keeps what each key and project has spent in the current
// calendar month (UTC) in memory, and rejects requests once a budget is
// used up. The store saves the same spend with the request records, and
// Load reads it back, so spend survives restarts.
type Tracker struct {
	logger *slog.Logger

	mu       sync.Mutex
	period   string // the month of the counts, as store.Period formats it
	keys     map[int64]*account
	projects map[int64]*account
}

// account is what a key or project spent in the current month.
type account struct {
	spent    float64 // the cost of finished requests
	reserved float64 // the estimated cost of requests in progress
	// warned is the highest share of the budget, in percent, that has been
	// logged, and warnedBudget the budget it was a share of.
	warned       int
	warnedBudget float64
}

// Load returns a tracker that starts from the spend of the current month
// saved in st.
func Load(ctx context.Context, st SpendReader, now time.Time, logger *slog.Logger) (*Tracker, error) {
	t := &Tracker{logger: logger}
	t.reset(store.Period(now))
	spend, err := st.SpendByKey(ctx, t.period)
	if err != nil {
		return nil, fmt.Errorf("load spend: %w", err)
	}
	for _, s := range spend {
		accountOf(t.keys, s.KeyID).spent += s.USD
		accountOf(t.projects, s.ProjectID).spent += s.USD
	}
	return t, nil
}

// Ticket is an admitted request, for Settle.
type Ticket struct {
	period                   string
	key, project             int64
	keyBudget, projectBudget float64
	reserved                 float64
}

// Admit checks the budgets of key k and its project for a request that
// started at now. When neither budget is used up, counting what requests in
// progress hold, it holds the request's estimated cost until Settle and
// returns the ticket to settle it with. Otherwise it returns an
// *ExceededError.
func (t *Tracker) Admit(k store.Key, estimateUSD float64, now time.Time) (*Ticket, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	period := store.Period(now)
	t.advance(period)
	if period != t.period {
		// The request started before the month ended, and its month's
		// counts are gone. The store adds its cost to that month.
		return &Ticket{period: period}, nil
	}
	key, project := accountOf(t.keys, k.ID), accountOf(t.projects, k.ProjectID)
	for _, b := range []struct {
		scope  string
		a      *account
		budget float64
	}{{ScopeKey, key, k.BudgetUSD}, {ScopeProject, project, k.ProjectBudgetUSD}} {
		if b.budget > 0 && b.a.spent+b.a.reserved >= b.budget {
			e := &ExceededError{Scope: b.scope, BudgetUSD: b.budget, SpentUSD: b.a.spent, HeldUSD: b.a.reserved,
				Period: t.period}
			if b.scope == ScopeProject {
				e.Project = k.Project
			}
			return nil, e
		}
	}
	key.reserved += estimateUSD
	project.reserved += estimateUSD
	return &Ticket{period: period, key: k.ID, project: k.ProjectID, keyBudget: k.BudgetUSD,
		projectBudget: k.ProjectBudgetUSD, reserved: estimateUSD}, nil
}

// Settle replaces what ticket tk holds with the request's actual cost,
// which is 0 when it's unknown. It ignores a nil ticket.
func (t *Tracker) Settle(tk *Ticket, costUSD float64, now time.Time) {
	if tk == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance(store.Period(now))
	if tk.period != t.period {
		return // the month is over; the store has its spend
	}
	key, project := accountOf(t.keys, tk.key), accountOf(t.projects, tk.project)
	for _, a := range []*account{key, project} {
		a.reserved = max(0, a.reserved-tk.reserved) // float rounding mustn't leave a negative hold
		a.spent += costUSD
	}
	t.warn(key, tk.keyBudget, "key_id", tk.key)
	t.warn(project, tk.projectBudget, "project_id", tk.project)
}

// advance starts a new month's counts when period is later than the
// current one.
func (t *Tracker) advance(period string) {
	if period > t.period {
		t.reset(period)
	}
}

func (t *Tracker) reset(period string) {
	t.period, t.keys, t.projects = period, map[int64]*account{}, map[int64]*account{}
}

func accountOf(m map[int64]*account, id int64) *account {
	a := m[id]
	if a == nil {
		a = &account{}
		m[id] = a
	}
	return a
}

// warnAt are the shares of a budget, in percent and in descending order,
// at which the tracker logs a warning, once a month each.
var warnAt = []int{100, 80}

func (t *Tracker) warn(a *account, budget float64, idName string, id int64) {
	if budget <= 0 {
		return
	}
	if a.warnedBudget != budget { // a new budget has new thresholds
		a.warned, a.warnedBudget = 0, budget
	}
	for _, pct := range warnAt {
		if a.spent < budget*float64(pct)/100 {
			continue
		}
		if pct > a.warned {
			a.warned = pct
			msg := fmt.Sprintf("budget %d%% used", pct)
			if pct == 100 {
				msg = "budget used up; requests are rejected until next month or until the budget is raised"
			}
			t.logger.Warn(msg, idName, id, "budget_usd", budget, "spent_usd", a.spent, "period", t.period)
		}
		return
	}
}

// ExceededError reports a budget that is used up.
type ExceededError struct {
	Scope     string // ScopeKey or ScopeProject
	Project   string // the project name, for a project budget
	BudgetUSD float64
	SpentUSD  float64 // the cost of finished requests
	HeldUSD   float64 // the estimated cost of requests in progress
	Period    string  // the month, as store.Period formats it
}

// Error says what happened and what can be done, for the client.
func (e *ExceededError) Error() string {
	whose := "this key"
	if e.Scope == ScopeProject {
		whose = fmt.Sprintf("the project %q", e.Project)
	}
	month, _ := time.Parse("2006-01", e.Period) // Period comes from store.Period
	var b strings.Builder
	fmt.Fprintf(&b, "The monthly budget of %s, %s, is used up: %s spent in %s", whose, FormatUSD(e.BudgetUSD),
		FormatUSD(e.SpentUSD), month.Format("January 2006"))
	if e.HeldUSD > 0 {
		fmt.Fprintf(&b, ", plus %s held for requests in progress", FormatUSD(e.HeldUSD))
	}
	fmt.Fprintf(&b, ". It resets on %s at 00:00 UTC; the gateway's admin can raise it.",
		month.AddDate(0, 1, 0).Format("2006-01-02"))
	return b.String()
}

// FormatUSD formats an amount in dollars for people, in cents. An amount
// below a cent shows as "<$0.01", so that small spend doesn't look like
// none.
func FormatUSD(v float64) string {
	if v > 0 && v < 0.01 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", v)
}
