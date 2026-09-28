package policy

import "testing"

func TestAllowsModel(t *testing.T) {
	for _, tt := range []struct {
		patterns []string
		model    string
		want     bool
	}{
		{nil, "gpt-5", true},
		{[]string{}, "anything", true},
		{[]string{"fast"}, "fast", true},
		{[]string{"fast"}, "openai/gpt-5", false},
		{[]string{"openai/*"}, "openai/gpt-5", true},
		{[]string{"openai/*"}, "gpt-5", false},
		{[]string{"openai/*"}, "anthropic/claude-sonnet-5", false},
		{[]string{"claude-*", "gemini-*"}, "gemini-3-flash", true},
		// A malformed pattern matches nothing, rather than everything.
		{[]string{"["}, "[", false},
	} {
		if got := AllowsModel(tt.patterns, tt.model); got != tt.want {
			t.Errorf("AllowsModel(%q, %q) = %v, want %v", tt.patterns, tt.model, got, tt.want)
		}
	}
}
