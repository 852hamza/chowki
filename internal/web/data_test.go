package web

import (
	"testing"
	"time"
)

func TestFormats(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{usd(0), "$0.00"},
		{usd(0.004), "<$0.01"},
		{usd(0.006), "$0.01"},
		{usd(1234567.891), "$1,234,567.89"},
		{usd(-2.5), "-$2.50"},
		{count(0), "0"},
		{count(999), "999"},
		{count(1000), "1,000"},
		{count(1234567), "1,234,567"},
		{compact(0), "0"},
		{compact(999), "999"},
		{compact(1234), "1.2k"},
		{compact(12345), "12.3k"},
		{compact(999950), "1M"},
		{compact(1.5e6), "1.5M"},
		{compact(2e9), "2B"},
		{compact(3e12), "3000B"},
		{plural(1, "request"), "1 request"},
		{plural(2500, "request"), "2,500 requests"},
		{perDay(5, 30), "0.2"},
		{perDay(3000, 30), "100"},
		{perDay(30000, 2), "15,000"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestPercent(t *testing.T) {
	for _, tc := range []struct {
		v, top float64
		want   int
	}{{0, 10, 0}, {5, 0, 0}, {0.001, 10, 1}, {5, 10, 50}, {10, 10, 100}, {20, 10, 100}} {
		if got := percent(tc.v, tc.top); got != tc.want {
			t.Errorf("percent(%v, %v) = %d, want %d", tc.v, tc.top, got, tc.want)
		}
	}
}

func TestPeriod(t *testing.T) {
	now := time.Date(2026, 9, 27, 15, 4, 5, 0, time.UTC)
	for name, want := range map[string]string{"": "2026-09-01 month", "month": "2026-09-01 month",
		"7d": "2026-09-21 7d", "30d": "2026-08-29 30d", "90d": "2026-06-30 90d", "1y": "2026-09-01 month"} {
		from, to, resolved := period(name, now)
		if got := from.Format(time.DateOnly) + " " + resolved; got != want || !to.Equal(now) {
			t.Errorf("period(%q) = %s to %v, want %s", name, got, to, want)
		}
	}
	for _, tc := range []struct{ rangeName, metric, want string }{
		{"month", "", "/ui/"},
		{"7d", "", "/ui/?range=7d"},
		{"month", "tokens", "/ui/?metric=tokens"},
		{"90d", "spend", "/ui/?metric=spend&range=90d"},
	} {
		if got := href(tc.rangeName, tc.metric); got != tc.want {
			t.Errorf("href(%q, %q) = %q, want %q", tc.rangeName, tc.metric, got, tc.want)
		}
	}
}

func TestNewMeter(t *testing.T) {
	for _, tc := range []struct {
		spent         float64
		status, label string
		width         int
	}{
		{0, "ok", "0% used", 0},
		{79.9, "ok", "79% used", 80},
		{80, "warning", "80% used", 80},
		{99.99, "warning", "99% used", 100},
		{100, "critical", "Used up: new requests are rejected", 100},
		{250, "critical", "Used up: new requests are rejected", 100},
	} {
		m := newMeter("k", "Key", tc.spent, 100)
		if m.Status != tc.status || m.Label != tc.label || m.Width != tc.width {
			t.Errorf("newMeter(%v of 100) = %s %q width %d; want %s %q %d", tc.spent, m.Status, m.Label, m.Width,
				tc.status, tc.label, tc.width)
		}
	}
}
