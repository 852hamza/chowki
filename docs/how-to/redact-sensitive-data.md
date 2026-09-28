---
title: Redact secrets and personal data
description: Mask, block or report API keys, private keys and personal data in prompts before they reach a provider.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Chowki checks the text of every request for secrets, such as API keys and private keys, and for
personal data, such as email addresses and card numbers. By default, it replaces what it finds
with placeholders before the request leaves your network. Choose, per virtual key, whether it
masks, blocks, only reports, or doesn't look.

## Before you begin

- A Chowki folder set up with `chowki init`. See
  [Send your first request](../get-started/quickstart.md).
- Run the commands in that folder, or pass `--config <FILE>` with the path of its `chowki.yaml`.

> **Important:** In `mask` mode, the model sees placeholders instead of the values, and may write
> them into its answers, for example into code. Where requests must carry personal data on
> purpose, set the key's mode to `alert` or `off`.

## What Chowki finds

| Type | What it matches |
|---|---|
| `private_key` | PEM private key blocks, such as `-----BEGIN RSA PRIVATE KEY-----` |
| `aws_access_key` | AWS access key IDs |
| `github_token`, `slack_token`, `openai_key`, `anthropic_key`, `google_api_key`, `stripe_key` | Tokens and keys of these services, by their formats |
| `jwt` | JSON Web Tokens |
| `chowki_key` | Chowki's own virtual keys |
| `secret` | Values of `password`, `token`, `api_key` and similar settings, such as `password = S3cr3t9x`, when the value looks random |
| `email` | Email addresses, except in the domains reserved for examples, such as `example.com` |
| `card_number` | Card numbers of the major networks that pass the Luhn check |
| `iban` | IBANs, including Pakistani ones, that pass the check-digit test |
| `cnic` | Pakistani CNIC numbers, such as `35202-1234567-1` |
| `pk_mobile` | Pakistani mobile numbers, such as `0300-1234567` and `+92 300 1234567` |
| `phone` | Phone numbers in international format, such as `+14155552671` |

Chowki reads only text: message contents and their text parts, system prompts, tool results and
text documents. It never reads images, audio or files, and it doesn't change tool calls that the
model made.

## Choose a mode

| Mode | What happens |
|---|---|
| `mask` | The default. Chowki replaces each value with a placeholder, such as `[REDACTED:email:b443e741]`, and forwards the request. The same value always gets the same placeholder, so the model can tell values apart and provider prompt caching keeps working; the placeholder doesn't reveal the value. |
| `block` | Chowki rejects the request with HTTP status 400 and an error that names the types it found, never the values. |
| `alert` | Chowki forwards the request unchanged, and logs a warning with the number of findings of each type. |
| `off` | Chowki doesn't check the request. |

1. To set the mode of a key, pass its prefix, as `chowki key list` shows it:

   ```sh
   chowki key update --redaction block chowki_jlIEY
   ```

   Output:

   ```text
   Updated the settings of virtual key chowki_jlIEY ("docs-bot"):
     Monthly budget:       none
     Requests per minute:  none
     Tokens per minute:    none
     Exact cache:          exact
     Redaction:            block
   ```

   `chowki key create` takes `--redaction` too. `--redaction default` makes the key follow
   `defaults.redaction` in `chowki.yaml`.

2. To change the mode of every key that doesn't have its own, set `defaults.redaction` in
   `chowki.yaml`, or the environment variable `CHOWKI_DEFAULTS_REDACTION`, and restart
   `chowki serve`. See the [configuration reference](../reference/configuration.md).

## Verify

In `mask` mode, the response to a request with findings has the header `X-Chowki-Redactions`,
the number of values that Chowki replaced:

```text
HTTP/1.1 200 OK
Content-Type: application/json
X-Chowki-Cache: miss
X-Chowki-Cost-Usd: 1.00000000
X-Chowki-Redactions: 2
X-Chowki-Request-Id: req_de0f0f9aabac480d5630a215
```

For the message `Email the report to jane.doe@company.io. The deploy key is <AWS ACCESS KEY ID>.`,
the provider gets:

```json
{"model":"gpt-6-sol","messages":[{"role":"user","content":"Email the report to [REDACTED:email:b443e741]. The deploy key is [REDACTED:aws_access_key:14bd61f2]."}]}
```

The request's line in the gateway's log counts the findings by type, and never holds the values:
`"redactions":{"aws_access_key":1,"email":1}`.

In `block` mode, an OpenAI-format request with an AWS access key ID gets:

```json
{"error":{"code":"sensitive_data_blocked","message":"The request contains data that this key may not send: aws_access_key. Remove it and send the request again.","param":null,"type":"invalid_request_error"}}
```

An Anthropic-format request gets the same message with the type `invalid_request_error`.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| The model answers with `[REDACTED:…]` text | The key's mode is `mask`, and the prompt held data that Chowki masked. | Remove the data from the prompt, or set the key's mode to `alert` or `off` if it must be sent. |
| Requests fail with `sensitive_data_blocked` | The key's mode is `block`, and the prompt holds the types that the message names. | Remove that data, or change the key's mode. |
| A value isn't masked | Its format isn't one that Chowki matches, or it fails the check for its type, such as the Luhn check. | Use `block` mode for keys whose prompts must never hold such data, and report the format as an issue. |

## Related

- [Manage virtual keys](manage-virtual-keys.md) · [Configuration reference](../reference/configuration.md) ·
  [Architecture](../concepts/architecture.md)
