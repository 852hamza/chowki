---
title: Manage virtual keys
description: Create, list and revoke the virtual keys that people and applications use to send requests through Chowki.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Give each person, application or CI job its own virtual key, so you can see who sends what and
revoke one key without touching the others or your provider keys.

## Before you begin

- A Chowki folder set up with `chowki init`. See
  [Send your first request](../get-started/quickstart.md).
- Run the commands in that folder, or pass `--config <FILE>` with the path of its `chowki.yaml`.
- The commands change the database directly. The gateway can keep running: a revoked key stops
  working with the next request.

## Create a key

1. Create a key for a person or application. Optionally, put it in a project, which groups keys:

   ```sh
   chowki key create --name ci-bot --project team-a
   ```

   Output:

   ```text
   Created virtual key "ci-bot" in project "team-a":

     chowki_EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE

   Copy it now. Chowki stores only a hash of it and can't show it again.
   ```

2. Give the key to its user through a secret store or another private channel.

To limit what a key can spend each month, add `--budget-usd`; see
[Set monthly budgets](set-budgets.md). To limit its requests and tokens per minute, add `--rpm` and
`--tpm`; see [Set rate limits](set-rate-limits.md). To answer repeated requests from the cache, add
`--cache exact`; see [Cache responses](cache-responses.md). To choose what happens to secrets and
personal data in its prompts, add `--redaction`; see
[Redact secrets and personal data](redact-sensitive-data.md).

Chowki stores only the key's prefix, its first 12 characters, and its SHA-256 hash. If a key is
lost, revoke it and create a new one.

## Use a key

Clients send the key in the header that their SDK already uses: `Authorization: Bearer`,
`x-api-key` or `x-goog-api-key`. Chowki never reads a key from the URL, where it could end up in
logs.

## List keys

```sh
chowki key list
```

Output:

```text
PREFIX        NAME      PROJECT  SPENT 2026-09  BUDGET  RPM   TPM     CACHE    REDACTION  CREATED               STATUS
chowki_nklNv  alice     team-a   $3.00          $50.00  none  none    default  default    2026-09-27 10:03 UTC  active
chowki_VFK1E  ci-bot    team-a   $1.00          none    60    100000  default  default    2026-09-27 10:03 UTC  active
chowki_y2pj7  docs-bot  team-a   $0.00          none    none  none    exact    default    2026-09-27 10:03 UTC  active
```

The list shows each key's prefix, never the key itself. It also shows what the key spent this
month (UTC), its monthly budget, its limits of requests (`RPM`) and tokens (`TPM`) per minute,
whether it uses the exact cache (`CACHE`), and its redaction mode (`REDACTION`).

## Restrict a key to some models

By default, a key may use every model and alias. To allow only some, pass a comma-separated list
of models, aliases and patterns, where `*` matches any name within a provider:

```sh
chowki key update --models "fast,openai/*" chowki_VFK1E
```

Output:

```text
Updated the settings of virtual key chowki_VFK1E ("ci-bot"):
  Monthly budget:       none
  Requests per minute:  120
  Tokens per minute:    100000
  Exact cache:          default
  Redaction:            default
  Models:               fast, openai/*
```

A request for another model fails with HTTP status 403 and the models that the key may use:

```json
{"error":{"code":"model_not_allowed","message":"This key may not use the model \"backup/gpt-6-luna\". It may use: fast, openai/*.","param":null,"type":"permission_error"}}
```

Chowki checks the name that the client asks for, so an allowed alias allows all of its targets.
`--models all` allows every model again.

## Revoke a key

1. Revoke a key by its prefix:

   ```sh
   chowki key revoke chowki_VFK1E
   ```

   Output:

   ```text
   Revoked virtual key chowki_VFK1E ("ci-bot"). Requests with it now fail.
   ```

You can also pass the full key instead of the prefix. Revoking is permanent, and revoking a key
twice changes nothing. Chowki records every key it creates or revokes in its audit log.

## Verify

A request with a revoked key fails with an authentication error in the format of the API that the
client speaks. For example, an Anthropic-format request gets:

```json
{"error":{"message":"The virtual key has been revoked.","type":"authentication_error"},"request_id":"req_16eb4533acf75af6e16ea194","type":"error"}
```

An OpenAI-format request gets a response with `"code":"revoked_api_key"`. `chowki key list` shows
the key as `revoked` with the time.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `no virtual key has the prefix "…"` | The prefix is mistyped, or the key belongs to another Chowki database. | Copy the prefix from `chowki key list`. |
| Requests fail with `invalid_api_key` | The key is incomplete, mistyped, or from another Chowki installation. | Check the whole key, or create a new one. |
| Requests fail with `missing_api_key` | The client sent no key, or sent it in another header. | Set the key as the client's API key. |
| Requests fail with `model_not_allowed` | The key's model list doesn't include the model. | Add it with `chowki key update --models`, or ask for an allowed model. |

## Related

- [Set monthly budgets](set-budgets.md) · [Set rate limits](set-rate-limits.md) ·
  [Configuration reference](../reference/configuration.md) ·
  [Architecture](../concepts/architecture.md)
