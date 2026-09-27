package ratelimit

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

// takeErr returns only the error of TakeTokens.
func takeErr(l *Limiter, key, tpm, estimate int64, now time.Time) error {
	_, err := l.TakeTokens(key, tpm, estimate, now)
	return err
}

// wait returns the wait of an *ExceededError, or fails the test.
func wait(t *testing.T, err error) time.Duration {
	t.Helper()
	var e *ExceededError
	if !errors.As(err, &e) {
		t.Fatalf("error = %v, want an *ExceededError", err)
	}
	return e.Wait
}

func TestAllowRequest(t *testing.T) {
	l := New()
	for i := range 3 {
		if err := l.AllowRequest(1, 3, t0); err != nil {
			t.Fatalf("request %d: %v; want the burst of 3 allowed", i+1, err)
		}
	}
	// 3 requests a minute refill one every 20 seconds.
	if got := wait(t, l.AllowRequest(1, 3, t0)); got != 20*time.Second {
		t.Errorf("wait = %v, want 20s", got)
	}
	if got := wait(t, l.AllowRequest(1, 3, t0.Add(15*time.Second))); got != 5*time.Second {
		t.Errorf("wait after 15s = %v, want 5s", got)
	}
	if err := l.AllowRequest(1, 3, t0.Add(20*time.Second)); err != nil {
		t.Errorf("after 20s: %v; want one more request", err)
	}
	if err := l.AllowRequest(2, 3, t0); err != nil {
		t.Errorf("another key: %v; want its own bucket", err)
	}
	for range 100 {
		if err := l.AllowRequest(3, 0, t0); err != nil {
			t.Fatalf("no limit: %v", err)
		}
	}
}

func TestBucketHoldsOneMinute(t *testing.T) {
	l := New()
	for range 2 {
		_ = l.AllowRequest(1, 2, t0)
	}
	// A long pause refills the bucket, but not beyond its limit.
	later := t0.Add(time.Hour)
	for i := range 2 {
		if err := l.AllowRequest(1, 2, later); err != nil {
			t.Fatalf("request %d after an hour: %v", i+1, err)
		}
	}
	if err := l.AllowRequest(1, 2, later); err == nil {
		t.Error("a third request was allowed; the bucket holds 2")
	}
}

func TestLimitChanges(t *testing.T) {
	l := New()
	_ = l.AllowRequest(1, 10, t0) // 9 left
	// A lower limit shrinks the bucket at once.
	for i := range 2 {
		if err := l.AllowRequest(1, 2, t0); err != nil {
			t.Fatalf("request %d at the lower limit: %v", i+1, err)
		}
	}
	if err := l.AllowRequest(1, 2, t0); err == nil {
		t.Fatal("the lower limit of 2 allowed a third request")
	}
	// A higher limit refills at its own rate: 60 a minute is one a second.
	if got := wait(t, l.AllowRequest(1, 60, t0)); got != time.Second {
		t.Errorf("wait at 60 a minute = %v, want 1s", got)
	}
	if err := l.AllowRequest(1, 60, t0.Add(time.Second)); err != nil {
		t.Errorf("a second later at 60 a minute: %v", err)
	}
}

func TestTakeTokens(t *testing.T) {
	l := New()
	first, err := l.TakeTokens(1, 1000, 600, t0)
	if err != nil || first == nil {
		t.Fatalf("TakeTokens() = %v, %v", first, err)
	}
	// 400 are left; 1000 a minute refill 200 in 12 seconds.
	_, err = l.TakeTokens(1, 1000, 600, t0)
	var e *ExceededError
	if !errors.As(err, &e) || e.Unit != UnitTokens || e.Need != 600 || e.Wait != 12*time.Second {
		t.Fatalf("second TakeTokens() error = %#v; want a 12s wait for 600 tokens", err)
	}
	// The request used 100 tokens, not 600: the 500 return.
	l.Settle(first, 100, t0)
	if _, err := l.TakeTokens(1, 1000, 600, t0); err != nil {
		t.Errorf("after settling at 100: %v; want 900 available", err)
	}

	if tk, err := l.TakeTokens(2, 0, 1e9, t0); tk != nil || err != nil {
		t.Errorf("no limit: TakeTokens() = %v, %v; want nil, nil", tk, err)
	}
	l.Settle(nil, 5, t0) // a request without a ticket
}

