package ratelimit

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Units of a limit.
const (
	UnitRequests = "requests"
	UnitTokens   = "tokens"
)

// Limiter enforces each key's limits of requests and tokens per minute
// with token buckets. A bucket holds one minute's worth, so a key can use
// its whole limit in a burst, and refills evenly over a minute. Buckets
// live in memory: they start full after a restart, and each Chowki node
// has its own.
type Limiter struct {
	mu       sync.Mutex
	requests map[int64]*bucket
	tokens   map[int64]*bucket
}

// New returns a limiter whose buckets start full.
func New() *Limiter {
	return &Limiter{requests: map[int64]*bucket{}, tokens: map[int64]*bucket{}}
}

// bucket holds up to limit and refills at limit per minute.
type bucket struct {
	limit float64
	level float64   // below 0 after requests used more tokens than estimated
	at    time.Time // when level was last refilled
}

// get returns the key's bucket in m, refilled at now and resized to limit.
func get(m map[int64]*bucket, key, limit int64, now time.Time) *bucket {
	b := m[key]
	if b == nil {
		b = &bucket{limit: float64(limit), level: float64(limit), at: now}
		m[key] = b
	}
	b.refill(now)
	b.limit = float64(limit)
	b.level = min(b.level, b.limit)
	return b
}

func (b *bucket) refill(now time.Time) {
	if d := now.Sub(b.at); d > 0 {
		b.level = min(b.level+float64(d)*b.limit/float64(time.Minute), b.limit)
		b.at = now
	}
}

// wait returns how long the bucket takes to hold n.
func (b *bucket) wait(n float64) time.Duration {
	return time.Duration((n - b.level) * float64(time.Minute) / b.limit)
}

// AllowRequest counts a request of the key against its limit of rpm
// requests per minute; 0 means no limit. It returns an *ExceededError when
// the limit is reached.
func (l *Limiter) AllowRequest(key, rpm int64, now time.Time) error {
	if rpm <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := get(l.requests, key, rpm, now)
	if b.level < 1 {
		return &ExceededError{Unit: UnitRequests, Limit: rpm, Wait: b.wait(1)}
	}
	b.level--
	return nil
}

// Ticket holds a request's estimated tokens, for Settle.
type Ticket struct {
	key      int64
	estimate int64
}

// TakeTokens holds the estimated tokens of a request of the key against its
// limit of tpm tokens per minute. With no limit, tpm 0, it returns a nil
// ticket. A request larger than the limit waits for a full bucket. It
// returns an *ExceededError when the tokens aren't available.
func (l *Limiter) TakeTokens(key, tpm, estimate int64, now time.Time) (*Ticket, error) {
	if tpm <= 0 {
		return nil, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := get(l.tokens, key, tpm, now)
	if need := float64(min(estimate, tpm)); b.level < need {
		return nil, &ExceededError{Unit: UnitTokens, Limit: tpm, Need: estimate, Wait: b.wait(need)}
	}
	b.level -= float64(estimate)
	return &Ticket{key: key, estimate: estimate}, nil
}

// Settle replaces the estimate that ticket t holds with the tokens that the
// request used: its input and output tokens, 0 when unknown. It ignores a
// nil ticket.
func (l *Limiter) Settle(t *Ticket, used int64, now time.Time) {
	if t == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.tokens[t.key] // TakeTokens created it, and buckets stay
	b.refill(now)
	b.level = min(b.level-float64(used-t.estimate), b.limit)
}

// ExceededError reports a reached rate limit.
type ExceededError struct {
	Unit  string        // UnitRequests or UnitTokens
	Limit int64         // per minute
	Need  int64         // the request's estimated tokens, for UnitTokens
	Wait  time.Duration // until the request fits
}

// Error says what happened and when to retry, for the client.
func (e *ExceededError) Error() string {
	unit := e.Unit
	if e.Limit == 1 {
		unit = strings.TrimSuffix(unit, "s")
	}
	msg := fmt.Sprintf("This key has reached its limit of %d %s per minute", e.Limit, unit)
	if e.Unit == UnitTokens {
		msg += fmt.Sprintf("; this request needs about %d tokens", e.Need)
	}
	return fmt.Sprintf("%s. Try again in %s.", msg, formatWait(e.RetryAfter()))
}

// RetryAfter returns the wait rounded up to whole seconds, at least one, as
// the Retry-After header gives it.
func (e *ExceededError) RetryAfter() time.Duration {
	return max((e.Wait + time.Second - 1).Truncate(time.Second), time.Second)
}

func formatWait(d time.Duration) string {
	switch {
	case d == time.Second:
		return "1 second"
	case d < time.Minute:
		return fmt.Sprintf("%d seconds", d/time.Second)
	}
	return d.String()
}
