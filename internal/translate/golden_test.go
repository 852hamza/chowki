package translate

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

const created = 1790000000

// golden compares got with the file testdata/name, or writes the file with
// -update. JSON is compared indented, so that diffs are readable.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if strings.HasSuffix(name, ".json") {
		var b bytes.Buffer
		if err := json.Indent(&b, got, "", "  "); err != nil {
			t.Fatalf("%s: the output isn't JSON: %v\n%s", name, err, got)
		}
		got = append(b.Bytes(), '\n')
	}
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run the tests with -update to write it", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs; with -update the output rewrites it\ngot:\n%s", name, got)
	}
}

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// events splits a server-sent event stream into its events' names and data.
func events(stream []byte) (names []string, data [][]byte) {
	for block := range strings.SplitSeq(strings.ReplaceAll(string(stream), "\r\n", "\n"), "\n\n") {
		var name, payload string
		for line := range strings.SplitSeq(block, "\n") {
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				name = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				payload = v
			}
		}
		if payload != "" {
			names, data = append(names, name), append(data, []byte(payload))
		}
	}
	return names, data
}

// stream is a translator of streams.
type stream interface {
	Event(name string, data []byte) [][]byte
	End() [][]byte
}

// translateStream runs a stream through a translator and writes the chunks
// as the gateway sends them.
func translateStream(s stream, input []byte) []byte {
	var out bytes.Buffer
	names, data := events(input)
	for i := range names {
		for _, chunk := range s.Event(names[i], data[i]) {
			out.WriteString("data: " + string(chunk) + "\n\n")
		}
	}
	for _, chunk := range s.End() {
		out.WriteString("data: " + string(chunk) + "\n\n")
	}
	out.WriteString("data: [DONE]\n\n")
	return out.Bytes()
}

func parse(t *testing.T, name string) *Request {
	t.Helper()
	r, err := ParseRequest(read(t, name))
	if err != nil {
		t.Fatalf("ParseRequest(%s): %v", name, err)
	}
	return r
}

// The tool-use loop in both directions: the request with tool calls and
// their results, and an answer with parallel tool calls, as JSON and as a
// stream. With Memory, the next request gets the answer's thinking back.
func TestAnthropicGolden(t *testing.T) {
	r := parse(t, "tool_loop.openai.json")
	out, err := ToAnthropic(r, "claude-sonnet-5", 128000, nil)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "tool_loop.anthropic.json", out)

	mem := NewMemory()
	out, err = FromAnthropic(read(t, "tool_call.anthropic.json"), created, mem)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "tool_call.anthropic.openai.json", out)
	out, err = ToAnthropic(r, "claude-sonnet-5", 128000, mem)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "tool_loop.anthropic.thinking.json", out)

	streamMem := NewMemory()
	golden(t, "tool_call.anthropic.openai.sse", translateStream(NewAnthropicStream(created, true, streamMem),
		read(t, "tool_call.anthropic.sse")))
	fromStream, err := ToAnthropic(r, "claude-sonnet-5", 128000, streamMem)
	var a, b any
	if err != nil || json.Unmarshal(fromStream, &a) != nil || json.Unmarshal(out, &b) != nil || !reflect.DeepEqual(a, b) {
		t.Errorf("the thinking of a stream differs from that of its JSON answer: %v\n%s", err, fromStream)
	}
}

func TestGeminiGolden(t *testing.T) {
	r := parse(t, "tool_loop.openai.json")
	out, err := ToGemini(r, "gemini-3.7-flash", nil)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "tool_loop.gemini.json", out)

	out, err = FromGemini(read(t, "tool_call.gemini.json"), "gemini-3.7-flash", created, NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "tool_call.gemini.openai.json", out)
	golden(t, "tool_call.gemini.openai.sse", translateStream(NewGeminiStream("gemini-3.7-flash", created, true,
		NewMemory()), read(t, "tool_call.gemini.sse")))
}
