---
title: Route requests with aliases and fallback
description: Give a list of models one name, and let Chowki fall back to the next one when a provider fails.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

An alias is a model name, such as `fast`, that stands for a list of provider models. Clients ask
for the alias, and Chowki sends each request to the first model on the list. When that provider
is rate-limited, failing or too slow, Chowki tries the next one, so your apps keep working through
a provider's outage.

## Before you begin

- A Chowki folder set up with `chowki init`, with at least two providers in `chowki.yaml`. See
  [Send your first request](../get-started/quickstart.md).
- Restart `chowki serve` after you change `chowki.yaml`.

## Define an alias

1. Add an `aliases` section to `chowki.yaml`. Each alias lists its targets as
   `<provider>/<model>`, in the order to try them:

   ```yaml
   aliases:
     fast: ["openai/gpt-6-luna", "backup/gpt-6-luna"]
   ```

   Here `openai` and `backup` are the names of two providers under `providers`.

2. Restart the gateway, and send requests with the alias as the model:

   ```sh
   curl http://localhost:8080/v1/chat/completions \
     -H "Authorization: Bearer $CHOWKI_KEY" \
     -H "Content-Type: application/json" \
     -d '{"model": "fast", "messages": [{"role": "user", "content": "Say hello"}]}'
   ```

Through `/v1/chat/completions`, an alias can list models of any provider, because Chowki
translates requests for Anthropic and Gemini models. The other endpoints use only the targets of
their own API: `/anthropic/v1/messages` those of type `anthropic`, for example.

### Fall back to other models of one provider

A provider can be too busy for one model while others answer. Google, for example, answers with
status 503 and `"This model is currently experiencing high demand"` when a Gemini model has more
requests than it can take. An alias that lists other models of the same provider keeps your
requests working:

```yaml
aliases:
  flash: ["gemini/gemini-3.6-flash", "gemini/gemini-3.5-flash", "gemini/gemini-2.5-flash"]
```

Send requests with `"model": "flash"`. The answer's `model` field names the model that answered.

## How fallback works

- Chowki falls back to the next target only when the current one answers with HTTP status 429 or
  a 5xx status, can't be reached, or doesn't answer within `server.upstream_timeout`. Any other
  error, such as a 400 for an invalid request, goes straight to the client, because the next target
  would reject the request too.
- Chowki falls back only before any part of the answer has reached the client. A stream that fails
  after it started ends there.
- A request tries at most three targets: the first one and two fallbacks.
- A target that fails three times in a row is skipped for 30 seconds, so that requests don't wait
  for a provider that is down. After the pause, Chowki tries it again.
- Budgets, rate limits and redaction apply once per request, whichever target answers. The request
  is recorded with the target that answered.

## Verify

When the first target fails and the next one answers, the client gets the answer, and the gateway
logs the fallback. For example, when the first target answers with status 503:

```text
HTTP/1.1 200 OK
Content-Type: application/json
X-Chowki-Cache: bypass
X-Chowki-Request-Id: req_89e712a066aeedddff7ae93f
```

The gateway's log has a warning for the target that failed, and the request's line names the
target that answered and counts the fallbacks:

```json
{"time":"2026-09-27T15:03:57.925533973+05:00","level":"WARN","msg":"falling back to the next target","request_id":"req_89e712a066aeedddff7ae93f","target":"openai/gpt-6-luna","status":"503 Service Unavailable"}
{"time":"2026-09-27T15:03:57.927998406+05:00","level":"INFO","msg":"request","request_id":"req_89e712a066aeedddff7ae93f","family":"openai","status":200,"latency_ms":5,"key_id":1,"provider":"backup","model":"gpt-6-luna","stream":false,"ttfb_ms":5,"cache":"bypass","fallbacks":1,"input_tokens":1200,"output_tokens":300,"cache_read_tokens":0,"cache_write_tokens":0,"unpriced":"the model isn't in the catalog"}
```

The catalog prices a model per provider, so the model of the provider `backup` in this example
is unpriced.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `chowki serve` fails with `aliases.<name>[<n>]` | A target doesn't name a provider from `providers`, or has no model. | Write each target as `<provider>/<model>` with a configured provider. |
| Requests for an alias fail with `wrong_endpoint` | The alias has no target of the endpoint's API type. | Add a target of that type, or call the other endpoint. |
| The client gets the first target's error | The error is one that doesn't fall back, such as a 400. | Fix the request; see the error message. |
| `"code":"UNAVAILABLE"` with `This model is currently experiencing high demand` | Google has more requests for that Gemini model than it can take, for now. | Retry later, or name another model. To fall back to other models on their own, use an alias, as in [Fall back to other models of one provider](#fall-back-to-other-models-of-one-provider). |

## Related

- [Configuration reference](../reference/configuration.md) ·
  [Manage virtual keys](manage-virtual-keys.md) · [Architecture](../concepts/architecture.md)
