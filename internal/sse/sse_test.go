package sse

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/testutil"
)

// pieces is a Reader that returns data n bytes at a time, like a network
// connection that delivers a stream in small parts.
type pieces struct {
	data []byte
	n    int
}

func (p *pieces) Read(b []byte) (int, error) {
	if len(p.data) == 0 {
		return 0, io.EOF
	}
	k := min(p.n, len(p.data), len(b))
	copy(b, p.data[:k])
	p.data = p.data[k:]
	return k, nil
}

type parsed struct {
	Name string
	Data string
	Has  bool // the event had a data field
}

// readAll parses input delivered n bytes at a time, checks that the raw
// bytes of the events add up to the input, and returns the events that have
// a name or data. Blank blocks, such as a lone comment or the LF of a CRLF
// split across reads, carry bytes but aren't events for the client.
func readAll(t *testing.T, input string, n int) []parsed {
	t.Helper()
	r := NewReader(&pieces{data: []byte(input), n: n})
	var got []parsed
	var raw bytes.Buffer
	for {
		ev, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		raw.Write(ev.Raw)
		if ev.Name != "" || ev.Data != nil {
			got = append(got, parsed{ev.Name, string(ev.Data), ev.Data != nil})
		}
	}
	if raw.String() != input {
		t.Fatalf("raw bytes (n=%d) = %q, want the input %q", n, raw.String(), input)
	}
	return got
}

func TestReader(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []parsed
	}{
		{"OpenAI style", "data: {\"a\":1}\n\ndata: [DONE]\n\n",
			[]parsed{{"", `{"a":1}`, true}, {"", "[DONE]", true}}},
		{"Anthropic style", "event: message_start\ndata: {}\n\nevent: ping\ndata: {\"type\":\"ping\"}\n\n",
			[]parsed{{"message_start", "{}", true}, {"ping", `{"type":"ping"}`, true}}},
		{"CRLF", "event: a\r\ndata: 1\r\n\r\ndata: 2\r\n\r\n", []parsed{{"a", "1", true}, {"", "2", true}}},
		{"CR", "data: 1\r\rdata: 2\r\r", []parsed{{"", "1", true}, {"", "2", true}}},
		{"multi-line data", "data: line 1\ndata: line 2\ndata\n\n", []parsed{{"", "line 1\nline 2\n", true}}},
		{"no space after colon", "data:x\n\n", []parsed{{"", "x", true}}},
		{"only one space removed", "data:  two\n\n", []parsed{{"", " two", true}}},
		{"comments and other fields", ": keep-alive\n\nid: 7\nretry: 100\ndata: x\n\n",
			[]parsed{{"", "x", true}}},
		{"empty data", "data:\n\n", []parsed{{"", "", true}}},
		{"leading blank line", "\ndata: x\n\n", []parsed{{"", "x", true}}},
		{"unterminated last event", "data: 1\n\ndata: 2", []parsed{{"", "1", true}, {"", "2", true}}},
		{"unterminated after a line", "data: 1\n", []parsed{{"", "1", true}}},
		{"empty stream", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, n := range []int{1, 2, 3, 7, 1 << 20} {
				if got := readAll(t, tt.input, n); !reflect.DeepEqual(got, tt.want) {
					t.Errorf("events (n=%d) = %+v, want %+v", n, got, tt.want)
				}
			}
		})
	}
}

func TestReaderSplitCRLF(t *testing.T) {
	// The CR of a CRLF ends one read and the LF starts the next: the event
	// must come out at the CR, and the LF must not end another event.
	src := io.MultiReader(strings.NewReader("data: 1\r\n\r"), strings.NewReader("\ndata: 2\r\n\r\n"))
	r := NewReader(src)
	first, err := r.Next()
	if err != nil || string(first.Data) != "1" {
		t.Fatalf("first event = %+v, %v", first, err)
	}
	second, err := r.Next()
	if err != nil || string(second.Data) != "2" || string(second.Raw) != "\ndata: 2\r\n\r\n" {
		t.Fatalf("second event = %+v (raw %q), %v", second, second.Raw, err)
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("Next() at the end = %v, want io.EOF", err)
	}
}

// endless returns 'a' forever: a stream with no line breaks.
type endless struct{}

func (endless) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 'a'
	}
	return len(b), nil
}

func TestEventTooLarge(t *testing.T) {
	if _, err := NewReader(endless{}).Next(); !errors.Is(err, ErrEventTooLarge) {
		t.Errorf("Next() = %v, want ErrEventTooLarge", err)
	}
}

