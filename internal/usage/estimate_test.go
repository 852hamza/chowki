package usage

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	base64 := strings.Repeat("iVBORw0KGgo+/=", 100) // 1400 bytes
	tokens := func(textBytes, media int) int64 {
		return int64((textBytes+bytesPerToken-1)/bytesPerToken + media*mediaTokens)
	}
	tests := []struct {
		name string
		body string
		want int64
	}{
		{"empty", ``, 0},
		{"text", `{"a":"bcd"}`, tokens(11, 0)},
		{"whitespace outside strings is free", "{ \"a\" :\n\t\"bcd\" }", tokens(11, 0)},
		{"whitespace inside strings counts", `{"a":"b d"}`, tokens(11, 0)},
		{"escaped quote", `{"a":"x\"y"}`, tokens(12, 0)},
		{"OpenAI image", `{"url":"data:image/png;base64,` + base64 + `"}`, tokens(8, 1)},
		{"escaped data URL", `{"url":"data:image\/png;base64,` + base64 + `"}`, tokens(8, 1)},
		{"data URL with line breaks", `{"url":"data:image/png;base64,AAAA\nBBBB"}`, tokens(8, 1)},
		{"data URL that isn't base64", `{"u":"data:text/plain,hello"}`, tokens(29, 0)},
		{"Anthropic image", `{"type":"base64","data":"` + base64 + `"}`, tokens(25, 1)},
		{"short base64 is text", `{"id":"abc123"}`, tokens(15, 0)},
		{"long text isn't media", `{"t":"` + strings.Repeat("word ", 300) + `"}`, tokens(1508, 0)},
		{"two images", `["data:a;base64,QQ==","data:b;base64,Qg=="]`, tokens(3, 2)},
		{"unterminated string", `{"a":"bc`, tokens(8, 0)},
		{"trailing backslash", `"\`, tokens(2, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EstimateTokens([]byte(tt.body)); got != tt.want {
				t.Errorf("EstimateTokens() = %d, want %d", got, tt.want)
			}
		})
	}
}

func FuzzEstimateTokens(f *testing.F) {
	for _, seed := range []string{``, `{"a":"b"}`, `"data:x;base64,QQ=="`, `"\`, `{"a":"\"\\"}`,
		`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		n := EstimateTokens(body)
		// Each byte adds at most a quarter token, or a whole media file when
		// it's the quote that starts one.
		if n < 0 || n > int64(len(body))*mediaTokens {
			t.Fatalf("EstimateTokens(%q) = %d", body, n)
		}
	})
}
