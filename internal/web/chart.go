package web

import (
	"math"
	"strconv"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

// chart is a column chart of a metric by day. The server lays it out and
// the stylesheet draws it, in HTML rather than SVG so that its text keeps
// its size at any width. The Content-Security-Policy allows no inline
// styles, so each column's height is a whole percent of the plot, set by
// a class, and the gridlines are spaced evenly, as nice ticks are.
type chart struct {
	Title   string
	Columns []column
	// Ticks label the gridlines, from the top down.
	Ticks []string
	// Empty says why there are no columns to draw, when every one is zero.
	Empty string
}

// column is one day of the chart.
type column struct {
	Height int    // percent of the plot
	Label  string // the date under the column, which only some columns show
	Minor  bool   // the label hides on narrow screens
	Tip    string // the tooltip, which screen readers read as well
	// Day, Requests, Tokens, Spend and Savings are the row of the table
	// view.
	Day, Requests, Tokens, Spend, Savings string
}

// maxLabels is how many dates at most label the x-axis.
const maxLabels = 7

// dayChart charts metric for each day in [from, to), with zero for days
// missing from groups, a breakdown by day.
func dayChart(groups []store.Group, from, to time.Time, metric string) chart {
	byDay := make(map[string]store.Group, len(groups))
	var high float64
	for _, g := range groups {
		byDay[g.ID] = g
		high = max(high, value(g, metric))
	}
	var c chart
	for _, m := range metrics {
		if m.name == metric {
			c.Title = m.label + " by day"
		}
	}
	switch {
	case high > 0:
	case metric == "spend":
		c.Empty = "No priced requests in this range. Choose Requests or Tokens to see the traffic of unpriced models."
	default:
		c.Empty = "No requests in this range."
	}
	top, step := niceScale(high, metric != "spend")
	for i := int(math.Round(top / step)); i >= 0; i-- {
		c.Ticks = append(c.Ticks, tickLabel(float64(i)*step, step, metric))
	}

	var days []time.Time
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	every := max(1, (len(days)+maxLabels-1)/maxLabels)
	for i, d := range days {
		g := byDay[d.Format(time.DateOnly)]
		col := column{Height: percent(value(g, metric), top), Day: d.Format("Mon, Jan 2"),
			Requests: count(g.Requests), Tokens: compact(float64(g.InputTokens + g.OutputTokens)),
			Spend: usd(g.CostUSD), Savings: usd(g.SavingsUSD)}
		col.Tip = col.Day + ": " + plural(g.Requests, "request") + ", " + col.Tokens + " tokens, " + col.Spend
		if i%every == 0 {
			col.Label = d.Format("Jan 2")
			col.Minor = (i/every)%2 == 1
		}
		c.Columns = append(c.Columns, col)
	}
	return c
}

// niceScale returns the top of an axis for values up to high, and the step
// between its ticks: 1, 2 or 5 times a power of ten, for up to five steps.
// Whole makes the step at least 1, for counts.
func niceScale(high float64, whole bool) (top, step float64) {
	if high <= 0 {
		return 1, 1
	}
	raw := high / 4
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	step = 10 * mag
	for _, m := range []float64{1, 2, 5} {
		// The tolerance keeps rounding errors from skipping a step.
		if m*mag >= raw*(1-1e-9) {
			step = m * mag
			break
		}
	}
	if whole {
		step = max(step, 1)
	}
	return step * math.Ceil(high/step-1e-9), step
}

// tickLabel formats an axis tick, with as many decimals as the step needs.
func tickLabel(v, step float64, metric string) string {
	if metric != "spend" {
		return compact(v)
	}
	if step >= 1 {
		return "$" + thousands(strconv.FormatFloat(v, 'f', 0, 64))
	}
	decimals := max(2, int(-math.Floor(math.Log10(step)+1e-9)))
	return "$" + thousands(strconv.FormatFloat(v, 'f', decimals, 64))
}
