package metrics

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxSeries bounds the label sets of chowki_requests_total. Clients choose
// model names, so without a bound they could grow the registry without end;
// further label sets count under provider and model "other".
const maxSeries = 2000

// Request is what the registry learns from one finished request.
type Request struct {
	Family, Provider, Model string
	Status                  int
	Cache                   string // hit, miss, bypass, or "" before the cache
	Tokens                  Tokens
	CostUSD                 float64 // 0 when unpriced
	SavingsUSD              float64 // can be negative
	SavingsMethod           string
	Redactions              map[string]int
	// Upstream is how long the provider took, 0 when the request didn't
	// reach one; Overhead is the rest of the request's time.
	Upstream, Overhead time.Duration
}

// Tokens are the tokens that a provider reported for a request.
type Tokens struct {
	Input, Output, CacheRead, CacheWrite, Reasoning int64
}

// Registry holds the gateway's own metrics, since it started, and writes
// them in the Prometheus text format.
type Registry struct {
	mu         sync.Mutex
	requests   map[requestLabels]float64
	tokens     map[string]float64
	cost       float64
	savings    map[string]float64
	redactions map[string]float64
	upstream   histogram
	overhead   histogram
}

type requestLabels struct {
	family, provider, model, status, cache string
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{
		requests:   map[requestLabels]float64{},
		tokens:     map[string]float64{},
		savings:    map[string]float64{},
		redactions: map[string]float64{},
		upstream:   newHistogram(0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600),
		overhead:   newHistogram(0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25),
	}
}

// Record counts a finished request.
func (r *Registry) Record(q Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := requestLabels{q.Family, q.Provider, q.Model, strconv.Itoa(q.Status), q.Cache}
	if _, ok := r.requests[l]; !ok && len(r.requests) >= maxSeries {
		l.provider, l.model = "other", "other"
	}
	r.requests[l]++
	for typ, n := range map[string]int64{"input": q.Tokens.Input, "output": q.Tokens.Output,
		"cache_read": q.Tokens.CacheRead, "cache_write": q.Tokens.CacheWrite, "reasoning": q.Tokens.Reasoning} {
		if n > 0 {
			r.tokens[typ] += float64(n)
		}
	}
	r.cost += q.CostUSD
	if q.SavingsMethod != "" {
		r.savings[q.SavingsMethod] += q.SavingsUSD
	}
	for typ, n := range q.Redactions {
		r.redactions[typ] += float64(n)
	}
	if q.Upstream > 0 {
		r.upstream.observe(q.Upstream.Seconds())
	}
	r.overhead.observe(q.Overhead.Seconds())
}

// Handler serves the metrics for Prometheus to scrape.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = r.Write(w) // the scraper may be gone; nothing to do then
	})
}

// Write writes the metrics in the Prometheus text exposition format, with
// the series of each metric in a stable order.
func (r *Registry) Write(w io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := bufio.NewWriter(w)

	header(b, "chowki_requests_total", "counter",
		"Requests that the gateway finished, by API family, provider, model, HTTP status and exact cache status.")
	labels := slices.SortedFunc(maps.Keys(r.requests), func(a, b requestLabels) int {
		return cmp.Or(strings.Compare(a.family, b.family), strings.Compare(a.provider, b.provider),
			strings.Compare(a.model, b.model), strings.Compare(a.status, b.status), strings.Compare(a.cache, b.cache))
	})
	for _, l := range labels {
		sample(b, "chowki_requests_total", r.requests[l], "family", l.family, "provider", l.provider,
			"model", l.model, "status", l.status, "cache", l.cache)
	}
	header(b, "chowki_tokens_total", "counter",
		"Tokens that providers reported, by type. input counts every prompt token, cached or not.")
	for _, typ := range slices.Sorted(maps.Keys(r.tokens)) {
		sample(b, "chowki_tokens_total", r.tokens[typ], "type", typ)
	}
	header(b, "chowki_cost_usd_total", "counter", "Cost of priced requests, in US dollars.")
	sample(b, "chowki_cost_usd_total", r.cost)
	header(b, "chowki_savings_usd", "gauge",
		"Net savings since the gateway started, in US dollars, by method; they can be negative.")
	for _, m := range slices.Sorted(maps.Keys(r.savings)) {
		sample(b, "chowki_savings_usd", r.savings[m], "method", m)
	}
	header(b, "chowki_redactions_total", "counter", "Secrets and personal data that redaction found, by type.")
	for _, typ := range slices.Sorted(maps.Keys(r.redactions)) {
		sample(b, "chowki_redactions_total", r.redactions[typ], "type", typ)
	}
	r.upstream.write(b, "chowki_upstream_latency_seconds", "Time that providers took to answer, streams included.")
	r.overhead.write(b, "chowki_overhead_seconds", "Time that the gateway itself added to a request.")
	return b.Flush()
}

func header(w *bufio.Writer, name, typ, help string) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

// sample writes one series; labels are name, value pairs. A bufio.Writer
// keeps its first error for Flush, so writes don't check their own.
func sample(w *bufio.Writer, name string, v float64, labels ...string) {
	var line strings.Builder
	line.WriteString(name)
	if len(labels) > 0 {
		line.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				line.WriteByte(',')
			}
			line.WriteString(labels[i])
			line.WriteByte('=')
			quote(&line, labels[i+1])
		}
		line.WriteByte('}')
	}
	line.WriteByte(' ')
	line.WriteString(formatFloat(v))
	line.WriteByte('\n')
	_, _ = w.WriteString(line.String())
}

// quote writes a label value in quotes, with the only escapes that the
// format has: backslash, double quote and line feed. Other control
// characters are left out.
func quote(w *strings.Builder, s string) {
	w.WriteByte('"')
	for _, r := range strings.ToValidUTF8(s, "") {
		switch {
		case r == '\\':
			w.WriteString(`\\`)
		case r == '"':
			w.WriteString(`\"`)
		case r == '\n':
			w.WriteString(`\n`)
		case r >= 0x20 && r != 0x7f:
			w.WriteRune(r)
		}
	}
	w.WriteByte('"')
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// histogram counts observations in fixed buckets.
type histogram struct {
	bounds []float64 // upper bounds, ascending
	counts []uint64  // per bucket, and one more for +Inf
	sum    float64
}

func newHistogram(bounds ...float64) histogram {
	return histogram{bounds: bounds, counts: make([]uint64, len(bounds)+1)}
}

func (h *histogram) observe(v float64) {
	h.counts[sort.SearchFloat64s(h.bounds, v)]++
	h.sum += v
}

func (h *histogram) write(w *bufio.Writer, name, help string) {
	header(w, name, "histogram", help)
	var total uint64
	for i, c := range h.counts {
		total += c
		le := "+Inf"
		if i < len(h.bounds) {
			le = formatFloat(h.bounds[i])
		}
		sample(w, name+"_bucket", float64(total), "le", le)
	}
	sample(w, name+"_sum", h.sum)
	sample(w, name+"_count", float64(total))
}
