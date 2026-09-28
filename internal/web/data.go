package web

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

// page is what the dashboard shows, formatted already.
type page struct {
	CSRF            string
	Ranges, Metrics []link
	Period          string
	Measure         string // the chosen metric, as in "Top keys by spend"
	Month           string // the month that budgets cover
	Hero            tile
	Tiles           []tile
	Chart           chart
	// Keys and Models are the top keys and models by the chosen metric.
	Keys, Models []bar
	Redactions   []bar
	Savings      []row
	Budgets      []meter
}

// link is one choice of a filter.
type link struct {
	Label, Href string
	Current     bool
}

type tile struct{ Label, Value, Note string }

type row struct{ Label, Value string }

// bar is a row of a bar list: its bar is as long as its value's share of
// the list's largest.
type bar struct {
	Label, Detail, Value string
	Width                int // percent
}

// meter is a budget and what was spent of it this month.
type meter struct {
	Name, Kind, Spent, Budget string
	Width                     int // percent, at most 100
	// Status is ok, warning or critical. Label says it in words, so that
	// color is never the only sign of it.
	Status, Label string
	used          float64
}

// ranges are the periods the dashboard offers; the first is the default.
var ranges = []struct {
	name, label string
	days        int // 0 is the month to date
}{{"month", "Month to date", 0}, {"7d", "7 days", 7}, {"30d", "30 days", 30}, {"90d", "90 days", 90}}

// metrics are what the chart and the top lists can measure. Without a
// choice, the dashboard measures spend, or requests when nothing was priced.
var metrics = []struct{ name, label string }{{"spend", "Spend"}, {"requests", "Requests"}, {"tokens", "Tokens"}}

// topN is how many rows a top list shows.
const topN = 8

// period returns the range named name, or the default one, as [from, to),
// and the name it resolved to. Ranges are whole days in UTC up to now.
func period(name string, now time.Time) (from, to time.Time, resolved string) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for _, r := range ranges {
		if r.name == name && r.days > 0 {
			return today.AddDate(0, 0, 1-r.days), now, r.name
		}
	}
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), now, ranges[0].name
}

// href links to the dashboard with a range and a metric, leaving out the
// defaults.
func href(rangeName, metric string) string {
	q := url.Values{}
	if rangeName != ranges[0].name {
		q.Set("range", rangeName)
	}
	if metric != "" {
		q.Set("metric", metric)
	}
	if len(q) == 0 {
		return "/ui/"
	}
	return "/ui/?" + q.Encode()
}

func (u *UI) build(ctx context.Context, q url.Values) (*page, error) {
	now := u.now().UTC()
	from, to, rangeName := period(q.Get("range"), now)
	t, err := u.Store.Totals(ctx, from, to)
	if err != nil {
		return nil, err
	}
	chosen := q.Get("metric")
	if !slices.ContainsFunc(metrics, func(m struct{ name, label string }) bool { return m.name == chosen }) {
		chosen = ""
	}
	metric := cmp.Or(chosen, "spend")
	if chosen == "" && t.CostUSD == 0 {
		metric = "requests"
	}

	p := &page{Period: from.Format("January 2") + " to " + to.Format("January 2, 2006") + ", UTC"}
	for _, r := range ranges {
		p.Ranges = append(p.Ranges, link{r.label, href(r.name, chosen), r.name == rangeName})
	}
	for _, m := range metrics {
		p.Metrics = append(p.Metrics, link{m.label, href(rangeName, m.name), m.name == metric})
		if m.name == metric {
			p.Measure = strings.ToLower(m.label)
		}
	}
	p.Month = now.Format("January 2006")

	days, err := u.Store.Breakdown(ctx, store.ByDay, from, to)
	if err != nil {
		return nil, err
	}
	p.Chart = dayChart(days, from, to, metric)
	p.Hero, p.Tiles = tiles(t, metric, len(p.Chart.Columns))
	for _, m := range slices.Sorted(maps.Keys(t.SavingsUSD)) {
		p.Savings = append(p.Savings, row{savingsLabel(m), usd(t.SavingsUSD[m])})
	}
	p.Redactions = countBars(t.Redactions)

	keys, err := u.Store.Breakdown(ctx, store.ByKey, from, to)
	if err != nil {
		return nil, err
	}
	p.Keys = groupBars(keys, metric, func(g store.Group) (string, string) {
		if g.ID == "" {
			return "No valid key", "rejected by authentication"
		}
		return g.Label, g.ID
	})
	models, err := u.Store.Breakdown(ctx, store.ByModel, from, to)
	if err != nil {
		return nil, err
	}
	p.Models = groupBars(models, metric, func(g store.Group) (string, string) {
		if g.ID == "" {
			return "No model", "rejected before routing"
		}
		return g.ID, ""
	})

	p.Budgets, err = u.budgets(ctx, now)
	return p, err
}

