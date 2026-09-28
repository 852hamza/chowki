package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/template"
)

const setupUsage = `Usage:
  chowki setup <TOOL> [--url <URL>] [--key <VIRTUAL_KEY>] [--model <MODEL>]

Prints the settings that connect a tool or SDK to the gateway. It changes no
file. <TOOL> is one of:

  claude-code       Claude Code
  codex             the OpenAI Codex CLI
  gemini-cli        the Gemini CLI
  openai-sdk        the OpenAI SDKs
  anthropic-sdk     the Anthropic SDKs
  google-genai-sdk  the Google Gen AI SDKs
  ollama            Ollama, as a provider of the gateway

Flags:
  --url    the gateway's address; by default $CHOWKI_PUBLIC_URL, or
           http://localhost:8080
  --key    a virtual key to put in the settings; by default a placeholder
  --model  the model to put in the settings, for the tools that need one
`

// setupTemplates are the settings of each tool, from each tool's own
// documentation or source: Claude Code's LLM gateway guide, the model
// providers of the Codex CLI, which speaks only the Responses API, and the
// environment variables that the Gemini CLI and the SDKs read.
var setupTemplates = map[string]string{
	"claude-code": `Claude Code reaches the gateway at {{.URL}}/anthropic.
{{if not .KeyGiven}}
Create a virtual key for it, if you haven't:

  chowki key create --name claude-code
{{end}}
Add the settings to ~/.claude/settings.json, which applies to all your
projects. Don't put them in a project's .claude/settings.json, which the
project shares with everyone who clones it.

{
  "env": {
    "ANTHROPIC_BASE_URL": "{{.URL}}/anthropic",
    "ANTHROPIC_AUTH_TOKEN": "{{.Key}}"
  }
}

Or set them in your shell:

  export ANTHROPIC_BASE_URL={{.URL}}/anthropic
  export ANTHROPIC_AUTH_TOKEN={{.Key}}

Claude Code sends ANTHROPIC_AUTH_TOKEN as a bearer token. Start claude and
run /status: it shows the base URL and the auth token.
`,
	"codex": `The Codex CLI reaches the gateway's Responses API at {{.URL}}/v1.
{{if not .KeyGiven}}
Create a virtual key for it, if you haven't:

  chowki key create --name codex
{{end}}
Add the settings to ~/.codex/config.toml:

model = "{{.Model}}"
model_provider = "chowki"

[model_providers.chowki]
name = "Chowki"
base_url = "{{.URL}}/v1"
env_key = "CHOWKI_API_KEY"
wire_api = "responses"

Then set the key in your shell:

  export CHOWKI_API_KEY={{.Key}}
{{if not .ModelGiven}}
Replace <MODEL> with a model of an OpenAI-compatible provider, such as
openai/gpt-6-sol or ollama/<model>: the gateway serves the Responses API
for those providers only.
{{end}}`,
	"gemini-cli": `The Gemini CLI reaches the gateway at {{.URL}}/gemini.
{{if not .KeyGiven}}
Create a virtual key for it, if you haven't:

  chowki key create --name gemini-cli
{{end}}
Set the variables in your shell, or in ~/.gemini/.env, which the Gemini CLI
reads:

  export GOOGLE_GEMINI_BASE_URL={{.URL}}/gemini
  export GEMINI_API_KEY={{.Key}}

With GOOGLE_GEMINI_BASE_URL set, the Gemini CLI signs in to the gateway
with GEMINI_API_KEY.
`,
	"openai-sdk": `The OpenAI SDKs reach the gateway at {{.URL}}/v1.
{{if not .KeyGiven}}
Create a virtual key for your app, if you haven't:

  chowki key create --name <APP>
{{end}}
Set the variables that the SDKs read:

  export OPENAI_BASE_URL={{.URL}}/v1
  export OPENAI_API_KEY={{.Key}}

Or pass them in code, in Python:

  client = OpenAI(base_url="{{.URL}}/v1", api_key="{{.Key}}")

Name models with their provider, such as openai/gpt-6-sol,
anthropic/claude-sonnet-5 or gemini/gemini-2.5-flash.
`,
	"anthropic-sdk": `The Anthropic SDKs reach the gateway at {{.URL}}/anthropic.
{{if not .KeyGiven}}
Create a virtual key for your app, if you haven't:

  chowki key create --name <APP>
{{end}}
Set the variables that the SDKs read:

  export ANTHROPIC_BASE_URL={{.URL}}/anthropic
  export ANTHROPIC_API_KEY={{.Key}}

Or pass them in code, in Python:

  client = Anthropic(base_url="{{.URL}}/anthropic", api_key="{{.Key}}")
`,
	"google-genai-sdk": `The Google Gen AI SDKs reach the gateway at {{.URL}}/gemini.
{{if not .KeyGiven}}
Create a virtual key for your app, if you haven't:

  chowki key create --name <APP>
{{end}}
Set the variables that the SDKs read:

  export GOOGLE_GEMINI_BASE_URL={{.URL}}/gemini
  export GEMINI_API_KEY={{.Key}}

The Python SDK prefers GOOGLE_API_KEY when it's set, so unset it. Or pass
the settings in code, in Python:

  client = genai.Client(
      api_key="{{.Key}}",
      http_options=types.HttpOptions(base_url="{{.URL}}/gemini"),
  )
`,
	"ollama": `Add Ollama to the providers in chowki.yaml, and restart chowki serve:

providers:
  - name: ollama
    type: openai
    base_url: http://localhost:11434/v1

Ollama needs no key. Send requests to the gateway, at
{{.URL}}/v1, and name Ollama's models as ollama/<model>,
such as ollama/llama3.2. Local models have no price, so their requests cost
$0.
`,
}

func runSetup(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki setup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	url := flags.String("url", "", "")
	key := flags.String("key", "", "")
	model := flags.String("model", "", "")
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		// The tool comes first; flags follow it.
		args = append(args[1:], args[0])
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, setupUsage)
			return exitOK
		}
		fmt.Fprintf(stderr, "chowki setup: %v\n\n%s", err, setupUsage)
		return exitUsage
	}
	if flags.NArg() != 1 {
		fmt.Fprint(stderr, setupUsage)
		return exitUsage
	}
	tool := flags.Arg(0)
	text, ok := setupTemplates[tool]
	if !ok {
		var tools []string
		for name := range setupTemplates {
			tools = append(tools, name)
		}
		slices.Sort(tools)
		fmt.Fprintf(stderr, "chowki setup: unknown tool %q; use one of: %s\n", tool, strings.Join(tools, ", "))
		return exitUsage
	}
	data := struct {
		URL, Key, Model      string
		KeyGiven, ModelGiven bool
	}{URL: *url, Key: *key, Model: *model, KeyGiven: *key != "", ModelGiven: *model != ""}
	if data.URL == "" {
		data.URL = os.Getenv("CHOWKI_PUBLIC_URL")
	}
	if data.URL == "" {
		data.URL = "http://localhost:8080"
	}
	data.URL = strings.TrimRight(data.URL, "/")
	if !data.KeyGiven {
		data.Key = "<VIRTUAL_KEY>"
	}
	if !data.ModelGiven {
		data.Model = "<MODEL>"
	}
	if err := template.Must(template.New(tool).Parse(text)).Execute(stdout, data); err != nil {
		fmt.Fprintf(stderr, "chowki setup: %v\n", err)
		return exitError
	}
	return exitOK
}
