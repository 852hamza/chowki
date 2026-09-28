---
title: Store provider keys in Chowki
description: Keep the keys of OpenAI, Anthropic, Gemini and other providers in Chowki's database, encrypted, instead of in environment variables.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

A provider's key can come from an environment variable, or from Chowki's database, where
`chowki provider set-key` stores it encrypted with the master key. Stored keys suit a server where
you'd rather not keep keys in `.env` files or a service's environment.

## Before you begin

- A Chowki folder set up with `chowki init`, and the providers in its `chowki.yaml`.
- The master key. A stored key opens only with the master key that stored it, so back up the master
  key with the database.

## Store a key

1. See where each provider's key comes from:

   ```sh
   chowki provider list
   ```

   Output, with an OpenAI key in `.env`:

   ```text
   NAME       TYPE       BASE URL                                   KEY
   openai     openai     https://api.openai.com/v1                  OPENAI_API_KEY
   anthropic  anthropic  https://api.anthropic.com                  missing: set ANTHROPIC_API_KEY, or run chowki provider set-key anthropic
   gemini     gemini     https://generativelanguage.googleapis.com  missing: set GEMINI_API_KEY, or run chowki provider set-key gemini
   ```

2. Store the key of a provider. `set-key` reads the key from standard input, never from its
   arguments, which your shell history and the process list would show. In bash or zsh, type or
   paste the key after the first command, and press Enter; it doesn't show:

   ```sh
   read -rs KEY && echo "$KEY" | chowki provider set-key anthropic
   unset KEY
   ```

   Output:

   ```text
   Stored the key of anthropic, encrypted with the master key. Restart chowki serve to use it.
   ```

3. Restart `chowki serve`. Its log says which providers use a stored key:
   `"msg":"using stored provider keys","providers":["anthropic"]`.

`chowki provider list` now shows the key as `stored`, with the time it was stored:

```text
anthropic  anthropic  https://api.anthropic.com                  stored 2026-09-28 01:24 UTC
```

To replace a key, run `set-key` again. To remove one, run `chowki provider remove-key <NAME>`, then
restart `chowki serve`. Each change goes to the audit log.

## How Chowki chooses a key

1. The environment variable that the provider's `api_key_env` names, from the environment or
   `.env`, when it's set. It wins, so that a deployment can override a stored key.
2. Otherwise, the provider's stored key.
3. Otherwise, the provider has no key, and its requests fail with `provider_key_missing`, unless it
   needs none, as a local Ollama server doesn't.

Chowki stores each key sealed with AES-256-GCM, under a key derived from the master key, and bound
to its provider's name, so that a key copied to another provider doesn't open. No part of a key is
stored in the clear, and Chowki never logs keys.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `chowki provider set-key: pipe the key in, so that it doesn't show` | The key would be typed where the terminal shows it. | Pipe it in, as in step 2. |
| `chowki doctor` says that the stored key doesn't open with this master key | The master key changed since the key was stored. | Store the key again with `chowki provider set-key <NAME>`. |
| `provider list` shows `OPENAI_API_KEY, which wins over the stored key` | The variable is set, in the environment or `.env`. | Remove it to use the stored key. |
| `the configuration has no provider "<NAME>"` | `set-key` stores keys only for the providers of `chowki.yaml`. | Add the provider under `providers` first. |

## Related

- [Check your setup](check-your-setup.md) ·
  [Configuration reference](../reference/configuration.md)
