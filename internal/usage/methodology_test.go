package usage

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/catalog"
	"github.com/852hamza/chowki/internal/testutil"
)

// TestSavingsExample writes the worked example of the savings methodology
// from the catalog's prices and Compute, so that the page can't drift from
// either.
func TestSavingsExample(t *testing.T) {
	cat, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	const model = "claude-sonnet-5"
	m, ok := cat.Find("anthropic", model)
	if !ok {
		t.Fatalf("the catalog has no anthropic/%s", model)
	}
	at := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	p := m.PriceFor(0, at)
	usd := func(v float64) string {
		if v < 0 {
			return "−$" + fmt.Sprintf("%.6f", -v)
		}
		return "$" + fmt.Sprintf("%.6f", v)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nPrices of `anthropic/%s`, per million tokens: input $%g, output $%g,\n"+
		"5-minute cache write $%g, cache read $%g.\n\n", model, p.Input, p.Output, *p.CacheWrite, *p.CacheRead)
	b.WriteString("| Request | Input tokens | Written to the cache | Read from the cache | Output tokens | Cost | " +
		"Savings |\n|---|---|---|---|---|---|---|\n")
	var total, saved, second float64
	for _, r := range []struct {
		what  string
		usage Usage
	}{
		{"1. Writes the prefix to the provider's cache", Usage{Input: 52000, CacheWrite: 50000, Output: 500}},
		{"2. Reads the prefix from the cache", Usage{Input: 52000, CacheRead: 50000, Output: 500}},
	} {
		c := Compute(Anthropic, Report{Usage: &r.usage}, m, at)
		if c.USD == nil {
			t.Fatalf("%s: unpriced: %s", r.what, c.Reason)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", r.what, commas(r.usage.Input), commas(r.usage.CacheWrite),
			commas(r.usage.CacheRead), commas(r.usage.Output), usd(*c.USD), usd(c.SavingsUSD))
		total, saved, second = total+*c.USD, saved+c.SavingsUSD, *c.USD
	}
	// An exact-cache hit costs nothing and saves what the answer cost.
	fmt.Fprintf(&b, "| 3. Repeats request 2, answered from Chowki's exact cache | 0 | 0 | 0 | 0 | %s | %s |\n",
		usd(0), usd(second))
	fmt.Fprintf(&b, "| Total | | | | | %s | %s |\n\n", usd(total), usd(saved+second))
	testutil.CheckGenerated(t, "docs/concepts/savings-methodology.md", "example", b.String())
}

func commas(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
