package pipeline_test

import (
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/testutil"
)

// Error messages read the same in a client's raw JSON as in its SDK:
// encoding/json writes <, > and & as <, > and &, which is
// hard to read in curl's output.
func TestErrorMessagesHaveNoEscapedCharacters(t *testing.T) {
	// A second provider of the OpenAI type, so that a model name without a
	// provider is ambiguous and the router answers with its hint.
	backup := testutil.NewOpenAI(t, testutil.Config{APIKey: backupKey})
	h := newHarness(t, testutil.Config{}, testutil.Config{},
		withProviders(map[string]*testutil.Server{"backup": backup}, nil))
	for _, body := range []string{
		`{"model": "", "messages": [{"role": "user", "content": "hi"}]}`,
		`{"messages": [{"role": "user", "content": "hi"}]}`,
		`{"model": "no-such-model", "messages": [{"role": "user", "content": "hi"}]}`,
	} {
		resp := h.post(t.Context(), "/v1/chat/completions", body)
		if got := readBody(t, resp); resp.StatusCode < 400 || strings.Contains(got, `\u00`) {
			t.Errorf("%s: status %d, body %s; want an error without escaped characters", body, resp.StatusCode, got)
		}
	}
}
