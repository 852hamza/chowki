package pipeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// object is a JSON object with the positions of its top-level values, so
// that a value can be replaced while every other byte stays the same.
type object struct {
	body   []byte
	values map[string]span
	order  []string // keys in body order
	end    int      // position of the closing brace
}

type span struct{ start, end int }

// parseObject reads a JSON object. It rejects invalid JSON, other values
// and duplicate top-level keys: with two "model" fields, the gateway and
// the provider could read different models.
func parseObject(body []byte) (*object, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("the body must be a JSON object")
	}
	o := &object{body: body, values: map[string]span{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		if _, dup := o.values[key]; dup {
			return nil, fmt.Errorf("the field %q appears twice", key)
		}
		end := int(dec.InputOffset())
		o.values[key] = span{end - len(raw), end}
		o.order = append(o.order, key)
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	o.end = int(dec.InputOffset()) - 1
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid JSON: data after the object")
	}
	return o, nil
}

// raw returns the value of a top-level key.
func (o *object) raw(key string) (json.RawMessage, bool) {
	s, ok := o.values[key]
	if !ok {
		return nil, false
	}
	return o.body[s.start:s.end], true
}

// with returns the body with the given top-level values replaced, or added
// before the closing brace when missing. Values must be valid JSON.
func (o *object) with(values map[string][]byte) []byte {
	type edit struct {
		span
		text []byte
	}
	var edits []edit
	var added []string
	for key, v := range values {
		if s, ok := o.values[key]; ok {
			edits = append(edits, edit{s, v})
		} else {
			added = append(added, key)
		}
	}
	slices.Sort(added)
	if len(added) > 0 {
		var b bytes.Buffer
		for i, key := range added {
			if i > 0 || len(o.order) > 0 {
				b.WriteByte(',')
			}
			k, _ := json.Marshal(key) // a string always marshals
			b.Write(k)
			b.WriteByte(':')
			b.Write(values[key])
		}
		edits = append(edits, edit{span{o.end, o.end}, b.Bytes()})
	}
	slices.SortFunc(edits, func(a, b edit) int { return a.start - b.start })

	out := make([]byte, 0, len(o.body)+64)
	last := 0
	for _, e := range edits {
		out = append(out, o.body[last:e.start]...)
		out = append(out, e.text...)
		last = e.end
	}
	return append(out, o.body[last:]...)
}
