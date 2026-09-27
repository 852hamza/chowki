package sse

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

// MaxEventSize limits one event, so a stream without event boundaries
// can't exhaust memory.
const MaxEventSize = 16 << 20

// ErrEventTooLarge means an event exceeded MaxEventSize.
var ErrEventTooLarge = errors.New("sse: event too large")

// Event is one block of a stream: the lines up to and including an empty
// line.
type Event struct {
	// Name is the value of the "event" field; empty when there is none.
	Name string
	// Data is the values of the "data" fields, joined by "\n".
	Data []byte
	// Raw is every byte of the block as it arrived, terminator included.
	Raw []byte
}

// Reader reads events from a stream as they arrive. It accepts CRLF, LF and
// CR line endings, as the SSE specification allows.
type Reader struct {
	src    io.Reader
	buf    []byte // read but not yet consumed
	err    error  // from src, returned once buf is used up
	skipLF bool   // the last line ended with CR at a read boundary
	// scanned is how much of buf is known to hold no line break, so a
	// long line is scanned once, not again after every read.
	scanned int
	chunk   []byte
}

// NewReader returns a Reader that reads from src.
func NewReader(src io.Reader) *Reader { return &Reader{src: src, chunk: make([]byte, 32<<10)} }

// Next returns the next event. It returns an event as soon as its empty
// line arrives, without waiting for more input. When the stream ends, any
// bytes after the last complete event come back as a final event, and then
// Next returns the stream's error, io.EOF for a normal end.
func (r *Reader) Next() (Event, error) {
	var ev Event
	var data []byte
	hasData := false
	for {
		if r.skipLF && len(r.buf) > 0 {
			// The rest of a CRLF whose CR ended the previous read.
			r.skipLF = false
			if r.buf[0] == '\n' {
				ev.Raw = append(ev.Raw, '\n')
				r.buf = r.buf[1:]
				r.scanned = 0
				continue
			}
		}
		line, n, complete := r.line()
		if !complete {
			if r.err == nil {
				if len(ev.Raw)+len(r.buf) > MaxEventSize {
					return Event{}, ErrEventTooLarge
				}
				r.fill()
				continue
			}
			if len(r.buf) == 0 {
				if len(ev.Raw) == 0 {
					return Event{}, r.err
				}
				return finish(ev, data, hasData), nil
			}
			line, n = r.buf, len(r.buf) // a last line without a terminator
		}
		ev.Raw = append(ev.Raw, r.buf[:n]...)
		r.buf = r.buf[n:]
		r.scanned = 0
		if complete && len(line) == 0 {
			return finish(ev, data, hasData), nil
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "event":
			ev.Name = string(value)
		case "data":
			if hasData {
				data = append(data, '\n')
			}
			data = append(data, value...)
			hasData = true
		}
	}
}

// finish sets the event's data. An event with a data field has non-nil
// Data, even when the field was empty.
func finish(ev Event, data []byte, hasData bool) Event {
	if hasData {
		ev.Data = append([]byte{}, data...)
	}
	return ev
}

// line returns the next complete line in the buffer without its
// terminator, and how many bytes it takes up with the terminator.
func (r *Reader) line() (line []byte, n int, complete bool) {
	i := bytes.IndexAny(r.buf[r.scanned:], "\r\n")
	if i < 0 {
		r.scanned = len(r.buf)
		return nil, 0, false
	}
	i += r.scanned
	switch {
	case r.buf[i] == '\n':
		return r.buf[:i], i + 1, true
	case i+1 < len(r.buf):
		if r.buf[i+1] == '\n' {
			return r.buf[:i], i + 2, true
		}
		return r.buf[:i], i + 1, true
	default:
		// A CR at the end of what has arrived ends the line now; an LF
		// that follows in the next read belongs to it.
		r.skipLF = true
		return r.buf[:i], i + 1, true
	}
}

func (r *Reader) fill() {
	n, err := r.src.Read(r.chunk)
	r.buf = append(r.buf, r.chunk[:n]...)
	if err != nil {
		r.err = err
	}
}

// Relay forwards the stream in src to w one event at a time, and calls
// flush after each event, so the client receives it immediately rather
// than when a buffer fills. keep, if not nil, sees every event and decides
// whether to forward it. Relay returns nil at the end of the stream, or the
// first read, write or flush error.
func Relay(w io.Writer, flush func() error, src io.Reader, keep func(Event) bool) error {
	r := NewReader(src)
	for {
		ev, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read stream: %w", err)
		}
		if keep != nil && !keep(ev) {
			continue
		}
		if _, err := w.Write(ev.Raw); err != nil {
			return fmt.Errorf("write stream: %w", err)
		}
		if err := flush(); err != nil {
			return fmt.Errorf("flush stream: %w", err)
		}
	}
}