// tiles returns the total of metric, the hero figure, and the tiles of
// the other totals, over a range of days.
func tiles(t store.Totals, metric string, days int) (hero tile, rest []tile) {
	var saved float64
	for _, v := range t.SavingsUSD {
		saved += v
	}
	var redactions int64
	for _, n := range t.Redactions {
		redactions += n
	}
	in, out := float64(t.Tokens.Input), float64(t.Tokens.Output)
	spend := tile{"Spend", usd(t.CostUSD), count(t.Unpriced) + " unpriced requests count as $0"}
	switch {
	case t.Requests == 0:
		spend.Note = "No requests"
	case t.Unpriced == 0:
		spend.Note = "Every request with usage had a price"
	case t.Unpriced == 1:
		spend.Note = "1 unpriced request counts as $0"
	}
	totals := map[string]tile{
		"spend":    spend,
		"requests": {"Requests", count(t.Requests), "About " + perDay(t.Requests, days) + " a day"},
		"tokens":   {"Tokens", compact(in + out), compact(in) + " in, " + compact(out) + " out"},
	}
	for _, m := range metrics {
		if m.name == metric {
			hero = totals[m.name]
		} else {
			rest = append(rest, totals[m.name])
		}
	}
	failed := tile{"Failed", count(t.Errors), "None"}
	if t.Requests > 0 {
		failed.Note = strconv.FormatFloat(100*float64(t.Errors)/float64(t.Requests), 'f', 1, 64) + "% of requests"
	}
	hits := tile{"Cache hit rate", "–", "No exact-cache lookups"}
	if n := t.CacheHits + t.CacheMisses; n > 0 {
		hits.Value = strconv.FormatFloat(100*float64(t.CacheHits)/float64(n), 'f', 0, 64) + "%"
		hits.Note = count(t.CacheHits) + " of " + plural(n, "lookup")
	}
	return hero, append(rest, failed, tile{"Net savings", usd(saved), "From caching"}, hits,
		tile{"Redactions", count(redactions), "Secrets and personal data"})
}

// perDay is the average of n over a number of days, with a decimal when
// it's small.
func perDay(n int64, days int) string {
	avg := float64(n) / float64(max(days, 1))
	if avg < 10 {
		return strconv.FormatFloat(avg, 'f', 1, 64)
	}
	return thousands(strconv.FormatFloat(avg, 'f', 0, 64))
}

func savingsLabel(method string) string {
	switch method {
	case "prompt_cache":
		return "Provider prompt caching"
	case "exact_cache":
		return "Exact cache"
	}
	return method
}

// value is what a group measures in metric.
func value(g store.Group, metric string) float64 {
	switch metric {
	case "requests":
		return float64(g.Requests)
	case "tokens":
		return float64(g.InputTokens + g.OutputTokens)
	}
	return g.CostUSD
}

func format(v float64, metric string) string {
	switch metric {
	case "requests":
		return count(int64(v))
	case "tokens":
		return compact(v)
	}
	return usd(v)
}

