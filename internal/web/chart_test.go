package web

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

func TestNiceScale(t *testing.T) {
	for _, tc := range []struct {
		high      float64
		whole     bool
		top, step float64
	}{
		{0, false, 1, 1},
		{4, false, 4, 1},
		{4.1, false, 6, 2},
		{9, false, 10, 5},
		{0.3, false, 0.3, 0.1},
		{0.0004, false, 0.0004, 0.0001},
		{1234, true, 1500, 500},
		{1, true, 1, 1},
		{3, true, 3, 1},
		{0.5, true, 1, 1},
	} {
		top, step := niceScale(tc.high, tc.whole)
		if math.Abs(top-tc.top) > 1e-12 || math.Abs(step-tc.step) > 1e-12 {
			t.Errorf("niceScale(%v, %v) = %v, %v; want %v, %v", tc.high, tc.whole, top, step, tc.top, tc.step)
		}
		if steps := top / step; steps > 5.000001 || top < tc.high {
			t.Errorf("niceScale(%v, %v) = %v, %v: %v steps", tc.high, tc.whole, top, step, steps)
		}
	}
}

func TestTickLabel(t *testing.T) {
	for _, tc := range []struct {
		v, step float64
		metric  string
		want    string
	}{
		{0, 0.5, "spend", "$0.00"},
		{1.5, 0.5, "spend", "$1.50"},
		{1500, 500, "spend", "$1,500"},
		{0.0003, 0.0001, "spend", "$0.0003"},
		{1.5e6, 5e5, "tokens", "1.5M"},
		{50, 10, "requests", "50"},
	} {
		if got := tickLabel(tc.v, tc.step, tc.metric); got != tc.want {
			t.Errorf("tickLabel(%v, %v, %s) = %q, want %q", tc.v, tc.step, tc.metric, got, tc.want)
		}
	}
}

func TestDayChart(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	c := dayChart([]store.Group{{ID: "2026-09-02", Requests: 1, CostUSD: 1}, {ID: "2026-09-04", Requests: 2, CostUSD: 3}},
		from, to, "spend")
	var heights []int
	var labels []string
	for _, col := range c.Columns {
		heights = append(heights, col.Height)
		labels = append(labels, col.Label)
	}
	if c.Title != "Spend by day" || c.Empty != "" || !reflect.DeepEqual(heights, []int{0, 33, 0, 100}) ||
		!reflect.DeepEqual(c.Ticks, []string{"$3", "$2", "$1", "$0"}) ||
		!reflect.DeepEqual(labels, []string{"Sep 1", "Sep 2", "Sep 3", "Sep 4"}) || !c.Columns[1].Minor ||
		c.Columns[3].Tip != "Fri, Sep 4: 2 requests, 0 tokens, $3.00" || c.Columns[0].Spend != "$0.00" {
		t.Errorf("dayChart() = %+v", c)
	}

	start, end, _ := period("90d", to)
	c = dayChart(nil, start, end, "requests")
	var shown int
	for _, col := range c.Columns {
		if col.Label != "" {
			shown++
		}
	}
	if len(c.Columns) != 90 || shown > maxLabels || c.Empty != "No requests in this range." {
		t.Errorf("a chart of 90 empty days: %d columns, %d labels, empty %q", len(c.Columns), shown, c.Empty)
	}
	if c := dayChart(nil, from, to, "spend"); c.Empty == "" || c.Empty == "No requests in this range." {
		t.Errorf("an empty chart of spend says %q", c.Empty)
	}
}
