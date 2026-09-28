package main

import "testing"

func TestMatcherReplace(t *testing.T) {
	m := newMatcher(
		[]string{"widget.example", "github.com/acme/widget", "https://github.com/acme/widget", "a.example", "b.example"},
		[]string{"gadget.example", "github.com/zeta/widget", "https://github.com/zeta/widget", "b.example", "c.example"},
	)
	tests := []struct{ name, in, want string }{
		{"repo URL", "see https://github.com/acme/widget.", "see https://github.com/zeta/widget."},
		{"clone URL", "git clone https://github.com/acme/widget.git", "git clone https://github.com/zeta/widget.git"},
		{"import path", `"github.com/acme/widget/internal/x"`, `"github.com/zeta/widget/internal/x"`},
		{"subdomain", "https://docs.widget.example/start", "https://docs.gadget.example/start"},
		{"email", "security@widget.example", "security@gadget.example"},
		{"domain at end of sentence", "Visit widget.example.", "Visit gadget.example."},
		{"domain at end of text", "widget.example", "gadget.example"},
		{"longer repo name", "github.com/acme/widget2 github.com/acme/widget-x", "github.com/acme/widget2 github.com/acme/widget-x"},
		{"longer domain label", "mywidget.example widget.examples", "mywidget.example widget.examples"},
		{"longer host name", "widget.example.net", "widget.example.net"},
		{"bare owner and repo", "acme widget", "acme widget"},
		{"no chained replacement", "a.example b.example", "b.example c.example"},
		{"multibyte neighbors", "→widget.example←", "→gadget.example←"},
		{"default value", "${IMAGE:-github.com/acme/widget:latest}", "${IMAGE:-github.com/zeta/widget:latest}"},
		{"hyphen before a value", "x-github.com/acme/widget my-widget.example", "x-github.com/acme/widget my-widget.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := m.replace(tt.in)
			if got != tt.want {
				t.Errorf("replace(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if changed != (tt.in != tt.want) {
				t.Errorf("replace(%q) changed = %v, want %v", tt.in, changed, tt.in != tt.want)
			}
		})
	}
}

func TestMatcherPrefersLongestValue(t *testing.T) {
	m := newMatcher([]string{"widget.example", "https://widget.example/docs"}, nil)
	pos, i := m.find("go to https://widget.example/docs now", 0)
	if pos != 6 || m.olds[i] != "https://widget.example/docs" {
		t.Errorf("find() = %d, %q; want 6, the docs URL", pos, m.olds[i])
	}
	if pos, _ := newMatcher(nil, nil).find("anything", 0); pos != -1 {
		t.Errorf("empty matcher find() = %d, want -1", pos)
	}
}

// The full name, <owner>/<repo>, is replaced where it stands alone, such as
// after gh --repo, and never inside a longer name.
func TestMatcherFullName(t *testing.T) {
	m := newMatcher([]string{"github.com/acme/widget", "acme/widget"}, []string{"github.com/zeta/widget", "zeta/widget"})
	for in, want := range map[string]string{
		"gh attestation verify x --repo acme/widget": "gh attestation verify x --repo zeta/widget",
		"see github.com/acme/widget/issues":          "see github.com/zeta/widget/issues",
		"myacme/widget acme/widgets acme/widget-x":   "myacme/widget acme/widgets acme/widget-x",
	} {
		if got, _ := m.replace(in); got != want {
			t.Errorf("replace(%q) = %q, want %q", in, got, want)
		}
	}
}
