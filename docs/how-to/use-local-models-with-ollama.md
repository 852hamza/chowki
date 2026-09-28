---
title: Use local models with Ollama
description: Add Ollama as a provider, and reach local models through Chowki with virtual keys.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

Add a local Ollama server as a provider: apps and agents then reach its models through Chowki,
with virtual keys, rate limits, caching and redaction, and every request recorded.

## Before you begin

- Ollama running on the same machine, with a model pulled, such as `ollama pull llama3.2`.
- A Chowki folder set up with `chowki init`.

## Add Ollama

1. Print the settings:

   ```sh
   chowki setup ollama
   ```

   Output:

   ```text
   Add Ollama to the providers in chowki.yaml, and restart chowki serve:

   providers:
     - name: ollama
       type: openai
       base_url: http://localhost:11434/v1

   Ollama needs no key. Send requests to the gateway, at
   http://localhost:8080/v1, and name Ollama's models as ollama/<model>,
   such as ollama/llama3.2. Local models have no price, so their requests cost
   $0.
   ```

2. Add the `ollama` provider to `chowki.yaml`, and restart `chowki serve`.
3. Send a request with the model named `ollama/<model>`:

   ```sh
   curl http://localhost:8080/v1/chat/completions \
     -H "Authorization: Bearer $CHOWKI_KEY" \
     -H "Content-Type: application/json" \
     -d '{"model":"ollama/llama3.2","messages":[{"role":"user","content":"Say hello."}]}'
   ```

Chowki reaches providers on your own machine or network only while
`security.allow_private_upstreams` is `true`, as `chowki init` sets it. See the
[configuration reference](../reference/configuration.md).

## Related

- [Connect the Codex CLI](connect-codex.md): Ollama serves the Responses API too.
