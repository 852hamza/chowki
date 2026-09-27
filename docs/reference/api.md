---
title: API reference
description: The endpoints that Chowki serves, its headers, and every error code it returns.
type: reference
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Chowki speaks the APIs of the providers: clients keep their SDK and change only the base URL and
the key. This page lists what Chowki adds: its endpoints, its headers and its own errors.

<!-- Written by hand for now; a test checks that it lists every error code. -->

## Base URLs

| API format | Base URL |
|---|---|
| OpenAI, such as the OpenAI SDKs | `http://<HOST>:8080/v1` |
| Anthropic, such as the Anthropic SDKs and Claude Code | `http://<HOST>:8080/anthropic` |
| Gemini, such as the Google Gen AI SDKs | `http://<HOST>:8080/gemini` |

Replace `<HOST>` with the gateway's host, and `8080` with the port of `server.listen`.

## Authentication

Send a virtual key in the header that the client's SDK uses: `Authorization: Bearer <KEY>`,
`x-api-key: <KEY>` or `x-goog-api-key: <KEY>`. Chowki never reads a key from the URL. See
[Manage virtual keys](../how-to/manage-virtual-keys.md).

## Endpoints

| Endpoint | What it does |
|---|---|
| `POST /v1/chat/completions` | OpenAI chat completions, streaming or not. |
| `POST /v1/embeddings` | OpenAI embeddings. Redaction reads the `input` text; the exact cache applies. |
| `GET /v1/models` | The aliases and catalog models that the key may use through the OpenAI-format endpoints. Models that the catalog doesn't list, such as local ones, aren't in it but still work. |
| `POST /anthropic/v1/messages` | Anthropic messages, streaming or not. |
| `POST /anthropic/v1/messages/count_tokens` | Anthropic token counting. Providers don't bill it, so it spends no budget or tokens per minute, and the exact cache doesn't apply. Redaction does. |
| `POST /gemini/v1beta/models/<MODEL>:generateContent` | Gemini content generation. |
| `POST /gemini/v1beta/models/<MODEL>:streamGenerateContent?alt=sse` | Gemini streaming, as server-sent events. Without `alt=sse`, Chowki answers 400. |
| `POST /gemini/v1beta/models/<MODEL>:countTokens` | Gemini token counting: free, like Anthropic's. |
| `POST /gemini/v1beta/models/<MODEL>:embedContent`, `…:batchEmbedContents` | Gemini embeddings. |
| `GET /healthz`, `GET /readyz`, `GET /metrics` | Health checks and metrics, without a key. See the [metrics reference](metrics.md). |
| `/admin/v1/…` | The admin API, with an admin token instead of a virtual key. See the [admin API reference](admin-api.md). |

In the Gemini paths, replace `<MODEL>` with a model, such as `gemini-2.5-flash`, with
`<provider>/<model>`, or with an alias. See [Use Google Gemini](../how-to/use-google-gemini.md).

For example, `GET /v1/models` answers:

```json
{"object":"list","data":[{"id":"fast","object":"model","created":0,"owned_by":"chowki"},{"id":"openai/gpt-6-astra","object":"model","created":0,"owned_by":"openai"},{"id":"openai/gpt-6-luna","object":"model","created":0,"owned_by":"openai"},{"id":"openai/gpt-6-sol","object":"model","created":0,"owned_by":"openai"}]}
```

An embeddings request works as with the provider:

```sh
curl http://localhost:8080/v1/embeddings \
  -H "Authorization: Bearer $CHOWKI_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "openai/<EMBEDDING_MODEL>", "input": "The gateway checks every request."}'
```

Replace `<EMBEDDING_MODEL>` with an embedding model of your provider. The answer is the
provider's, unchanged; from a local stand-in provider:

```json
{"object": "list", "data": [{"object": "embedding", "index": 0, "embedding": [0.0023, -0.0091, 0.0154]}], "model": "<EMBEDDING_MODEL>", "usage": {"prompt_tokens": 12, "total_tokens": 12}}
```

## Request headers

| Header | Values | Effect |
|---|---|---|
| `x-chowki-cache` | `on`, `off` | Uses or skips the exact cache for this request, whatever the key's setting. See [Cache responses](../how-to/cache-responses.md). |

## Response headers

| Header | When | Value |
|---|---|---|
| `x-chowki-request-id` | Always | The request's ID, as the log and records show it. |
| `x-chowki-cost-usd` | Priced non-streaming requests | The request's cost in US dollars. |
| `x-chowki-cache` | Chat and embeddings requests | `hit`, `miss` or `bypass`. |
| `x-chowki-redactions` | When redaction finds something | The number of findings. |
| `Retry-After`, `retry-after-ms` | Over a rate limit | When to retry, in seconds and in milliseconds. |
| `x-should-retry` | Over a budget | `false`: retrying doesn't help. |

## Errors

Chowki answers its own errors in the error format of the API that the client speaks, so that SDKs
show them. In the OpenAI format, `code` holds the codes below:

```json
{"error":{"code":"budget_exceeded","message":"…","param":null,"type":"rate_limit_error"}}
```

In the Anthropic format, `error.type` is one of Anthropic's types for the status, such as
`rate_limit_error` for 429, and the message says what happened:

```json
{"error":{"message":"…","type":"rate_limit_error"},"request_id":"req_…","type":"error"}
```

In the Gemini format, Google's error model, `status` is the status name for the HTTP status, and
the `reason` of the `ErrorInfo` detail is the code below in capitals:

```json
{"error":{"code":401,"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","domain":"chowki","reason":"MISSING_API_KEY"}],"message":"Send your Chowki virtual key in the Authorization: Bearer, x-api-key or x-goog-api-key header.","status":"UNAUTHENTICATED"}}
```

| Code | Status | Meaning |
|---|---|---|
| `missing_api_key` | 401 | The request has no virtual key. |
| `invalid_api_key` | 401 | The virtual key is malformed, unknown or wrong. |
| `revoked_api_key` | 401 | The virtual key was revoked. |
| `invalid_request` | 400 | The body isn't valid JSON, or a field that Chowki reads is invalid. |
| `request_too_large` | 413 | The body is larger than `server.max_body_mb`. |
| `model_not_allowed` | 403 | The key may not use the model. |
| `unknown_provider` | 400 | Chowki can't tell which provider serves the model. |
| `wrong_endpoint` | 400 | The model's provider speaks another API than the endpoint's, and the endpoint doesn't translate. |
| `unsupported_option` | 400 | The request is translated for a provider of another API, which can't honor an option; `param` names it. |
| `sensitive_data_blocked` | 400 | Redaction in `block` mode found secrets or personal data. |
| `rate_limit_exceeded` | 429 | The key reached its limit of requests or tokens per minute. |
| `budget_exceeded` | 429 | The key's or its project's monthly budget is used up. |
| `provider_key_missing` | 500 | The provider's key isn't set in the gateway's environment. |
| `upstream_unavailable` | 502 | The provider can't be reached. |
| `upstream_timeout` | 504 | The provider didn't answer within `server.upstream_timeout`. |
| `internal_error` | 500 | Chowki failed; its log has the details. |
| `not_found` | 404 | The path isn't an endpoint of the gateway. |

Errors from providers, such as a 400 for an invalid parameter, reach the client unchanged.

## Related

- [Configuration reference](configuration.md) · [Metrics reference](metrics.md) ·
  [Architecture](../concepts/architecture.md)
