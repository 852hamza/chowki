---
title: Use Anthropic prompt caching
description: Let Chowki mark repeated Anthropic prompt prefixes for the provider's cache, and see what it saves.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Anthropic bills a prompt prefix that it has cached at a tenth of the input price, but only when
the request marks the prefix with `cache_control`. Chowki adds that mark for you when the same
tools and system prompt repeat, so apps that don't manage caching themselves pay less.

## Before you begin

- A Chowki folder set up with `chowki init`, and a provider of type `anthropic` in `chowki.yaml`.
  See [Send your first request](../get-started/quickstart.md).
- The optimizer is on by default. It changes only requests to the Anthropic-format endpoint,
  `/anthropic/v1/messages`.

## How Chowki marks a prefix

- The prefix is a request's tools and system prompt. Chowki remembers each prefix, per model, for
  five minutes: the lifetime of Anthropic's default cache.
- When a prefix comes again within five minutes, and it's at least as long as the model's minimum
  for caching, Chowki adds one `"cache_control": {"type": "ephemeral"}` to the end of the system
  prompt, or to the last tool when there's no system prompt. A system prompt sent as a string
  becomes a single text block, which Anthropic reads the same way.
- Chowki never changes a request that already has `cache_control` anywhere, such as the requests
  of Claude Code, which manages its own caching. It never reorders content, and it adds one mark
  at most, within Anthropic's limit of four.
- The minimum length depends on the model: for example, 512 tokens for Claude Opus 5.5, 1,024 for
  Claude Sonnet 5 and 4,096 for Claude Haiku 4.5. Chowki doesn't mark prefixes of models whose
  minimum isn't in its catalog.

> **Note:** Anthropic bills the first cached request at a higher rate for the tokens it writes to
> the cache: for Claude Sonnet 5, $2.50 instead of $2 per million. Requests that read the cache
> within five minutes pay $0.20. Chowki waits for a prefix to repeat before it marks it, so that a
> one-off prompt doesn't pay the higher rate.

## Turn the optimizer off

To forward every request exactly as the client sent it, set `defaults.prompt_cache` to `off` in
`chowki.yaml`:

```yaml
defaults:
  prompt_cache: "off"
```

Or set the environment variable `CHOWKI_DEFAULTS_PROMPT_CACHE=off`, then restart `chowki serve`.

## Verify

When Chowki marks a request, its line in the gateway's log has `"prompt_cache_breakpoint":true`.
For example, the second of two requests with the same long system prompt:

```json
{"time":"2026-09-27T11:48:51.662531117+05:00","level":"INFO","msg":"request","request_id":"req_37e67294e14746048eb9233a","family":"anthropic","status":200,"latency_ms":1,"key_id":1,"provider":"anthropic","model":"claude-sonnet-5","stream":false,"ttfb_ms":1,"cache":"bypass","prompt_cache_breakpoint":true,"input_tokens":5000,"output_tokens":200,"cache_read_tokens":0,"cache_write_tokens":0,"cost_usd":0.012}
```

The `cache_write_tokens` and `cache_read_tokens` fields show what Anthropic reported that it wrote
to and read from its cache. This example comes from a local stand-in provider, so both are 0;
Anthropic reports the tokens it wrote for the first marked request, and the tokens it read for the
ones after it. Chowki records the net savings of each request with the method `prompt_cache`: the
reads saved, minus the extra cost of the writes. The net savings of a request can be negative,
and Chowki records them that way.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| No request has `prompt_cache_breakpoint` | The requests have `cache_control` already, their prefix is shorter than the model's minimum, or the same prefix doesn't come twice within five minutes. | Check the requests' tools and system prompt. A client that marks prefixes itself doesn't need the optimizer. |
| `cache_write_tokens` grows but `cache_read_tokens` stays 0 | The prefix changes between requests, for example because the system prompt holds a date or a request ID. | Move the parts that change out of the system prompt, into the messages. |

## Related

- [Cache responses](cache-responses.md) · [Configuration reference](../reference/configuration.md) ·
  [Architecture](../concepts/architecture.md)