func TestTokensBeyondTheEstimate(t *testing.T) {
	l := New()
	tk, err := l.TakeTokens(1, 1000, 100, t0)
	if err != nil {
		t.Fatal(err)
	}
	// The request used 3100 tokens, 3000 more than it held: the bucket is
	// 2100 short, and needs 2.2 minutes to hold the next 100.
	l.Settle(tk, 3100, t0)
	if got := wait(t, takeErr(l, 1, 1000, 100, t0)); got != 132*time.Second {
		t.Errorf("wait = %v, want 2m12s", got)
	}
}

func TestSettleCapsAtTheLimit(t *testing.T) {
	l := New()
	tk, err := l.TakeTokens(1, 1000, 900, t0)
	if err != nil {
		t.Fatal(err)
	}
	// A minute later the bucket is full again; returning the estimate of a
	// failed request mustn't take it over the limit.
	l.Settle(tk, 0, t0.Add(time.Minute))
	if err := takeErr(l, 1, 1000, 1000, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := takeErr(l, 1, 1000, 1, t0.Add(time.Minute)); err == nil {
		t.Error("the bucket held more than its limit")
	}
}

func TestRequestLargerThanTheLimit(t *testing.T) {
	l := New()
	if err := takeErr(l, 1, 1000, 5000, t0); err != nil {
		t.Fatalf("a request of 5000 tokens with a full bucket of 1000: %v; want it allowed", err)
	}
	// The bucket owes 4000 tokens: 4 minutes, and then 1 minute for a
	// request as large as the whole bucket.
	if got := wait(t, takeErr(l, 1, 1000, 5000, t0)); got != 5*time.Minute {
		t.Errorf("wait = %v, want 5m", got)
	}
}

func TestExceededError(t *testing.T) {
	tests := []struct {
		err   ExceededError
		after time.Duration
		msg   string
	}{
		{ExceededError{Unit: UnitRequests, Limit: 60, Wait: 300 * time.Millisecond}, time.Second,
			"This key has reached its limit of 60 requests per minute. Try again in 1 second."},
		{ExceededError{Unit: UnitRequests, Limit: 3, Wait: 20 * time.Second}, 20 * time.Second,
			"This key has reached its limit of 3 requests per minute. Try again in 20 seconds."},
		{ExceededError{Unit: UnitTokens, Limit: 100000, Need: 12000, Wait: 4*time.Minute + 100*time.Millisecond},
			4*time.Minute + time.Second, "This key has reached its limit of 100000 tokens per minute; " +
				"this request needs about 12000 tokens. Try again in 4m1s."},
		{ExceededError{Unit: UnitRequests, Limit: 1, Wait: 0}, time.Second,
			"This key has reached its limit of 1 request per minute. Try again in 1 second."},
	}
	for _, tt := range tests {
		if got := tt.err.RetryAfter(); got != tt.after {
			t.Errorf("RetryAfter() of %v = %v, want %v", tt.err.Wait, got, tt.after)
		}
		if got := tt.err.Error(); got != tt.msg {
			t.Errorf("Error() =\n%s\nwant\n%s", got, tt.msg)
		}
	}
}

func TestConcurrentRequests(t *testing.T) {
	l := New()
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			for range 10 {
				if l.AllowRequest(1, 100, t0) == nil {
					allowed.Add(1)
				}
				if tk, err := l.TakeTokens(1, 1000, 1, t0); err == nil {
					l.Settle(tk, 1, t0)
				}
			}
		})
	}
	wg.Wait()
	if n := allowed.Load(); n != 100 {
		t.Errorf("allowed %d of 500 requests at once, want the limit of 100", n)
	}
}
