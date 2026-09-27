package secretbox

import (
	"bytes"
	"errors"
	"testing"
)

func newBox(t *testing.T, master byte, purpose string) *Box {
	t.Helper()
	b, err := New(bytes.Repeat([]byte{master}, KeySize), purpose)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealAndOpen(t *testing.T) {
	box := newBox(t, 1, "cache")
	plaintext, data := []byte(`{"answer":"secret reply"}`), []byte("hash-1")
	env := box.Seal(plaintext, data)
	if bytes.Contains(env, plaintext[2:]) {
		t.Fatal("the envelope contains the plaintext")
	}
	if again := box.Seal(plaintext, data); bytes.Equal(env, again) {
		t.Error("two envelopes of the same plaintext are equal; the nonce must be random")
	}
	got, err := box.Open(env, data)
	if err != nil || !bytes.Equal(got, plaintext) {
		t.Fatalf("Open() = %q, %v", got, err)
	}

	tampered := bytes.Clone(env)
	tampered[len(tampered)-1] ^= 1
	unknownVersion := bytes.Clone(env)
	unknownVersion[0] = 2
	for name, open := range map[string]func() ([]byte, error){
		"other data":          func() ([]byte, error) { return box.Open(env, []byte("hash-2")) },
		"changed byte":        func() ([]byte, error) { return box.Open(tampered, data) },
		"unknown key version": func() ([]byte, error) { return box.Open(unknownVersion, data) },
		"too short":           func() ([]byte, error) { return box.Open(env[:10], data) },
		"other purpose":       func() ([]byte, error) { return newBox(t, 1, "provider").Open(env, data) },
		"other master key":    func() ([]byte, error) { return newBox(t, 2, "cache").Open(env, data) },
	} {
		if got, err := open(); !errors.Is(err, ErrOpen) || got != nil {
			t.Errorf("%s: Open() = %q, %v; want ErrOpen", name, got, err)
		}
	}
}

func TestNewRejectsAShortKey(t *testing.T) {
	if _, err := New(make([]byte, 16), "cache"); err == nil {
		t.Error("New() accepted a 16-byte master key")
	}
}

func FuzzOpen(f *testing.F) {
	box, err := New(bytes.Repeat([]byte{1}, KeySize), "cache")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(box.Seal([]byte("reply"), []byte("h")), []byte("h"))
	f.Add([]byte{keyVersion}, []byte{})
	f.Fuzz(func(t *testing.T, env, data []byte) {
		if _, err := box.Open(env, data); err == nil && env[0] != keyVersion {
			t.Fatalf("Open(%x) succeeded for an unknown key version", env)
		}
	})
}
