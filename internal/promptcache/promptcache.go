package promptcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"

	"github.com/852hamza/chowki/internal/usage"
)

const (
	// window is how long a prefix counts as repeated: Anthropic's default
	// cache lifetime.
	window = 5 * time.Minute
	// minRepeats is how often a prefix must be seen within the window
	// before it gets a breakpoint. A prefix seen once doesn't: writing it
	// to the cache costs more than reading it uncached.
	minRepeats = 2
	// maxPrefixes bounds the prefixes the optimizer remembers.
	maxPrefixes = 10000
)

// cacheControl marks the end of a prefix for Anthropic's 5-minute cache.
const cacheControl = `"cache_control":{"type":"ephemeral"}`

// Request is the part of an Anthropic Messages request that the optimizer
// reads.
type Request struct {
	// Provider and Model serve the request; each model has its own cache.
	Provider, Model string
	// MinTokens is the model's minimum cacheable prompt; 0 when unknown.
	MinTokens int
	// System, Tools, ToolChoice and Thinking are the raw top-level fields,
	// nil when absent. The prefix is the tools and the system prompt; the
	// other two change how the provider renders it.
	System, Tools, ToolChoice, Thinking json.RawMessage
	// Body is the whole request body.
	Body []byte
}

// Optimizer adds a prompt-cache breakpoint to requests whose tools and
// system prompt repeat, and leaves every other request alone.
type Optimizer struct {
	mu   sync.Mutex
	seen map[[32]byte][]time.Time // the last sightings of each prefix
}

// New returns an optimizer that has seen no prefixes.
func New() *Optimizer {
	return &Optimizer{seen: map[[32]byte][]time.Time{}}
}

// Breakpoint returns the top-level field to replace, "system" or "tools",
// and its new value, which ends with one cache_control breakpoint. It
// returns ok false, to leave the request unchanged, when the request has
// cache_control anywhere, as clients that manage their own caching do; when
// its prefix is too short or its model's minimum is unknown; and when the
// prefix hasn't repeated within the last 5 minutes. It never reorders
// content, and the one breakpoint it adds stays within Anthropic's limit.
func (o *Optimizer) Breakpoint(r Request, now time.Time) (field string, value json.RawMessage, ok bool) {
	if r.MinTokens <= 0 || bytes.Contains(r.Body, []byte(`"cache_control"`)) {
		return "", nil, false
	}
	// The breakpoint ends the system prompt, which follows the tools; with
	// no usable system prompt, it ends the tools.
	if isPresent(r.System) {
		field = "system"
		value, ok = withBreakpoint(r.System, true)
	}
	if !ok && isPresent(r.Tools) {
		field = "tools"
		value, ok = withBreakpoint(r.Tools, false)
	}
	if !ok || usage.EstimateTokens(r.System)+usage.EstimateTokens(r.Tools) < int64(r.MinTokens) {
		return "", nil, false
	}
	if !o.repeated(fingerprint(r), now) {
		return "", nil, false
	}
	return field, value, true
}

func isPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// fingerprint identifies a request's prefix and what shapes its cache.
func fingerprint(r Request) [32]byte {
	data, _ := json.Marshal([]any{r.Provider, r.Model, r.System, r.Tools, r.ToolChoice, r.Thinking}) // raw JSON always marshals
	return sha256.Sum256(data)
}

// repeated records a sighting of the prefix fp at now, and reports whether
// the prefix has now been seen minRepeats times within the window.
func (o *Optimizer) repeated(fp [32]byte, now time.Time) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	var times []time.Time
	for _, t := range o.seen[fp] {
		if now.Sub(t) < window {
			times = append(times, t)
		}
	}
	times = append(times, now)
	if len(times) > minRepeats {
		times = times[len(times)-minRepeats:]
	}
	o.seen[fp] = times
	if len(o.seen) > maxPrefixes {
		o.sweep(now)
	}
	return len(times) >= minRepeats
}

// sweep forgets prefixes not seen within the window and, if that isn't
// enough, arbitrary others.
func (o *Optimizer) sweep(now time.Time) {
	for fp, times := range o.seen {
		if now.Sub(times[len(times)-1]) >= window {
			delete(o.seen, fp)
		}
	}
	for fp := range o.seen {
		if len(o.seen) <= maxPrefixes {
			break
		}
		delete(o.seen, fp)
	}
}

// withBreakpoint returns raw, a system prompt or a tools array, with a
// breakpoint on its last block. A system prompt given as a string becomes
// one text block, which Anthropic reads the same way.
func withBreakpoint(raw json.RawMessage, textAllowed bool) (json.RawMessage, bool) {
	var text string
	if textAllowed && json.Unmarshal(raw, &text) == nil {
		if text == "" {
			return nil, false
		}
		return json.RawMessage(`[{"type":"text","text":` + string(bytes.TrimSpace(raw)) + `,` + cacheControl + `}]`), true
	}
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil || len(blocks) == 0 {
		return nil, false
	}
	last := bytes.TrimSpace(blocks[len(blocks)-1])
	if len(last) < 2 || last[0] != '{' {
		return nil, false
	}
	inner := bytes.TrimSpace(last[1 : len(last)-1])
	sep := ","
	if len(inner) == 0 {
		sep = ""
	}
	blocks[len(blocks)-1] = json.RawMessage("{" + string(inner) + sep + cacheControl + "}")
	var b bytes.Buffer
	b.WriteByte('[')
	for i, block := range blocks {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(block)
	}
	b.WriteByte(']')
	return b.Bytes(), true
}
