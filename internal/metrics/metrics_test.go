package metrics

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// sampleLine is one series of the text exposition format.
type sampleLine struct {
	name   string
	labels map[string]string
	value  float64
}

var (
	metricNameRE = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelNameRE  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// parse reads the Prometheus text exposition format, version 0.0.4, and
// fails on anything the format doesn't allow: it's our own parser, so
// that the tests don't need a Prometheus library.
func parse(text string) (types map[string]string, samples []sampleLine, err error) {
	types = map[string]string{}
	if !strings.HasSuffix(text, "\n") {
		return nil, nil, errors.New("the last line doesn't end with a line feed")
	}
	for i, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		fail := func(msg string) error { return fmt.Errorf("line %d %q: %s", i+1, line, msg) }
		if rest, ok := strings.CutPrefix(line, "# "); ok {
			kind, rest, _ := strings.Cut(rest, " ")
			name, arg, _ := strings.Cut(rest, " ")
			if !metricNameRE.MatchString(name) {
				return nil, nil, fail("bad metric name")
			}
			switch kind {
			case "TYPE":
				if !strings.Contains(" counter gauge histogram summary untyped ", " "+arg+" ") || types[name] != "" {
					return nil, nil, fail("bad or repeated TYPE")
				}
				types[name] = arg
			case "HELP":
			default:
				return nil, nil, fail("unknown comment")
			}
			continue
		}
		s := sampleLine{labels: map[string]string{}}
		end := strings.IndexAny(line, "{ ")
		if end < 0 {
			return nil, nil, fail("no value")
		}
		s.name, line = line[:end], line[end:]
		if !metricNameRE.MatchString(s.name) {
			return nil, nil, fail("bad metric name")
		}
		if strings.HasPrefix(line, "{") {
			if line, err = parseLabels(line[1:], s.labels); err != nil {
				return nil, nil, fail(err.Error())
			}
		}
		value, ok := strings.CutPrefix(line, " ")
		if !ok {
			return nil, nil, fail("no space before the value")
		}
		if s.value, err = strconv.ParseFloat(value, 64); err != nil {
			return nil, nil, fail("bad value")
		}
		family := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(s.name, "_bucket"), "_sum"), "_count")
		if types[s.name] == "" && types[family] != "histogram" {
			return nil, nil, fail("a sample without a TYPE line")
		}
		samples = append(samples, s)
	}
	return types, samples, nil
}

// parseLabels reads name="value" pairs up to the closing brace, and
// returns the rest of the line.
func parseLabels(s string, into map[string]string) (string, error) {
	for {
		if rest, ok := strings.CutPrefix(s, "}"); ok {
			return rest, nil
		}
		name, rest, ok := strings.Cut(s, `="`)
		if !ok || !labelNameRE.MatchString(name) {
			return "", errors.New("bad label name")
		}
		var value strings.Builder
		for {
			if rest == "" {
				return "", errors.New("unterminated label value")
			}
			c := rest[0]
			rest = rest[1:]
			if c == '"' {
				break
			}
			if c == '\\' {
				if rest == "" {
					return "", errors.New("bad escape")
				}
				switch rest[0] {
				case '\\', '"':
					value.WriteByte(rest[0])
				case 'n':
					value.WriteByte('\n')
				default:
					return "", errors.New("bad escape")
				}
				rest = rest[1:]
				continue
			}
			if c == '\n' {
				return "", errors.New("raw line feed in a label value")
			}
			value.WriteByte(c)
		}
		into[name] = value.String()
		s = strings.TrimPrefix(rest, ",")
	}
}

func example() *Registry {
	r := New()
	r.Record(Request{Family: "openai", Provider: "openai", Model: "gpt-test", Status: 200, Cache: "miss",
		Tokens: Tokens{Input: 100, Output: 20, CacheRead: 60}, CostUSD: 0.5, SavingsUSD: 0.25,
		SavingsMethod: "prompt_cache", Upstream: 2 * time.Second, Overhead: 3 * time.Millisecond})
	r.Record(Request{Family: "openai", Provider: "openai", Model: "gpt-test", Status: 200, Cache: "hit",
		SavingsUSD: 0.5, SavingsMethod: "exact_cache", Overhead: time.Millisecond})
	r.Record(Request{Family: "anthropic", Status: 401, Redactions: map[string]int{"email": 2},
		Overhead: 200 * time.Microsecond})
	return r
}

