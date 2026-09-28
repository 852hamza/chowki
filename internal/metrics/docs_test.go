package metrics

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/testutil"
)

// TestReferenceListsEveryMetric keeps the metrics reference, which is
// written by hand, in step with the code.
func TestReferenceListsEveryMetric(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "metrics.md"))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := New().Write(&b); err != nil {
		t.Fatal(err)
	}
	types, _, err := parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	for name := range types {
		if !strings.Contains(string(data), "## `"+name+"`") {
			t.Errorf("docs/reference/metrics.md has no section for %s", name)
		}
	}
}

// TestMetricsSummary writes the summary table of the metrics reference
// from what the registry exports.
func TestMetricsSummary(t *testing.T) {
	r := New()
	r.Record(Request{Family: "openai", Provider: "openai", Model: "gpt-x", Status: 200, Cache: "miss",
		Tokens: Tokens{Input: 1, Output: 1, CacheRead: 1, CacheWrite: 1, Reasoning: 1}, CostUSD: 1, SavingsUSD: 1,
		SavingsMethod: "exact_cache", Redactions: map[string]int{"email": 1}, Upstream: time.Second,
		Overhead: time.Millisecond})
	var out strings.Builder
	if err := r.Write(&out); err != nil {
		t.Fatal(err)
	}
	types, samples, err := parse(out.String())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	help := map[string]string{}
	for line := range strings.SplitSeq(out.String(), "\n") {
		if rest, ok := strings.CutPrefix(line, "# HELP "); ok {
			name, text, _ := strings.Cut(rest, " ")
			names = append(names, name)
			help[name] = text
		}
	}
	labels := map[string]map[string]bool{}
	for _, s := range samples {
		name := s.name
		for _, suffix := range []string{"_bucket", "_sum", "_count"} {
			if base := strings.TrimSuffix(name, suffix); types[base] == "histogram" {
				name = base
			}
		}
		if labels[name] == nil {
			labels[name] = map[string]bool{}
		}
		for l := range s.labels {
			if l != "le" { // a histogram's buckets, not a label to query by
				labels[name][l] = true
			}
		}
	}
	var b strings.Builder
	b.WriteString("\n| Metric | Type | Labels | Description |\n|---|---|---|---|\n")
	for _, name := range names {
		ls := "None"
		if keys := slices.Sorted(maps.Keys(labels[name])); len(keys) > 0 {
			ls = "`" + strings.Join(keys, "`, `") + "`"
		}
		typ := types[name]
		fmt.Fprintf(&b, "| [`%s`](#%s) | %s | %s | %s |\n", name, name, strings.ToUpper(typ[:1])+typ[1:], ls,
			help[name])
	}
	b.WriteString("\n")
	testutil.CheckGenerated(t, "docs/reference/metrics.md", "summary", b.String())
}
