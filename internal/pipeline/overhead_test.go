package pipeline_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/testutil"
)

// median returns the median duration of n calls of fn, after a warm-up.
func median(n int, fn func()) time.Duration {
	for range 20 {
		fn()
	}
	d := make([]time.Duration, n)
	for i := range d {
		start := time.Now()
		fn()
		d[i] = time.Since(start)
	}
	slices.Sort(d)
	return d[n/2]
}

// AC: the gateway's overhead in a local benchmark is under 5 ms at the
// median, measured as the difference between calling the same fake
// provider through Chowki and directly.
func TestOverhead(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing test: needs a build without the race detector")
	}
	h := newHarness(t, testutil.Config{Usage: fakeUsage}, testutil.Config{})
	direct := func() {
		resp := h.send(t.Context(), http.MethodPost, h.openai.URL+"/v1/chat/completions",
			map[string]string{"Authorization": "Bearer " + openAIKey}, openAIBody)
		readBody(t, resp)
	}
	gateway := func() {
		resp := h.post(t.Context(), "/v1/chat/completions", openAIBody)
		if readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	}
	directP50 := median(300, direct)
	gatewayP50 := median(300, gateway)
	overhead := gatewayP50 - directP50
	t.Logf("p50: direct %v, through Chowki %v, overhead %v", directP50, gatewayP50, overhead)
	if overhead > 5*time.Millisecond {
		t.Errorf("gateway overhead at p50 = %v, want under 5ms", overhead)
	}
}

func BenchmarkGateway(b *testing.B) {
	h := newHarness(b, testutil.Config{Usage: fakeUsage}, testutil.Config{})
	for b.Loop() {
		resp := h.post(b.Context(), "/v1/chat/completions", openAIBody)
		if readBody(b, resp); resp.StatusCode != http.StatusOK {
			b.Fatalf("status = %d", resp.StatusCode)
		}
	}
}