// AC: metrics follow the Prometheus text format, checked against golden
// output and a parser of our own.
func TestWrite(t *testing.T) {
	var b strings.Builder
	if err := example().Write(&b); err != nil {
		t.Fatal(err)
	}
	const want = `# HELP chowki_requests_total Requests that the gateway finished, by API family, provider, model, HTTP status and exact cache status.
# TYPE chowki_requests_total counter
chowki_requests_total{family="anthropic",provider="",model="",status="401",cache=""} 1
chowki_requests_total{family="openai",provider="openai",model="gpt-test",status="200",cache="hit"} 1
chowki_requests_total{family="openai",provider="openai",model="gpt-test",status="200",cache="miss"} 1
# HELP chowki_tokens_total Tokens that providers reported, by type. input counts every prompt token, cached or not.
# TYPE chowki_tokens_total counter
chowki_tokens_total{type="cache_read"} 60
chowki_tokens_total{type="input"} 100
chowki_tokens_total{type="output"} 20
# HELP chowki_cost_usd_total Cost of priced requests, in US dollars.
# TYPE chowki_cost_usd_total counter
chowki_cost_usd_total 0.5
# HELP chowki_savings_usd Net savings since the gateway started, in US dollars, by method; they can be negative.
# TYPE chowki_savings_usd gauge
chowki_savings_usd{method="exact_cache"} 0.5
chowki_savings_usd{method="prompt_cache"} 0.25
# HELP chowki_redactions_total Secrets and personal data that redaction found, by type.
# TYPE chowki_redactions_total counter
chowki_redactions_total{type="email"} 2
# HELP chowki_upstream_latency_seconds Time that providers took to answer, streams included.
# TYPE chowki_upstream_latency_seconds histogram
chowki_upstream_latency_seconds_bucket{le="0.05"} 0
chowki_upstream_latency_seconds_bucket{le="0.1"} 0
chowki_upstream_latency_seconds_bucket{le="0.25"} 0
chowki_upstream_latency_seconds_bucket{le="0.5"} 0
chowki_upstream_latency_seconds_bucket{le="1"} 0
chowki_upstream_latency_seconds_bucket{le="2.5"} 1
chowki_upstream_latency_seconds_bucket{le="5"} 1
chowki_upstream_latency_seconds_bucket{le="10"} 1
chowki_upstream_latency_seconds_bucket{le="30"} 1
chowki_upstream_latency_seconds_bucket{le="60"} 1
chowki_upstream_latency_seconds_bucket{le="120"} 1
chowki_upstream_latency_seconds_bucket{le="300"} 1
chowki_upstream_latency_seconds_bucket{le="600"} 1
chowki_upstream_latency_seconds_bucket{le="+Inf"} 1
chowki_upstream_latency_seconds_sum 2
chowki_upstream_latency_seconds_count 1
# HELP chowki_overhead_seconds Time that the gateway itself added to a request.
# TYPE chowki_overhead_seconds histogram
chowki_overhead_seconds_bucket{le="0.0005"} 1
chowki_overhead_seconds_bucket{le="0.001"} 2
chowki_overhead_seconds_bucket{le="0.0025"} 2
chowki_overhead_seconds_bucket{le="0.005"} 3
chowki_overhead_seconds_bucket{le="0.01"} 3
chowki_overhead_seconds_bucket{le="0.025"} 3
chowki_overhead_seconds_bucket{le="0.05"} 3
chowki_overhead_seconds_bucket{le="0.1"} 3
chowki_overhead_seconds_bucket{le="0.25"} 3
chowki_overhead_seconds_bucket{le="+Inf"} 3
chowki_overhead_seconds_sum 0.0042
chowki_overhead_seconds_count 3
`
	if b.String() != want {
		t.Errorf("Write() =\n%s\nwant\n%s", b.String(), want)
	}
	if _, _, err := parse(b.String()); err != nil {
		t.Error(err)
	}
}

func TestLabelEscaping(t *testing.T) {
	r := New()
	model := "a \"quoted\"\\model\nwith\ta tab and é"
	r.Record(Request{Family: "openai", Provider: "p", Model: model, Status: 404})
	var b strings.Builder
	if err := r.Write(&b); err != nil {
		t.Fatal(err)
	}
	_, samples, err := parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if got := samples[0].labels["model"]; got != strings.ReplaceAll(model, "\t", "") {
		t.Errorf("model label = %q, want %q without the tab", got, model)
	}
}

func TestSeriesAreBounded(t *testing.T) {
	r := New()
	for i := range maxSeries + 50 {
		r.Record(Request{Family: "openai", Provider: "p", Model: fmt.Sprint("model-", i), Status: 404})
	}
	if n := len(r.requests); n > maxSeries+1 {
		t.Errorf("%d series, want at most %d", n, maxSeries+1)
	}
	if got := r.requests[requestLabels{"openai", "other", "other", "404", ""}]; got != 50 {
		t.Errorf("the overflow series counts %v, want 50", got)
	}
}

func TestHandler(t *testing.T) {
	w := httptest.NewRecorder()
	example().Handler().ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/plain; version=0.0.4; charset=utf-8" ||
		!strings.Contains(w.Body.String(), "chowki_cost_usd_total 0.5\n") {
		t.Errorf("Handler() = %d %q\n%s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
}
