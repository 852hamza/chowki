// Command loadtest measures the overhead that chowki serve adds to requests.
// It runs the gateway, pinned to a few CPUs, against a fake provider that
// answers after a fixed delay, sends chat requests at a fixed rate, half of
// them streamed, and reports the latency percentiles minus that delay, and
// the gateway's own overhead histogram. It fails when the p99 overhead is
// over the limit.
//
//	go build -o bin/chowki ./cmd/chowki
//	go run ./tools/loadtest --rps 200 --duration 60s --cpus 0,4
//
// With a gateway built with -race, it fails when the gateway reports a data
// race. Such a gateway is many times slower, so test it at a lower rate:
//
//	go build -race -o bin/chowki-race ./cmd/chowki
//	go run ./tools/loadtest --binary bin/chowki-race --rps 20 --max-p99 1s
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	opts := options{}
	flag.StringVar(&opts.binary, "binary", "bin/chowki", "the chowki binary to test")
	flag.IntVar(&opts.rps, "rps", 200, "requests per second")
	flag.DurationVar(&opts.duration, "duration", 60*time.Second, "how long to measure")
	flag.DurationVar(&opts.warmup, "warmup", 5*time.Second, "how long to send requests before measuring")
	flag.DurationVar(&opts.delay, "delay", 20*time.Millisecond, "how long the fake provider takes to answer")
	flag.StringVar(&opts.cpus, "cpus", "0,4", "the CPUs for the gateway, as taskset takes them; empty for all")
	flag.DurationVar(&opts.maxP99, "max-p99", 25*time.Millisecond, "the highest p99 overhead that passes")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, opts); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

type options struct {
	binary, cpus                    string
	rps                             int
	duration, warmup, delay, maxP99 time.Duration
}

// result is what the client saw of one request.
type result struct {
	latency time.Duration
	stream  bool
	err     error
}

func run(ctx context.Context, opts options) error {
	binary, err := filepath.Abs(opts.binary)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "chowki-loadtest-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	upstream, err := startProvider(ctx, opts.delay)
	if err != nil {
		return err
	}
	defer func() { _ = upstream.Close() }()

	addr, err := freeAddress(ctx)
	if err != nil {
		return err
	}
	config := fmt.Sprintf("server:\n  listen: %q\nproviders:\n  - name: openai\n    type: openai\n"+
		"    base_url: http://%s/v1\n    api_key_env: LOADTEST_UPSTREAM_KEY\n", addr, upstream.Addr())
	if err := os.WriteFile(filepath.Join(dir, "chowki.yaml"), []byte(config), 0o600); err != nil {
		return err
	}
	env := append(os.Environ(), "LOADTEST_UPSTREAM_KEY=loadtest-upstream-key", "CHOWKI_CONFIG=")
	if _, err := command(ctx, dir, env, binary, "init"); err != nil {
		return err
	}
	out, err := command(ctx, dir, env, binary, "key", "create", "--name", "loadtest")
	if err != nil {
		return err
	}
	key := regexp.MustCompile(`chowki_[0-9A-Za-z]{49}`).FindString(out)
	if key == "" {
		return errors.New("no key in the output of chowki key create")
	}

	args := []string{binary, "serve"}
	procs := 0
	if opts.cpus != "" {
		args = append([]string{"taskset", "-c", opts.cpus}, args...)
		procs = len(strings.Split(opts.cpus, ","))
	}
	logFile, err := os.Create(filepath.Join(dir, "serve.log"))
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	gw := exec.CommandContext(ctx, args[0], args[1:]...)
	gw.Dir, gw.Env, gw.Stderr = dir, env, logFile
	gw.Cancel = func() error { return gw.Process.Signal(syscall.SIGTERM) }
	if procs > 0 {
		gw.Env = append(gw.Env, "GOMAXPROCS="+strconv.Itoa(procs))
	}
	if err := gw.Start(); err != nil {
		return err
	}
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			_ = gw.Process.Signal(syscall.SIGTERM)
			_ = gw.Wait()
		}
	}
	defer stop()
	base := "http://" + addr
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 4 * opts.rps,
		DisableCompression: true}, Timeout: time.Minute}
	if err := waitReady(ctx, client, base); err != nil {
		return err
	}

	fmt.Printf("Warming up for %s at %d requests per second.\n", opts.warmup, opts.rps)
	load(ctx, client, base, key, opts.rps, opts.warmup)
	before, err := overheadBuckets(ctx, client, base)
	if err != nil {
		return err
	}
	fmt.Printf("Measuring for %s.\n", opts.duration)
	start := time.Now()
	results := load(ctx, client, base, key, opts.rps, opts.duration)
	elapsed := time.Since(start)
	after, err := overheadBuckets(ctx, client, base)
	if err != nil {
		return err
	}
	stop()
	// A gateway built with -race reports data races in its log.
	logText, err := os.ReadFile(logFile.Name())
	if err != nil {
		return err
	}
	if strings.Contains(string(logText), "DATA RACE") {
		return fmt.Errorf("the gateway found a data race:\n%s", logText)
	}
	return report(results, elapsed, opts, subtract(after, before))
}

// load sends requests at rps for d, from a schedule, so that a slow
// answer doesn't delay the next request, and returns what it saw.
func load(ctx context.Context, client *http.Client, base, key string, rps int, d time.Duration) []result {
	n := int(d.Seconds() * float64(rps))
	interval := time.Second / time.Duration(rps)
	results := make([]result, n)
	var wg sync.WaitGroup
	start := time.Now()
	for i := range n {
		time.Sleep(time.Until(start.Add(time.Duration(i) * interval)))
		wg.Go(func() { results[i] = send(ctx, client, base, key, i%2 == 1) })
	}
	wg.Wait()
	return results
}

