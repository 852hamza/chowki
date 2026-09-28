package translate

import (
	"strings"
	"testing"
)

// Translated answers write <, > and & as they are, as the providers do.
func TestFromGeminiKeepsCharacters(t *testing.T) {
	body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"Dev & Ops: use <b> and a -> b"}]},` +
		`"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":5}}`
	out, err := FromGemini([]byte(body), "gemini-2.5-flash", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"Dev & Ops: use <b> and a -> b"`) || strings.Contains(string(out), `\u00`) {
		t.Errorf("FromGemini = %s; want the text as it is", out)
	}
}
