package usage

import (
	"os"
	"path/filepath"
	"testing"
)

// The usage that OpenAI-compatible providers report, in examples from
// their documentation. The provider's quirks must not skew the tokens.
func TestCompatibleProviders(t *testing.T) {
	for _, tc := range []struct {
		file, source string
		want         Usage
	}{
		{"deepseek.json", "https://api-docs.deepseek.com/api/create-chat-completion",
			Usage{Input: 16, Output: 10}},
		// xAI counts reasoning apart from the completion: 32 + 9 + 94 = 135.
		{"xai.json", "https://docs.x.ai/developers/rest-api-reference/inference/chat-completions",
			Usage{Input: 32, Output: 9 + 94, CacheRead: 6, Reasoning: 94}},
		{"mistral.json", "https://docs.mistral.ai/studio-api/conversations/advanced/prompt-caching",
			Usage{Input: 1013, Output: 30, CacheRead: 1008}},
		{"groq.json", "https://console.groq.com/docs/api-reference", Usage{Input: 18, Output: 556}},
		// OpenRouter counts reasoning inside the completion, as OpenAI does.
		{"openrouter.json", "https://openrouter.ai/docs/use-cases/usage-accounting",
			Usage{Input: 194, Output: 2, CacheWrite: 100}},
	} {
		body, err := os.ReadFile(filepath.Join("testdata", "compatible", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		r, err := ParseResponse(OpenAI, body)
		if err != nil || r.Usage == nil || *r.Usage != tc.want || r.Model == "" {
			t.Errorf("%s (from %s): %+v, %v; want usage %+v", tc.file, tc.source, r.Usage, err, tc.want)
		}
	}

	// In a stream, Groq reports usage in x_groq.
	body, err := os.ReadFile(filepath.Join("testdata", "compatible", "groq-stream.sse"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewStream(OpenAI)
	feed(s, string(body))
	if r := s.Report(); r.Usage == nil || *r.Usage != (Usage{Input: 18, Output: 556}) {
		t.Errorf("Groq stream usage = %+v", r.Usage)
	}
}
