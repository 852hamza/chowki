---
title: Cache responses
description: Answer repeated requests from Chowki's exact cache, at once and at no cost.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Turn on Chowki's exact cache for a virtual key, and a request that is identical to an earlier one
gets the stored response: at once, without a call to the provider, and at no cost. It suits
requests that repeat, such as tests, evaluations and scripts that send the same prompts again.

## Before you begin

- A Chowki folder set up with `chowki init`. See
  [Send your first request](../get-started/quickstart.md).
- Run the commands in that folder, or pass `--config <FILE>` with the path of its `chowki.yaml`.

> **Important:** A cached response is the same answer every time, even when a request asks for
> varied answers, for example with a temperature above 0. Turn the cache on only where a repeated
> answer is fine.

## How the cache works

- Only non-streaming requests use the cache, and Chowki stores only successful responses.
- Two requests are identical when they go to the same provider and model with the same content.
  Formatting, the order of fields, and the fields `user`, `metadata`, `stream` and `stream_options`
  don't matter. The `anthropic-version` and `anthropic-beta` headers do.
- Keys in different projects never share responses.
- Chowki serves a stored response for an hour, and then it expires; set `defaults.cache_ttl` to
  change this. When the cache grows past `storage.cache_max_mb`, 256 MiB by default, Chowki deletes
  the least recently used responses. See the
  [configuration reference](../reference/configuration.md).
- Chowki encrypts stored responses with AES-256-GCM, with a key derived from its master key.
- A cached answer costs $0, and its original cost counts as savings. It doesn't count toward the
  key's token limit or budget, but it does count toward its limit of requests per minute.

## Turn on the cache for a key

1. Create a key with the cache on:

   ```sh
   chowki key create --name docs-bot --project team-a --cache exact
   ```

   Output:

   ```text
   Created virtual key "docs-bot" in project "team-a":

     chowki_EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE

   Copy it now. Chowki stores only a hash of it and can't show it again.

   Settings:
     Monthly budget:       none
     Requests per minute:  none
     Tokens per minute:    none
     Exact cache:          exact
   ```

2. To turn the cache on for an existing key, pass its prefix, as `chowki key list` shows it:

   ```sh
   chowki key update --cache exact chowki_9VRDs
   ```

   Output:

   ```text
   Updated the settings of virtual key chowki_9VRDs ("alice"):
     Monthly budget:       $100.00
     Requests per minute:  none
     Tokens per minute:    none
     Exact cache:          exact
   ```

`--cache off` turns the cache off for a key. `--cache default` makes the key follow
`defaults.cache` in `chowki.yaml`, which is `off` unless you change it.

## Turn the cache on or off for one request

Send the header `x-chowki-cache` with `on` or `off`. It wins over the key's setting. For example,
this request always goes to the provider:

```sh
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $CHOWKI_KEY" \
  -H "x-chowki-cache: off" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-6-sol", "messages": [{"role": "user", "content": "What is a checkpoint?"}]}'
```

## Verify

Send the same request twice with a key that has the cache on, and print only the response headers:

```sh
curl -sS -D - -o /dev/null http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $CHOWKI_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-6-sol", "messages": [{"role": "user", "content": "What is a checkpoint?"}]}'
```

The first response comes from the provider and is stored:

```text
HTTP/1.1 200 OK
Content-Type: application/json
X-Chowki-Cache: miss
X-Chowki-Cost-Usd: 1.00000000
X-Chowki-Request-Id: req_fe86a333ef13bfdf15ea22a4
```

The second comes from the cache, and costs nothing:

```text
HTTP/1.1 200 OK
Content-Type: application/json
X-Chowki-Cache: hit
X-Chowki-Cost-Usd: 0.00000000
X-Chowki-Request-Id: req_2702287d578c019cef99a51f
```

Your costs differ. `X-Chowki-Cache` is `bypass` for a request that doesn't use the cache: a
streaming request, or one from a key with the cache off. In the gateway's log, a request that the
cache answered has `"cache":"hit"`.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `X-Chowki-Cache: bypass` | The request streams, the key's cache is off, or `storage.cache_max_mb` is `0`. | Send the request without `"stream": true`, or turn the cache on with `chowki key update --cache exact`. |
| A repeated request is a `miss` | Its content, model or `anthropic-*` headers differ, or the stored response expired or was evicted. | Compare the two requests; raise `defaults.cache_ttl` or `storage.cache_max_mb`. |
| Requests fail with `The x-chowki-cache header must be "on" or "off".` | The header has another value. | Send `on` or `off`, or leave the header out. |
| The log warns that a cache entry doesn't open | The master key changed since the response was stored. | Nothing to do: the next response to the request replaces the entry. |

## Related

- [Manage virtual keys](manage-virtual-keys.md) ·
  [Configuration reference](../reference/configuration.md) · [Architecture](../concepts/architecture.md)
