package pipeline

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestObjectWith(t *testing.T) {
	tests := []struct {
		name, body string
		values     map[string]string
		want       string
	}{
		{"replace keeps every other byte", `{ "model" : "openai/gpt" ,"messages":[{"x":1}],  "n":1}`,
			map[string]string{"model": `"gpt"`}, `{ "model" : "gpt" ,"messages":[{"x":1}],  "n":1}`},
		{"add to an object", `{"model":"m"}`, map[string]string{"stream_options": `{"include_usage":true}`},
			`{"model":"m","stream_options":{"include_usage":true}}`},
		{"add to an empty object", `{ }`, map[string]string{"b": "2", "a": "1"}, `{ "a":1,"b":2}`},
		{"replace and add", `{"model":"p/m","stream":true}`,
			map[string]string{"model": `"m"`, "stream_options": `{"include_usage":true}`},
			`{"model":"m","stream":true,"stream_options":{"include_usage":true}}`},
		{"replace nested object", `{"stream_options":{"include_obfuscation":false},"stream":true}`,
			map[string]string{"stream_options": `{"include_obfuscation":false,"include_usage":true}`},
			`{"stream_options":{"include_obfuscation":false,"include_usage":true},"stream":true}`},
		{"trailing whitespace", "{\"a\":1}\n", map[string]string{"a": "2"}, "{\"a\":2}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parseObject([]byte(tt.body))
			if err != nil {
				t.Fatalf("parseObject() error = %v", err)
			}
			values := map[string][]byte{}
			for k, v := range tt.values {
				values[k] = []byte(v)
			}
			got := string(o.with(values))
			if got != tt.want {
				t.Errorf("with() = %s, want %s", got, tt.want)
			}
			if !json.Valid([]byte(got)) {
				t.Errorf("with() returned invalid JSON: %s", got)
			}
		})
	}
}

func TestParseObjectErrors(t *testing.T) {
	for body, want := range map[string]string{
		``:                    "must be a JSON object",
		`[1]`:                 "must be a JSON object",
		`"x"`:                 "must be a JSON object",
		`{"a":}`:              "invalid JSON",
		`{"a":1`:              "invalid JSON",
		`{"a":1,"a":2}`:       `"a" appears twice`,
		`{"a":1} {"b":2}`:     "data after the object",
		`{"a":1}x`:            "invalid JSON",
		`{"model":"m",}`:      "invalid JSON",
		`{"a":{"a":1,"a":2}}`: "", // duplicates below the top level are the provider's business
	} {
		_, err := parseObject([]byte(body))
		switch {
		case want == "" && err != nil:
			t.Errorf("parseObject(%q) error = %v", body, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("parseObject(%q) error = %v, want %q", body, err, want)
		}
	}
}

func FuzzObject(f *testing.F) {
	for _, seed := range []string{`{"model":"m","stream":true}`, `{}`, `{"a":[1,{"b":null}]}`, ` {"x" : "y"} `} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		o, err := parseObject([]byte(body))
		if err != nil {
			return
		}
		out := o.with(map[string][]byte{"model": []byte(`"fuzz"`), "zz_added": []byte(`true`)})
		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("with() made invalid JSON from %q: %s", body, out)
		}
		if got["model"] != "fuzz" || got["zz_added"] != true {
			t.Errorf("with() lost an edit: %s", out)
		}
	})
}