// prompt is a request of a realistic size, which redaction scans.
var prompt = strings.Repeat("Summarize the meeting notes below in three bullet points, and list the owners "+
	"of each action item with their deadlines. ", 12)

func send(ctx context.Context, client *http.Client, base, key string, stream bool) result {
	body := fmt.Sprintf(`{"model":"openai/gpt-6-luna","stream":%t,"messages":[{"role":"user","content":%q}]}`,
		stream, prompt)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions",
		strings.NewReader(body))
	if err != nil {
		return result{err: err}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return result{err: err}
	}
	_, err = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	latency := time.Since(start)
	if err == nil && resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("status %d", resp.StatusCode)
	}
	return result{latency: latency, stream: stream, err: err}
}

// startProvider starts a fake OpenAI provider that answers chat requests
// after delay, streamed or not.
func startProvider(ctx context.Context, delay time.Duration) (net.Listener, error) {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		time.Sleep(delay)
		if !strings.Contains(string(data), `"stream":true`) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"chatcmpl-1","object":"chat.completion","created":1790000000,`+
				`"model":"gpt-6-luna","choices":[{"index":0,"message":{"role":"assistant","content":"- One\n- Two\n- Three"},`+
				`"finish_reason":"stop"}],"usage":{"prompt_tokens":300,"completion_tokens":12,"total_tokens":312}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, part := range []string{"- One", "\n- Two", "\n- Three"} {
			fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1790000000,"+
				"\"model\":\"gpt-6-luna\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", part)
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1790000000,"+
			"\"model\":\"gpt-6-luna\",\"choices\":[],\"usage\":{\"prompt_tokens\":300,\"completion_tokens\":12,"+
			"\"total_tokens\":312}}\n\ndata: [DONE]\n\n")
	})
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return ln, nil
}

func freeAddress(ctx context.Context) (string, error) {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String(), nil
}

func command(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", filepath.Base(name), strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func waitReady(ctx context.Context, client *http.Client, base string) error {
	for range 100 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/readyz", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("the gateway didn't become ready in 10 seconds")
}

// overheadBuckets reads the cumulative buckets of the gateway's overhead
// histogram, by upper bound in seconds.
func overheadBuckets(ctx context.Context, client *http.Client, base string) (map[float64]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/metrics", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	buckets := map[float64]float64{}
	line := regexp.MustCompile(`^chowki_overhead_seconds_bucket\{le="([^"]+)"\} (\S+)$`)
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		m := line.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		le, err := strconv.ParseFloat(m[1], 64)
		if m[1] == "+Inf" {
			le, err = math.Inf(1), nil
		}
		if err != nil {
			return nil, err
		}
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			return nil, err
		}
		buckets[le] = v
	}
	if len(buckets) == 0 {
		return nil, errors.New("/metrics has no chowki_overhead_seconds buckets")
	}
	return buckets, sc.Err()
}

func subtract(after, before map[float64]float64) map[float64]float64 {
	out := map[float64]float64{}
	for le, v := range after {
		out[le] = v - before[le]
	}
	return out
}

// bucketPercentile returns the upper bound of the bucket that holds the
// percentile p of a cumulative histogram.
func bucketPercentile(buckets map[float64]float64, p float64) float64 {
	bounds := slices.Sorted(func(yield func(float64) bool) {
		for le := range buckets {
			if !yield(le) {
				return
			}
		}
	})
	total := buckets[math.Inf(1)]
	for _, le := range bounds {
		if buckets[le] >= p*total {
			return le
		}
	}
	return math.Inf(1)
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[min(len(sorted)-1, int(math.Ceil(p*float64(len(sorted))))-1)]
}

func report(results []result, elapsed time.Duration, opts options, buckets map[float64]float64) error {
	var overheads []time.Duration
	failed := 0
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			failed++
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		overheads = append(overheads, r.latency-opts.delay)
	}
	slices.Sort(overheads)
	ms := func(d time.Duration) string { return fmt.Sprintf("%.2f ms", float64(d.Microseconds())/1000) }
	fmt.Printf("\nRequests:  %d in %s (%.1f per second), %d failed\n", len(results), elapsed.Round(time.Millisecond),
		float64(len(results))/elapsed.Seconds(), failed)
	if firstErr != nil {
		fmt.Printf("First failure: %v\n", firstErr)
	}
	fmt.Printf("Overhead seen by the client (latency minus the provider's %s):\n", opts.delay)
	fmt.Printf("  p50 %s   p90 %s   p99 %s   max %s\n", ms(percentile(overheads, 0.5)), ms(percentile(overheads, 0.9)),
		ms(percentile(overheads, 0.99)), ms(percentile(overheads, 1)))
	fmt.Printf("Overhead that the gateway measured (chowki_overhead_seconds, bucket bounds):\n")
	bound := func(p float64) string {
		le := bucketPercentile(buckets, p)
		if math.IsInf(le, 1) {
			return "over the last bucket"
		}
		return "≤ " + ms(time.Duration(le*float64(time.Second)))
	}
	fmt.Printf("  p50 %s   p99 %s\n", bound(0.5), bound(0.99))
	p99 := percentile(overheads, 0.99)
	switch {
	case failed > 0:
		return fmt.Errorf("%d requests failed", failed)
	case p99 > opts.maxP99:
		return fmt.Errorf("the p99 overhead, %s, is over %s", ms(p99), ms(opts.maxP99))
	}
	fmt.Printf("\nPassed: the p99 overhead is under %s.\n", ms(opts.maxP99))
	return nil
}
