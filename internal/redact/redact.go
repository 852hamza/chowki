package redact

import (
	"cmp"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"unicode/utf8"
)

// Modes of redaction, for a key or the configuration.
const (
	ModeOff   = "off"   // send requests as they are
	ModeMask  = "mask"  // replace what the detectors find with placeholders
	ModeBlock = "block" // reject requests in which the detectors find something
	ModeAlert = "alert" // send requests as they are, and log what was found
)

// Redactor finds secrets and personal data in text and masks them with
// placeholders that are deterministic, so that the same value always gets
// the same placeholder and provider prompt caching keeps working, and
// that don't reveal the value.
type Redactor struct {
	secret []byte // keys the placeholders' HMAC
	def    string // the mode of keys without their own
}

// New returns a redactor whose placeholders are keyed with secret and whose
// default mode is def.
func New(secret []byte, def string) *Redactor {
	return &Redactor{secret: secret, def: def}
}

// Mode returns the redaction mode of a key whose own mode is keyMode, ""
// for the default.
func (r *Redactor) Mode(keyMode string) string {
	if keyMode == "" {
		return r.def
	}
	return keyMode
}

// Finding is a piece of text that a detector found.
type Finding struct {
	Type       string
	Start, End int // byte offsets in the text
}

// Find returns what the detectors find in text, in order and without
// overlaps: of two overlapping findings, the one that starts first wins,
// then the more specific type.
func Find(text string) []Finding {
	type found struct {
		Finding
		priority int
	}
	var all []found
	var lower string
	candidates := map[int][]window{}
	for p, d := range detectors {
		var ws []window
		switch {
		case d.find != byKeywords:
			if _, ok := candidates[d.find]; !ok {
				candidates[d.find] = finders[d.find](text)
			}
			ws = candidates[d.find]
		case d.lower:
			if lower == "" {
				lower = asciiLower(text)
			}
			ws = keywordWindows(lower, d)
		default:
			ws = keywordWindows(text, d)
		}
		for _, w := range merge(ws) {
			for _, m := range d.re.FindAllStringSubmatchIndex(text[w.start:w.end], -1) {
				s, e := m[2*d.group], m[2*d.group+1]
				if s < 0 {
					continue
				}
				s, e = s+w.start, e+w.start
				if d.valid == nil || d.valid(text[s:e]) {
					all = append(all, found{Finding{d.typ, s, e}, p})
				}
			}
		}
	}
	slices.SortFunc(all, func(a, b found) int {
		return cmp.Or(cmp.Compare(a.Start, b.Start), cmp.Compare(a.priority, b.priority), cmp.Compare(b.End, a.End))
	})
	var out []Finding
	for _, f := range all {
		if len(out) == 0 || f.Start >= out[len(out)-1].End {
			out = append(out, f.Finding)
		}
	}
	return out
}

// keywordWindows returns a window around each keyword of d in hay, with a
// byte of context on both sides for \b, ending at whole characters.
func keywordWindows(hay string, d *detector) []window {
	var ws []window
	for _, kw := range d.keywords {
		for i := 0; ; {
			j := strings.Index(hay[i:], kw)
			if j < 0 {
				break
			}
			at := i + j
			ws = append(ws, window{runeStart(hay, max(at-d.before-1, 0)), runeEnd(hay, min(at+d.after+1, len(hay)))})
			i = at + len(kw)
		}
	}
	return ws
}

// merge joins overlapping windows, so that a region full of keywords is
// scanned once and the running time stays linear.
func merge(ws []window) []window {
	slices.SortFunc(ws, func(a, b window) int { return cmp.Compare(a.start, b.start) })
	var out []window
	for _, w := range ws {
		if n := len(out); n > 0 && w.start <= out[n-1].end {
			out[n-1].end = max(out[n-1].end, w.end)
			continue
		}
		out = append(out, w)
	}
	return out
}

func runeStart(s string, i int) int {
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

func runeEnd(s string, i int) int {
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return i
}

// asciiLower lowercases the ASCII letters of s. Unlike strings.ToLower, it
// keeps every byte where it is, so offsets stay valid.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// Placeholder returns what replaces a value of type typ:
// [REDACTED:<type>:<8 hex digits of an HMAC of the value>].
func (r *Redactor) Placeholder(typ, value string) string {
	mac := hmac.New(sha256.New, r.secret)
	mac.Write([]byte(typ))
	mac.Write([]byte{0})
	mac.Write([]byte(value))
	return "[REDACTED:" + typ + ":" + hex.EncodeToString(mac.Sum(nil))[:8] + "]"
}

// Mask returns text with each finding, from Find, replaced by its
// placeholder.
func (r *Redactor) Mask(text string, findings []Finding) string {
	var b strings.Builder
	last := 0
	for _, f := range findings {
		b.WriteString(text[last:f.Start])
		b.WriteString(r.Placeholder(f.Type, text[f.Start:f.End]))
		last = f.End
	}
	b.WriteString(text[last:])
	return b.String()
}