// groupBars lists the top groups by metric. name returns a group's label
// and what identifies it, if the label doesn't.
func groupBars(gs []store.Group, metric string, name func(store.Group) (label, id string)) []bar {
	gs = slices.Clone(gs)
	slices.SortStableFunc(gs, func(a, b store.Group) int { return cmp.Compare(value(b, metric), value(a, metric)) })
	var out []bar
	for _, g := range gs[:min(len(gs), topN)] {
		label, id := name(g)
		detail := plural(g.Requests, "request")
		if metric == "requests" {
			detail = usd(g.CostUSD)
		}
		if id != "" {
			detail = id + " · " + detail
		}
		out = append(out, bar{Label: label, Detail: detail, Value: format(value(g, metric), metric),
			Width: percent(value(g, metric), value(gs[0], metric))})
	}
	return out
}

func countBars(counts map[string]int64) []bar {
	names := slices.SortedFunc(maps.Keys(counts), func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), strings.Compare(a, b))
	})
	var out []bar
	for _, name := range names[:min(len(names), topN)] {
		out = append(out, bar{Label: name, Value: count(counts[name]),
			Width: percent(float64(counts[name]), float64(counts[names[0]]))})
	}
	return out
}

// percent is v's share of top in whole percents. A value above zero gets
// at least 1, so that its bar shows.
func percent(v, top float64) int {
	if top <= 0 || v <= 0 {
		return 0
	}
	return max(1, min(100, int(math.Round(100*v/top))))
}

// budgets returns a meter for every active key and every project that has
// a budget, the fullest first.
func (u *UI) budgets(ctx context.Context, now time.Time) ([]meter, error) {
	spend, err := u.Store.SpendByKey(ctx, store.Period(now))
	if err != nil {
		return nil, err
	}
	byKey, byProject := map[int64]float64{}, map[int64]float64{}
	for _, s := range spend {
		byKey[s.KeyID] += s.USD
		byProject[s.ProjectID] += s.USD
	}
	keys, err := u.Store.ListKeys(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := u.Store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	var out []meter
	for _, k := range keys {
		if k.BudgetUSD > 0 && !k.Revoked() {
			out = append(out, newMeter(k.Name+" · "+k.Prefix, "Key", byKey[k.ID], k.BudgetUSD))
		}
	}
	for _, p := range projects {
		if p.BudgetUSD > 0 {
			out = append(out, newMeter(p.Name, "Project", byProject[p.ID], p.BudgetUSD))
		}
	}
	slices.SortStableFunc(out, func(a, b meter) int { return cmp.Compare(b.used, a.used) })
	return out, nil
}

// newMeter shows spend against a budget, with the warning levels of the
// budget tracker: 80% and 100%.
func newMeter(name, kind string, spent, budget float64) meter {
	used := spent / budget
	m := meter{Name: name, Kind: kind, Spent: usd(spent), Budget: usd(budget), Width: percent(min(used, 1), 1),
		Status: "ok", Label: fmt.Sprintf("%.0f%% used", math.Floor(100*used)), used: used}
	switch {
	case used >= 1:
		m.Status, m.Label = "critical", "Used up: new requests are rejected"
	case used >= 0.8:
		m.Status = "warning"
	}
	return m
}

// usd formats an amount of dollars to the cent.
func usd(v float64) string {
	switch {
	case v < 0:
		return "-" + usd(-v)
	case v > 0 && v < 0.005:
		return "<$0.01"
	}
	return "$" + thousands(strconv.FormatFloat(v, 'f', 2, 64))
}

func count(n int64) string { return thousands(strconv.FormatInt(n, 10)) }

func plural(n int64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return count(n) + " " + noun + "s"
}

// compact formats a count to about three digits, such as 950, 12.3k or
// 1.5M.
func compact(v float64) string {
	units := []string{"", "k", "M", "B"}
	i := 0
	for i < len(units)-1 && math.Round(v*10)/10 >= 1000 {
		v /= 1000
		i++
	}
	if i == 0 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0") + units[i]
}

// thousands puts commas between the thousands of a non-negative number.
func thousands(s string) string {
	whole, frac, found := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if found {
		b.WriteString("." + frac)
	}
	return b.String()
}