func TestRelayFlushesEachEvent(t *testing.T) {
	pr, pw := io.Pipe()
	flushed := make(chan string, 10)
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- Relay(&out, func() error { flushed <- out.String(); return nil }, pr, nil)
	}()

	// The producer sends the second event only after the relay flushed the
	// first, so a relay that buffers would hang here.
	for i, ev := range []string{"data: 1\n\n", "data: 2\n\n"} {
		if _, err := pw.Write([]byte(ev)); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-flushed:
			if want := []string{"data: 1\n\n", "data: 1\n\ndata: 2\n\n"}[i]; got != want {
				t.Errorf("after event %d the client had %q, want %q", i+1, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("event %d wasn't flushed", i+1)
		}
	}
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Errorf("Relay() = %v", err)
	}
}

func TestRelayKeep(t *testing.T) {
	var out bytes.Buffer
	var seen []string
	err := Relay(&out, func() error { return nil }, strings.NewReader("data: a\n\ndata: DROP\n\ndata: b\n\n"),
		func(ev Event) bool {
			seen = append(seen, string(ev.Data))
			return string(ev.Data) != "DROP"
		})
	if err != nil || out.String() != "data: a\n\ndata: b\n\n" || len(seen) != 3 {
		t.Errorf("Relay() = %v, output %q, seen %q", err, out.String(), seen)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("client went away") }

func TestRelayErrors(t *testing.T) {
	broken := io.MultiReader(strings.NewReader("data: 1\n\n"), iotestErrReader{})
	var out bytes.Buffer
	if err := Relay(&out, func() error { return nil }, broken, nil); err == nil || out.String() != "data: 1\n\ndata: x" {
		t.Errorf("Relay() of a broken stream = %v, output %q", err, out.String())
	}
	if err := Relay(failingWriter{}, func() error { return nil }, strings.NewReader("data: 1\n\n"), nil); err == nil ||
		!strings.Contains(err.Error(), "client went away") {
		t.Errorf("Relay() to a failing writer = %v", err)
	}
	if err := Relay(&out, func() error { return errors.New("no flush") }, strings.NewReader("data: 1\n\n"), nil); err == nil {
		t.Error("Relay() ignored a flush error")
	}
	// A broken stream first returns what arrived, then the error.
	r := NewReader(iotestErrReader{})
	if ev, err := r.Next(); err != nil || string(ev.Data) != "x" {
		t.Errorf("Next() = %+v, %v; want the partial event", ev, err)
	}
	if _, err := r.Next(); err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("second Next() = %v, want the stream error", err)
	}
}

// iotestErrReader returns a partial event, then an error other than EOF.
type iotestErrReader struct{}

func (iotestErrReader) Read(b []byte) (int, error) {
	return copy(b, "data: x"), errors.New("connection reset")
}

func TestRelayIsLosslessWithFakeProvider(t *testing.T) {
	s := testutil.NewAnthropic(t, testutil.Config{Chunks: 5})
	fetch := func() io.ReadCloser {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL+"/v1/messages",
			strings.NewReader(`{"model":"m","max_tokens":5,"messages":[{}],"stream":true}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.Body
	}
	body := fetch()
	defer func() { _ = body.Close() }()
	var upstream, out bytes.Buffer
	var names []string
	if err := Relay(&out, func() error { return nil }, io.TeeReader(body, &upstream), func(ev Event) bool {
		names = append(names, ev.Name)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), upstream.Bytes()) {
		t.Errorf("relayed stream differs from the provider's:\n%s\nwant\n%s", out.Bytes(), upstream.Bytes())
	}
	if names[0] != "message_start" || names[len(names)-1] != "message_stop" {
		t.Errorf("event names = %q", names)
	}
}

func FuzzReader(f *testing.F) {
	for _, seed := range []string{"data: 1\n\n", "event: a\r\ndata: b\r\n\r\n", "data: 1\r\rdata: 2", ":c\n\ndata\n\n\r\n"} {
		f.Add(seed, uint8(1))
	}
	f.Fuzz(func(t *testing.T, input string, n uint8) {
		whole := readAll(t, input, len(input)+1)
		if split := readAll(t, input, int(n%16)+1); !reflect.DeepEqual(split, whole) {
			t.Errorf("events differ with %d-byte reads:\n%+v\nwant\n%+v", n%16+1, split, whole)
		}
	})
}
