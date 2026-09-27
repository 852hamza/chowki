package main

import (
	"cmp"
	"slices"
	"strings"
)

// matcher finds identity values in text. At each position it prefers the
// longest value, and it matches whole tokens only: "example.com" doesn't match
// inside "myexample.com", "example.company" or "example.com.evil.net", and
// "github.com/acme/app" doesn't match inside "github.com/acme/app2". It does
// match "docs.example.com", "user@example.com" and "github.com/acme/app.git".
type matcher struct {
	olds []string // longest first
	news []string // replacement for olds[i]; nil for a find-only matcher
}

func newMatcher(olds, news []string) *matcher {
	idx := make([]int, len(olds))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		return cmp.Or(cmp.Compare(len(olds[b]), len(olds[a])), strings.Compare(olds[a], olds[b]))
	})
	m := &matcher{}
	for _, i := range idx {
		m.olds = append(m.olds, olds[i])
		if news != nil {
			m.news = append(m.news, news[i])
		}
	}
	return m
}

// find returns the position of the first match at or after from and the index
// of the matched value, or -1, -1.
func (m *matcher) find(text string, from int) (pos, idx int) {
	for i := from; i < len(text); i++ {
		for j, v := range m.olds {
			if matchAt(text, i, v) {
				return i, j
			}
		}
	}
	return -1, -1
}

// replace returns text with every match replaced, and whether it changed.
func (m *matcher) replace(text string) (string, bool) {
	var b strings.Builder
	last := 0
	for {
		pos, j := m.find(text, last)
		if pos < 0 {
			break
		}
		b.WriteString(text[last:pos])
		b.WriteString(m.news[j])
		last = pos + len(m.olds[j])
	}
	if last == 0 { // values are never empty, so a match always moves last
		return text, false
	}
	b.WriteString(text[last:])
	return b.String(), true
}

// matchAt reports whether v occurs as a whole token at text[i:].
func matchAt(text string, i int, v string) bool {
	if !strings.HasPrefix(text[i:], v) || i > 0 && isWordByte(text[i-1]) {
		return false
	}
	end := i + len(v)
	if end == len(text) {
		return true
	}
	c := text[end]
	if isWordByte(c) {
		return false
	}
	// For a bare domain, a dot followed by a label continues the host name.
	// Paths may end in a dot plus a suffix, as in "github.com/acme/app.git".
	isDomain := !strings.Contains(v, "/")
	if c == '.' && isDomain && end+1 < len(text) && isWordByte(text[end+1]) {
		return false
	}
	return true
}

func isWordByte(c byte) bool {
	return c == '-' || c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}
