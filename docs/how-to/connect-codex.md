---
title: Connect the Codex CLI
description: Add Chowki to the OpenAI Codex CLI as a model provider.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

Add Chowki to the Codex CLI as a model provider: Codex then calls models through Chowki's Responses
API with a virtual key.

## Before you begin

- Chowki running, with an OpenAI-compatible provider, such as `openai` or Ollama. The Codex CLI
  speaks only OpenAI's Responses API, which Chowki relays to OpenAI-compatible providers, not to
  Anthropic or Gemini ones.
- The Codex CLI installed.

## Connect the Codex CLI

1. Print the settings, with the model to use:

   ```sh
   chowki setup codex --model openai/gpt-6-sol
   ```

   Output:

   ```text
   The Codex CLI reaches the gateway's Responses API at http://localhost:8080/v1.

   Create a virtual key for it, if you haven't:

     chowki key create --name codex

   Add the settings to ~/.codex/config.toml:

   model = "openai/gpt-6-sol"
   model_provider = "chowki"

   [model_providers.chowki]
   name = "Chowki"
   base_url = "http://localhost:8080/v1"
   env_key = "CHOWKI_API_KEY"
   wire_api = "responses"

   Then set the key in your shell:

     export CHOWKI_API_KEY=<VIRTUAL_KEY>
   ```

2. Add the settings to `~/.codex/config.toml`, and export `CHOWKI_API_KEY` with the virtual key.
3. Start `codex`.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Codex says that `wire_api = "chat"` is no longer supported | The provider's settings use the chat API. | Set `wire_api = "responses"`, as `chowki setup codex` prints it. |
| Requests fail with `wrong_endpoint` | The model belongs to an Anthropic or Gemini provider, which the Responses API doesn't reach. | Choose a model of an OpenAI-compatible provider. |

## Related

- [Use local models with Ollama](use-local-models-with-ollama.md)
