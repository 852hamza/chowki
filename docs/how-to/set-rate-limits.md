---
title: Set rate limits
description: Limit how many requests and tokens a virtual key can send each minute.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Limit how many requests and tokens a virtual key can send each minute, so that one runaway script
or agent can't use up your provider's rate limits or crowd out your other keys.

## Before you begin

- A Chowki folder set up with `chowki init`. See
  [Send your first request](../get-started/quickstart.md).
- Run the commands in that folder, or pass `--config <FILE>` with the path of its `chowki.yaml`.
- The commands change the database directly. The gateway can keep running: new limits apply to the
  next request.

## How the limits work

- `--rpm` limits requests per minute. `--tpm` limits tokens per minute: input and output tokens
  together, as the provider reports them.
- Each limit refills evenly over a minute, and a key can use a whole minute's worth at once. For
  example, a key with `--rpm 60` can send 60 requests in a burst, and then one more each second.
- Chowki checks the request limit before it reads a request. For the token limit, it estimates a
  request's input tokens from its size, about 4 bytes per token, and holds them until the response
  ends. Then it counts the tokens that the provider reported. A request that fails gives its tokens
  back, and an answer from the [exact cache](cache-responses.md) uses none.
- A request that needs more tokens than the limit goes through once the key has a full minute's
  worth. The key then waits until the extra tokens have refilled.
- Chowki keeps rate limits in memory: they start over when Chowki restarts.

## Set limits for a key

1. Create a key with limits:

   ```sh
   chowki key create --name ci-bot --project team-a --rpm 60 --tpm 100000
   ```

   Output:

   ```text
   Created virtual key "ci-bot" in project "team-a":

     chowki_EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE

   Copy it now. Chowki stores only a hash of it and can't show it again.

   Settings:
     Monthly budget:       none
     Requests per minute:  60
     Tokens per minute:    100000
     Exact cache:          default
     Redaction:            default
     Models:               all
   ```

2. To change the limits of an existing key, pass its prefix, as `chowki key list` shows it:

   ```sh
   chowki key update --rpm 120 chowki_VFK1E
   ```

   Output:

   ```text
   Updated the settings of virtual key chowki_VFK1E ("ci-bot"):
     Monthly budget:       none
     Requests per minute:  120
     Tokens per minute:    100000
     Exact cache:          default
     Redaction:            default
     Models:               all
   ```

To remove a limit, set it to `0`. You can set rate limits and a budget together; see
[Set monthly budgets](set-budgets.md). Chowki records every change in its audit log.

## Verify

`chowki key list` shows each key's limits in the `RPM` and `TPM` columns:

```text
PREFIX        NAME      PROJECT  SPENT 2026-09  BUDGET  RPM   TPM     CACHE    REDACTION  CREATED               STATUS
chowki_nklNv  alice     team-a   $3.00          $50.00  none  none    default  default    2026-09-27 10:03 UTC  active
chowki_VFK1E  ci-bot    team-a   $1.00          none    60    100000  default  default    2026-09-27 10:03 UTC  active
chowki_y2pj7  docs-bot  team-a   $0.00          none    none  none    exact    default    2026-09-27 10:03 UTC  active
```

When a key reaches a limit, requests fail with HTTP status 429 in the error format of the API that
the client speaks. Two headers say when to retry: `Retry-After`, in seconds, and `retry-after-ms`,
in milliseconds. The official OpenAI and Anthropic SDKs wait that long and retry on their own.

For example, the third request in a minute from a key with `--rpm 2` gets:

```text
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 30
Retry-After-Ms: 29908
X-Chowki-Request-Id: req_8d8092f9cae7ceff07936b4f

{"error":{"code":"rate_limit_exceeded","message":"This key has reached its limit of 2 requests per minute. Try again in 30 seconds.","param":null,"type":"rate_limit_error"}}
```

In the Anthropic format, the same error is:

```json
{"error":{"message":"This key has reached its limit of 2 requests per minute. Try again in 30 seconds.","type":"rate_limit_error"},"request_id":"req_e4409d514f6b6c0d1f61b507","type":"error"}
```

A key over its token limit gets a message such as `This key has reached its limit of 100000 tokens
per minute; this request needs about 17 tokens. Try again in 1m47s.`

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Requests fail with `rate_limit_exceeded` | The key sends more requests or tokens per minute than its limits allow. | Wait for the time in `Retry-After`, or raise the limit with `chowki key update`. |
| A key waits minutes after one large request | The request used more tokens than the key's limit per minute, which refills evenly. | Set `--tpm` above the tokens of the key's largest requests. |
| Limits start over after a restart | Chowki keeps rate limits in memory. | This is expected. Budgets, which count money, are saved in the database. |

## Related

- [Manage virtual keys](manage-virtual-keys.md) · [Set monthly budgets](set-budgets.md) ·
  [Architecture](../concepts/architecture.md)
