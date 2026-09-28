---
title: Connect the Gemini CLI
description: Point the Gemini CLI at Chowki, so that its requests use a virtual key.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

Route the Gemini CLI through Chowki: it then signs in to Chowki with a virtual key, and Chowki
holds your Gemini API key.

## Before you begin

- Chowki running with a `gemini` provider and its key. See
  [Use Google Gemini](use-google-gemini.md).
- The Gemini CLI installed.

## Connect the Gemini CLI

1. Print the settings:

   ```sh
   chowki setup gemini-cli
   ```

   Output:

   ```text
   The Gemini CLI reaches the gateway at http://localhost:8080/gemini.

   Create a virtual key for it, if you haven't:

     chowki key create --name gemini-cli

   Set the variables in your shell, or in ~/.gemini/.env, which the Gemini CLI
   reads:

     export GOOGLE_GEMINI_BASE_URL=http://localhost:8080/gemini
     export GEMINI_API_KEY=<VIRTUAL_KEY>

   With GOOGLE_GEMINI_BASE_URL set, the Gemini CLI signs in to the gateway
   with GEMINI_API_KEY.
   ```

2. Export the two variables, or add them to `~/.gemini/.env`, with the virtual key in place of
   `<VIRTUAL_KEY>`.
3. Start `gemini`.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Requests fail with `INVALID_API_KEY` | `GEMINI_API_KEY` holds your Gemini key, not a virtual key. | Set it to the virtual key: Chowki holds the Gemini key. |
| The Gemini CLI signs in with Google, or uses Vertex AI | `GOOGLE_GENAI_USE_GCA` or `GOOGLE_GENAI_USE_VERTEXAI` is `true`: the CLI reads them before `GOOGLE_GEMINI_BASE_URL`. | Unset them. |

## Related

- [Use Google Gemini](use-google-gemini.md)
