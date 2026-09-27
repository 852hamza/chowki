package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseDotEnv(t *testing.T) {
	data := strings.Join([]string{
		"# comment",
		"",
		"PLAIN=value",
		"  SPACED = value with spaces  ",
		"export EXPORTED=yes",
		"export\tTABBED=yes",
		"EMPTY=",
		"COMMENT_ONLY= # nothing here",
		"INLINE=abc # comment",
		"HASH=abc#def",
		"SINGLE='literal \\n $HOME # not a comment'",
		`DOUBLE="line\nbreak \"quoted\" back\\slash tab\t"`,
		`QUOTED_COMMENT="value" # comment`,
		"CRLF=value\r",
		"exporter=kept",
		"DUP=first",
		"DUP=second",
	}, "\n")
	got, err := ParseDotEnv([]byte(data))
	if err != nil {
		t.Fatalf("ParseDotEnv() error = %v", err)
	}
	want := map[string]string{
		"PLAIN":          "value",
		"SPACED":         "value with spaces",
		"EXPORTED":       "yes",
		"TABBED":         "yes",
		"EMPTY":          "",
		"COMMENT_ONLY":   "",
		"INLINE":         "abc",
		"HASH":           "abc#def",
		"SINGLE":         `literal \n $HOME # not a comment`,
		"DOUBLE":         "line\nbreak \"quoted\" back\\slash tab\t",
		"QUOTED_COMMENT": "value",
		"CRLF":           "value",
		"exporter":       "kept",
		"DUP":            "second",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseDotEnv() =\n%q\nwant\n%q", got, want)
	}
}

func TestParseDotEnvErrors(t *testing.T) {
	const secret = "sk-EXAMPLE-secret"
	tests := []struct{ data, want string }{
		{"NO_EQUALS " + secret, "line 1: want KEY=VALUE"},
		{"1BAD=" + secret, "line 1: want KEY=VALUE"},
		{"\nKEY='" + secret, "line 2: KEY: missing closing single quote"},
		{`KEY="` + secret, "missing closing double quote"},
		{`KEY="` + secret + `\`, "missing closing double quote"},
		{`KEY="` + secret + `\x"`, "unknown escape"},
		{`KEY="` + secret + `" trailing`, "unexpected text after the closing quote"},
		{"KEY='" + secret + "' trailing", "unexpected text after the closing quote"},
		{"export", "want KEY=VALUE"},
	}
	for _, tt := range tests {
		_, err := ParseDotEnv([]byte(tt.data))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("ParseDotEnv(%q) error = %v, want %q", tt.data, err, tt.want)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("ParseDotEnv(%q) error shows the value: %v", tt.data, err)
		}
	}
}

func TestDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("FROM_FILE=file\nBOTH=file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOTH", "environment")
	lookup, err := DotEnv(path)
	if err != nil {
		t.Fatalf("DotEnv() error = %v", err)
	}
	for name, want := range map[string]string{"FROM_FILE": "file", "BOTH": "environment"} {
		if got, ok := lookup(name); !ok || got != want {
			t.Errorf("lookup(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	if _, ok := lookup("CHOWKI_TEST_UNSET_VARIABLE"); ok {
		t.Error("lookup of an unset variable succeeded")
	}

	missing, err := DotEnv(filepath.Join(dir, "missing.env"))
	if err != nil || missing == nil {
		t.Fatalf("DotEnv() of a missing file = %v, want the plain environment", err)
	}
	if got, _ := missing("BOTH"); got != "environment" {
		t.Errorf("lookup without a file = %q, want environment", got)
	}

	if err := os.WriteFile(path, []byte("BAD LINE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DotEnv(path); err == nil || !strings.Contains(err.Error(), path+": line 1") {
		t.Errorf("DotEnv() error = %v, want the file name and line", err)
	}
}
