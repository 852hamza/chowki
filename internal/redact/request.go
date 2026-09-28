package redact

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Request finds secrets and personal data in the text of a request body
// of API family "openai" or "anthropic": message contents and their text
// parts, system prompts, tool results and documents given as text. It
// never reads images, audio or any other field. It counts the findings by
// type, and with mask it also returns the body with each finding replaced
// by its placeholder and every other byte unchanged.
func (r *Redactor) Request(family string, body []byte, mask bool) ([]byte, map[string]int, error) {
	root, err := parse(body)
	if err != nil {
		return nil, nil, fmt.Errorf("redact: %w", err)
	}
	var counts map[string]int
	type edit struct {
		start, end int
		value      []byte
	}
	var edits []edit
	for _, n := range textFields(family, root) {
		findings := Find(n.str)
		if len(findings) == 0 {
			continue
		}
		if counts == nil {
			counts = map[string]int{}
		}
		for _, f := range findings {
			counts[f.Type]++
		}
		if mask {
			edits = append(edits, edit{n.start, n.end, encodeString(r.Mask(n.str, findings))})
		}
	}
	if len(edits) == 0 {
		return body, counts, nil
	}
	slices.SortFunc(edits, func(a, b edit) int { return cmp.Compare(a.start, b.start) })
	var out bytes.Buffer
	last := 0
	for _, e := range edits {
		out.Write(body[last:e.start])
		out.Write(e.value)
		last = e.end
	}
	out.Write(body[last:])
	return out.Bytes(), counts, nil
}

func encodeString(s string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// node is a JSON value and where it is in the body.
type node struct {
	start, end int
	kind       byte   // '{', '[', '"' for a string, or 0 for other values
	str        string // a string's value
	keys       []string
	vals       []*node // an object's values, in the order of keys, or an array's elements
}

// all returns every value of key in an object. JSON allows a key twice,
// and providers may read either, so both are checked.
func (n *node) all(key string) []*node {
	if n.kind != '{' {
		return nil
	}
	var out []*node
	for i, k := range n.keys {
		if k == key {
			out = append(out, n.vals[i])
		}
	}
	return out
}

// textFields returns the string values in a request body that hold text
// for the model, each once.
func textFields(family string, root *node) []*node {
	var out []*node
	seen := map[*node]bool{}
	text := func(n *node) {
		if n.kind == '"' && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	// content is a string or a list of blocks, some of which hold text.
	var content func(n *node)
	content = func(n *node) {
		if n.kind != '[' {
			text(n)
			return
		}
		for _, block := range n.vals {
			for _, typ := range block.all("type") {
				switch typ.str {
				case "text":
					for _, t := range block.all("text") {
						text(t)
					}
				case "tool_result":
					for _, c := range block.all("content") {
						content(c)
					}
				case "document":
					for _, src := range block.all("source") {
						document(src, text, content)
					}
				}
			}
		}
	}
	if family == "anthropic" {
		for _, s := range root.all("system") {
			content(s)
		}
	}
	for _, messages := range root.all("messages") {
		if messages.kind == '[' {
			for _, m := range messages.vals {
				for _, c := range m.all("content") {
					content(c)
				}
			}
		}
	}
	return out
}

// document finds the text of an Anthropic document source: plain text in
// data, or content blocks.
func document(src *node, text, content func(*node)) {
	for _, typ := range src.all("type") {
		switch typ.str {
		case "text":
			for _, d := range src.all("data") {
				text(d)
			}
		case "content":
			for _, c := range src.all("content") {
				content(c)
			}
		}
	}
}

// parse reads a JSON document into nodes with their positions.
func parse(body []byte) (*node, error) {
	p := &parser{dec: json.NewDecoder(bytes.NewReader(body)), body: body}
	p.dec.UseNumber()
	return p.value()
}

type parser struct {
	dec  *json.Decoder
	body []byte
}

// token reads a token and returns where it starts and ends: between the
// end of the previous token and the next one, there can only be
// whitespace, commas and colons.
func (p *parser) token() (tok json.Token, start, end int, err error) {
	start = int(p.dec.InputOffset())
	if tok, err = p.dec.Token(); err != nil {
		return nil, 0, 0, err
	}
	for start < len(p.body) && strings.IndexByte(" \t\r\n,:", p.body[start]) >= 0 {
		start++
	}
	return tok, start, int(p.dec.InputOffset()), nil
}

func (p *parser) value() (*node, error) {
	tok, start, end, err := p.token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		n := &node{start: start, kind: '{'}
		if t == '[' {
			n.kind = '['
		}
		for p.dec.More() {
			if n.kind == '{' {
				key, _, _, err := p.token()
				if err != nil {
					return nil, err
				}
				k, _ := key.(string)
				n.keys = append(n.keys, k)
			}
			child, err := p.value()
			if err != nil {
				return nil, err
			}
			n.vals = append(n.vals, child)
		}
		if _, _, n.end, err = p.token(); err != nil { // the closing bracket
			return nil, err
		}
		return n, nil
	case string:
		return &node{start: start, end: end, kind: '"', str: t}, nil
	default:
		return &node{start: start, end: end}, nil
	}
}
