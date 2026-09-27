package redact

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
)

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

var (
	awsKey = "AKIA" + "IOSFODNN7EXAMPLE"
	jwt    = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" + ".eyJzdWIiOiIxMjM0NTY3ODkwIn0" +
		".dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
)

func TestRequest(t *testing.T) {
	r := New([]byte("k"), ModeMask)
	tests := []struct {
		name, family, body string
		counts             map[string]int
		untouched          []string // parts of the body that must stay as they are
	}{
		{"openai", "openai", `{"model":"m","user":"jane.doe@company.io","messages":[` +
			`{"role":"system","content":"Reply to jane.doe@company.io"},` +
			`{"role":"user","content":[{"type":"text","text":"key ` + awsKey + `"},` +
			`{"type":"image_url","image_url":{"url":"data:image/png;base64,` + awsKey + `"}}]},` +
			`{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{\"k\":\"` + awsKey + `\"}"}}]},` +
			`{"role":"tool","tool_call_id":"c1","content":"card 4111 1111 1111 1111"}]}`,
			map[string]int{TypeEmail: 1, TypeAWSKey: 1, TypeCard: 1},
			[]string{`"user":"jane.doe@company.io"`, `base64,` + awsKey, `\"k\":\"` + awsKey}},
		{"anthropic", "anthropic", `{"model":"m","system":[{"type":"text","text":"Token: ` + jwt + `"}],"messages":[` +
			`{"role":"user","content":"Call +14155552671"},` +
			`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"CNIC 35202-1234567-1"},` +
			`{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"IBAN GB82 WEST 1234 5698 7654 32"}]},` +
			`{"type":"document","source":{"type":"text","media_type":"text/plain","data":"From jane.doe@company.io"}},` +
			`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + awsKey + `"}}]},` +
			`{"role":"assistant","content":[{"type":"thinking","thinking":"the key is ` + awsKey + `","signature":"s"}]}]}`,
			map[string]int{TypeJWT: 1, TypePhone: 1, TypeCNIC: 1, TypeIBAN: 1, TypeEmail: 1},
			[]string{`"data":"` + awsKey, `the key is ` + awsKey}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, counts, err := r.Request(tt.family, []byte(tt.body), true)
			if err != nil || !json.Valid(out) || !maps.Equal(counts, tt.counts) {
				t.Fatalf("Request() = %s, %v, %v; want valid JSON and counts %v", out, counts, err, tt.counts)
			}
			for _, part := range tt.untouched {
				if !strings.Contains(string(out), part) {
					t.Errorf("Request() changed %s:\n%s", part, out)
				}
			}
			if n := strings.Count(string(out), "[REDACTED:"); n != len(tt.counts) {
				t.Errorf("Request() has %d placeholders, want %d:\n%s", n, len(tt.counts), out)
			}
			// Without mask, the body stays as it is and the counts are the same.
			same, counts, err := r.Request(tt.family, []byte(tt.body), false)
			if err != nil || string(same) != tt.body || !maps.Equal(counts, tt.counts) {
				t.Errorf("Request() without mask = %s, %v, %v", same, counts, err)
			}
		})
	}
}

func TestRequestKeepsOtherBytes(t *testing.T) {
	r := New([]byte("k"), ModeMask)
	text := "Héllo 世界 😀 \"quoted\" <tag> & mail jane.doe@company.io\nnext line"
	body := `{ "model" : "m",` + "\n" + `  "messages": [ {"role": "user", "content": ` + jsonString(text) + `} ] }`
	out, counts, err := r.Request("openai", []byte(body), true)
	if err != nil || counts[TypeEmail] != 1 {
		t.Fatalf("Request() = %s, %v, %v", out, counts, err)
	}
	masked := strings.Replace(text, "jane.doe@company.io", r.Placeholder(TypeEmail, "jane.doe@company.io"), 1)
	want := strings.Replace(body, jsonString(text), `"Héllo 世界 😀 \"quoted\" <tag> & mail `+
		r.Placeholder(TypeEmail, "jane.doe@company.io")+`\nnext line"`, 1)
	if string(out) != want {
		t.Errorf("Request() =\n%s\nwant\n%s", out, want)
	}
	var got struct {
		Messages []struct{ Content string }
	}
	if json.Unmarshal(out, &got) != nil || got.Messages[0].Content != masked {
		t.Errorf("the masked text reads %q, want %q", got.Messages[0].Content, masked)
	}
}

func TestRequestWithoutFindings(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`)
	out, counts, err := New(nil, ModeMask).Request("anthropic", body, true)
	if err != nil || counts != nil || &out[0] != &body[0] {
		t.Errorf("Request() = %s, %v, %v; want the same body and no counts", out, counts, err)
	}
	if _, _, err := New(nil, ModeMask).Request("openai", []byte(`{"messages":[`), true); err == nil {
		t.Error("Request() accepted a broken body")
	}
}

func FuzzRequest(f *testing.F) {
	f.Add(`{"messages":[{"role":"user","content":"mail jane.doe@company.io"}]}`)
	f.Add(`{"system":[{"type":"text","text":"x"}],"messages":[{"content":[{"type":"tool_result","content":"+14155552671"}]}]}`)
	f.Add(`{"messages":[{"content":[{"type":"text","type":"text","text":"a@b.co","text":"c@d.co"}]}]}`)
	r := New([]byte("k"), ModeMask)
	f.Fuzz(func(t *testing.T, body string) {
		if !json.Valid([]byte(body)) {
			return
		}
		for _, family := range []string{"openai", "anthropic"} {
			out, _, err := r.Request(family, []byte(body), true)
			if err == nil && !json.Valid(out) {
				t.Fatalf("Request(%s) = %s, which isn't JSON", body, out)
			}
		}
	})
}
