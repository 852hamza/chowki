package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSetup(t *testing.T) {
	t.Setenv("CHOWKI_PUBLIC_URL", "")
	key := "chowki_EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE"
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"claude code", []string{"setup", "claude-code"}, []string{`"ANTHROPIC_BASE_URL": "http://localhost:8080/anthropic"`,
			`"ANTHROPIC_AUTH_TOKEN": "<VIRTUAL_KEY>"`, "chowki key create --name claude-code", "~/.claude/settings.json"}},
		{"codex", []string{"setup", "codex", "--model", "openai/gpt-6-sol", "--key", key}, []string{
			`model = "openai/gpt-6-sol"`, `model_provider = "chowki"`, `base_url = "http://localhost:8080/v1"`,
			`wire_api = "responses"`, "export CHOWKI_API_KEY=" + key}},
		{"gemini cli", []string{"setup", "gemini-cli", "--url", "https://gw.example.com/"}, []string{
			"export GOOGLE_GEMINI_BASE_URL=https://gw.example.com/gemini", "export GEMINI_API_KEY=<VIRTUAL_KEY>"}},
		{"openai sdk", []string{"setup", "openai-sdk"}, []string{"export OPENAI_BASE_URL=http://localhost:8080/v1",
			"export OPENAI_API_KEY=<VIRTUAL_KEY>"}},
		{"anthropic sdk", []string{"setup", "anthropic-sdk"}, []string{
			"export ANTHROPIC_BASE_URL=http://localhost:8080/anthropic", "export ANTHROPIC_API_KEY=<VIRTUAL_KEY>"}},
		{"google gen ai sdk", []string{"setup", "google-genai-sdk"}, []string{
			"export GOOGLE_GEMINI_BASE_URL=http://localhost:8080/gemini", "unset it"}},
		{"ollama", []string{"setup", "ollama"}, []string{"base_url: http://localhost:11434/v1", "ollama/llama3.2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != exitOK {
				t.Fatalf("run(%q) = %d\n%s", tc.args, code, stderr.String())
			}
			for _, w := range tc.want {
				if !strings.Contains(stdout.String(), w) {
					t.Errorf("the output lacks %q:\n%s", w, stdout.String())
				}
			}
			if strings.Contains(tc.args[len(tc.args)-1], key) && strings.Contains(stdout.String(), "key create") {
				t.Error("with --key, the output still says to create one")
			}
		})
	}

	// The JSON of Claude Code's settings is valid, and CHOWKI_PUBLIC_URL
	// sets the address.
	t.Setenv("CHOWKI_PUBLIC_URL", "https://gateway.example.com")
	var stdout bytes.Buffer
	run([]string{"setup", "claude-code", "--key", key}, &stdout, &bytes.Buffer{})
	out := stdout.String()
	start, end := strings.Index(out, "{"), strings.LastIndex(out, "}")
	var settings struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(out[start:end+1]), &settings); err != nil ||
		settings.Env["ANTHROPIC_BASE_URL"] != "https://gateway.example.com/anthropic" ||
		settings.Env["ANTHROPIC_AUTH_TOKEN"] != key {
		t.Errorf("settings = %+v, %v", settings, err)
	}

	for _, tc := range []runCase{
		{name: "no tool", args: []string{"setup"}, wantCode: exitUsage, wantStderr: []string{"Usage:"}},
		{name: "unknown tool", args: []string{"setup", "cursor"}, wantCode: exitUsage,
			wantStderr: []string{`unknown tool "cursor"`, "claude-code, codex"}},
		{name: "unknown flag", args: []string{"setup", "codex", "--fast"}, wantCode: exitUsage},
		{name: "help", args: []string{"setup", "--help"}, wantCode: exitOK, wantStdout: []string{"google-genai-sdk"}},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(tc.args, &stdout, &stderr); code != tc.wantCode {
			t.Errorf("%s: run(%q) = %d, want %d", tc.name, tc.args, code, tc.wantCode)
		}
		for _, w := range tc.wantStderr {
			if !strings.Contains(stderr.String(), w) {
				t.Errorf("%s: stderr lacks %q:\n%s", tc.name, w, stderr.String())
			}
		}
		for _, w := range tc.wantStdout {
			if !strings.Contains(stdout.String(), w) {
				t.Errorf("%s: stdout lacks %q", tc.name, w)
			}
		}
	}
}
