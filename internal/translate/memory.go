package translate

import (
	"encoding/json"
	"sync"
	"time"
)

// Memory keeps what translated answers held that the OpenAI format can't
// carry: Anthropic's thinking blocks, and Gemini's thought signatures. Both
// providers want them back in the next request of a tool-use loop, and
// clients send back the IDs of tool calls, so Memory files them under those
// IDs. It never keeps text or tool arguments, only the thinking, which the
// providers encrypt, and it keeps it in memory for an hour at most: after
// that or a restart, a loop goes on without it. A nil Memory keeps nothing.
type Memory struct {
	mu    sync.Mutex
	byID  map[string]*memo
	queue []*memo // oldest first
	bytes int
	now   func() time.Time
}

// memo is what an answer's tool calls need back.
type memo struct {
	ids []string
	// items are an Anthropic answer's thinking blocks, text and tool calls,
	// in order, without the text and the calls themselves.
	items []item
	// signature is a Gemini call's thought signature.
	signature string
	size      int
	expires   time.Time
}

type item struct {
	thinking json.RawMessage // a thinking or redacted_thinking block
	text     bool            // where the answer's text was
	toolUse  string          // the ID of a tool call
}

// Limits of a Memory.
const (
	memoryTTL      = time.Hour
	memoryMaxBytes = 64 << 20
)

// NewMemory returns an empty Memory.
func NewMemory() *Memory {
	return &Memory{byID: map[string]*memo{}, now: time.Now}
}

func (m *Memory) put(mm *memo) {
	if m == nil || len(mm.ids) == 0 {
		return
	}
	for _, it := range mm.items {
		mm.size += len(it.thinking) + len(it.toolUse)
	}
	mm.size += len(mm.signature)
	for _, id := range mm.ids {
		mm.size += len(id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	mm.expires = now.Add(memoryTTL)
	for _, id := range mm.ids {
		m.byID[id] = mm
	}
	m.queue = append(m.queue, mm)
	m.bytes += mm.size
	for len(m.queue) > 0 && (m.bytes > memoryMaxBytes || now.After(m.queue[0].expires)) {
		old := m.queue[0]
		m.queue[0] = nil
		m.queue = m.queue[1:]
		m.bytes -= old.size
		for _, id := range old.ids {
			if m.byID[id] == old {
				delete(m.byID, id)
			}
		}
	}
}

func (m *Memory) get(id string) *memo {
	if m == nil || id == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mm := m.byID[id]
	if mm == nil || m.now().After(mm.expires) {
		return nil
	}
	return mm
}
