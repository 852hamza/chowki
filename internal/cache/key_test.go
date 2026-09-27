package cache

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func mustKey(t *testing.T, r Request) [32]byte {
	t.Helper()
	k, err := Key(r)
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	return k
}

func TestKey(t *testing.T) {
	base := Request{Family: "openai", Endpoint: "/v1/chat/completions", Provider: "openai", Model: "gpt-test",
		Project: 1, Headers: http.Header{}, Body: []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}],"temperature":0}`)}
	want := mustKey(t, base)

	same := map[string]func(r *Request){
		"whitespace and field order": func(r *Request) {
			r.Body = []byte("{ \"temperature\": 0,\n \"messages\": [{\"content\": \"hi\", \"role\": \"user\"}] }")
		},
		"volatile fields": func(r *Request) {
			r.Body = []byte(`{"model":"fast","user":"u1","metadata":{"id":2},"stream":false,"stream_options":null,` +
				`"messages":[{"role":"user","content":"hi"}],"temperature":0}`)
		},
	}
	for name, change := range same {
		r := base
		change(&r)
		if got := mustKey(t, r); got != want {
			t.Errorf("%s changed the key", name)
		}
	}

	different := map[string]func(r *Request){
		"message":  func(r *Request) { r.Body = bytes.Replace(r.Body, []byte(`"hi"`), []byte(`"hello"`), 1) },
		"number":   func(r *Request) { r.Body = bytes.Replace(r.Body, []byte(`:0}`), []byte(`:0.5}`), 1) },
		"family":   func(r *Request) { r.Family = "anthropic" },
		"endpoint": func(r *Request) { r.Endpoint = "/v1/other" },
		"provider": func(r *Request) { r.Provider = "local" },
		"model":    func(r *Request) { r.Model = "gpt-other" },
		"project":  func(r *Request) { r.Project = 2 },
		"a header": func(r *Request) { r.Headers = http.Header{"Anthropic-Beta": {"feature-1"}} },
		"new field": func(r *Request) {
			r.Body = []byte(`{"messages":[{"role":"user","content":"hi"}],"temperature":0,"n":2}`)
		},
	}
	for name, change := range different {
		r := base
		change(&r)
		if got := mustKey(t, r); got == want {
			t.Errorf("a different %s kept the key", name)
		}
	}

	if _, err := Key(Request{Body: []byte(`not JSON`)}); err == nil {
		t.Error("Key() accepted a body that isn't JSON")
	}
}

// The key of a JSON object doesn't depend on its formatting.
func FuzzKey(f *testing.F) {
	for _, seed := range []string{`{}`, `{"a":1,"b":[true,null,"x"]}`, `{"messages":[{"content":"é<>&"}]}`,
		`{"n":1e400}`, `not JSON`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		k, err := Key(Request{Body: body})
		if err != nil {
			return
		}
		var indented bytes.Buffer
		if json.Indent(&indented, body, "", "  ") != nil {
			return // Key reads the first JSON value only; Indent needs exactly one
		}
		if again, err := Key(Request{Body: indented.Bytes()}); err != nil || again != k {
			t.Fatalf("indenting %q changed the key: %v", body, err)
		}
	})
}
