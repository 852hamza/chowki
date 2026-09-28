---
title: Connect the OpenAI, Anthropic and Gemini SDKs
description: Point apps that use the official SDKs at Chowki with two environment variables.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

The official SDKs read their base URL and key from environment variables, so an app reaches Chowki
without a change to its code.

## Before you begin

- Chowki running, and a virtual key for your app: `chowki key create --name <APP>`.

## Set the variables

`chowki setup` prints the variables of each SDK; with `--key`, it fills in the key:

| SDK | Command | Variables |
|---|---|---|
| OpenAI | `chowki setup openai-sdk` | `OPENAI_BASE_URL=http://localhost:8080/v1` and `OPENAI_API_KEY` |
| Anthropic | `chowki setup anthropic-sdk` | `ANTHROPIC_BASE_URL=http://localhost:8080/anthropic` and `ANTHROPIC_API_KEY` |
| Google Gen AI | `chowki setup google-genai-sdk` | `GOOGLE_GEMINI_BASE_URL=http://localhost:8080/gemini` and `GEMINI_API_KEY` |

For example:

```sh
chowki setup google-genai-sdk
```

Output:

```text
The Google Gen AI SDKs reach the gateway at http://localhost:8080/gemini.

Create a virtual key for your app, if you haven't:

  chowki key create --name <APP>

Set the variables that the SDKs read:

  export GOOGLE_GEMINI_BASE_URL=http://localhost:8080/gemini
  export GEMINI_API_KEY=<VIRTUAL_KEY>

The Python SDK prefers GOOGLE_API_KEY when it's set, so unset it. Or pass
the settings in code, in Python:

  client = genai.Client(
      api_key="<VIRTUAL_KEY>",
      http_options=types.HttpOptions(base_url="http://localhost:8080/gemini"),
  )
```

With the variables set, `OpenAI()`, `Anthropic()` and `genai.Client()` in Python reach Chowki
without arguments.

## Related

- [Call Anthropic and Gemini models with OpenAI SDKs](call-any-model-with-openai-sdks.md) ·
  [API reference](../reference/api.md)
