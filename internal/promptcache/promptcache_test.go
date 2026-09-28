package promptcache

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

// long is a system prompt of about 1000 tokens.
var long = strings.Repeat("Answer as a careful assistant. ", 130)

func request(system, tools string) Request {
	r := Request{Provider: "anthropic", Model: "claude-test", MinTokens: 512}
	body := `{"model":"claude-test","messages":[]`
	if system != "" {
		r.System = json.RawMessage(system)
		body += `,"system":` + system
	}
	if tools != "" {
		r.Tools = json.RawMessage(tools)
		body += `,"tools":` + tools
	}
	r.Body = []byte(body + "}")
	return r
}

// second returns what Breakpoint does for r when r was seen once before.
func second(t *testing.T, r Request) (string, string, bool) {
	t.Helper()
	o := New()
	if _, _, ok := o.Breakpoint(r, t0); ok {
		t.Fatal("the first sighting of a prefix got a breakpoint")
	}
	field, value, ok := o.Breakpoint(r, t0.Add(time.Minute))
	if ok && !json.Valid(value) {
		t.Fatalf("the new %s isn't JSON: %s", field, value)
	}
	return field, string(value), ok
}

func TestBreakpoint(t *testing.T) {
	quoted, _ := json.Marshal(long)
	tool := `{"name":"search","description":"` + long + `","input_schema":{"type":"object"}}`
	tests := []struct {
		name          string
		r             Request
		field, suffix string // the new value ends with suffix; "" when nothing changes
	}{
		{"system string", request(string(quoted), ""), "system", `,"cache_control":{"type":"ephemeral"}}]`},
		{"system blocks", request(`[{"type":"text","text":"rules"},{"type":"text","text":`+string(quoted)+`}]`, ""),
			"system", `,"cache_control":{"type":"ephemeral"}}]`},
		{"tools without a system prompt", request("", "["+tool+"]"), "tools",
			`"input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}]`},
		{"system and tools: the breakpoint ends the system prompt", request(string(quoted), "["+tool+"]"), "system",
			`,"cache_control":{"type":"ephemeral"}}]`},
		{"too short", request(`"Be brief."`, ""), "", ""},
		{"nothing to cache", request("", ""), "", ""},
		{"empty system prompt", request(`""`, "["+tool+"]"), "tools",
			`"input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}]`},
		{"last block isn't an object", request(`["`+long+`"]`, ""), "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, value, ok := second(t, tt.r)
			if ok != (tt.field != "") || field != tt.field || !strings.HasSuffix(value, tt.suffix) {
				t.Errorf("Breakpoint() = %q, %s, %v; want %q ending with %s", field, value, ok, tt.field, tt.suffix)
			}
			if ok && strings.Count(value, "cache_control") != 1 {
				t.Errorf("Breakpoint() added %d breakpoints, want 1", strings.Count(value, "cache_control"))
			}
		})
	}
}

// AC: the optimizer never adds a breakpoint when cache_control already
// exists, so it never exceeds the provider's limit.
func TestRespectsExistingCacheControl(t *testing.T) {
	quoted, _ := json.Marshal(long)
	for name, body := range map[string]string{
		"on a message": `{"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}]}`,
		"top level":    `{"cache_control":{"type":"ephemeral"},"messages":[]}`,
	} {
		r := request(string(quoted), "")
		r.Body = []byte(body)
		if _, _, ok := second(t, r); ok {
			t.Errorf("%s: added a breakpoint to a request that has cache_control", name)
		}
	}
}

func TestUnknownMinimum(t *testing.T) {
	quoted, _ := json.Marshal(long)
	r := request(string(quoted), "")
	r.MinTokens = 0
	if _, _, ok := second(t, r); ok {
		t.Error("added a breakpoint for a model whose minimum is unknown")
	}
}

func TestWindow(t *testing.T) {
	quoted, _ := json.Marshal(long)
	r := request(string(quoted), "")
	o := New()
	o.Breakpoint(r, t0)
	if _, _, ok := o.Breakpoint(r, t0.Add(5*time.Minute)); ok {
		t.Error("a prefix last seen 5 minutes ago counted as repeated")
	}
	if _, _, ok := o.Breakpoint(r, t0.Add(6*time.Minute)); !ok {
		t.Error("a prefix seen a minute ago didn't count as repeated")
	}
	// A different model, tool choice or thinking setting is another prefix.
	for name, change := range map[string]func(*Request){
		"model":       func(r *Request) { r.Model = "claude-other" },
		"tool choice": func(r *Request) { r.ToolChoice = json.RawMessage(`{"type":"any"}`) },
		"thinking":    func(r *Request) { r.Thinking = json.RawMessage(`{"type":"enabled","budget_tokens":2048}`) },
	} {
		other := r
		change(&other)
		if _, _, ok := o.Breakpoint(other, t0.Add(6*time.Minute)); ok {
			t.Errorf("a request with another %s counted as repeated", name)
		}
	}
}

func TestSweep(t *testing.T) {
	o := New()
	for i := range maxPrefixes + 10 {
		o.repeated([32]byte{byte(i), byte(i >> 8)}, t0)
	}
	if len(o.seen) > maxPrefixes {
		t.Errorf("remembers %d prefixes, want at most %d", len(o.seen), maxPrefixes)
	}
	o.repeated([32]byte{1, 2, 3}, t0.Add(window))
	if len(o.seen) != 1 {
		t.Errorf("remembers %d prefixes after the window, want 1", len(o.seen))
	}
}

// withBreakpoint turns any system prompt or tools array into valid JSON
// with one more breakpoint, or leaves it.
func FuzzWithBreakpoint(f *testing.F) {
	for _, seed := range []string{`"x"`, `[{"type":"text","text":"a"}]`, `[{}]`, `[1]`, `[]`, `{}`, `[{"a":[{"b":{}}]} ]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if !json.Valid(raw) {
			return
		}
		for _, textAllowed := range []bool{true, false} {
			out, ok := withBreakpoint(raw, textAllowed)
			if !ok {
				continue
			}
			var blocks []json.RawMessage
			if err := json.Unmarshal(out, &blocks); err != nil || len(blocks) == 0 {
				t.Fatalf("withBreakpoint(%s) = %s: %v", raw, out, err)
			}
			var last map[string]json.RawMessage
			if json.Unmarshal(blocks[len(blocks)-1], &last) != nil ||
				string(last["cache_control"]) != `{"type":"ephemeral"}` {
				t.Fatalf("withBreakpoint(%s) = %s: no breakpoint on the last block", raw, out)
			}
		}
	})
}
