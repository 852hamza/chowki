---
title: Metrics and health checks reference
description: The health check endpoints and every metric that Chowki exports in the Prometheus text format.
type: reference
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Chowki serves health checks for load balancers and orchestrators, and its own metrics for
Prometheus, on the same address as the API. Metrics count since the gateway started; they start
again from zero after a restart.

<!-- Written by hand for now; a test checks that it lists every metric. -->

## Endpoints

| Endpoint | Answers |
|---|---|
| `GET /healthz` | `200` and `{"status":"ok"}` while the process runs. Use it for liveness probes. |
| `GET /readyz` | `200` and `{"status":"ready"}` when the database answers, or `503` and `{"status":"not ready","reason":"…"}` when it doesn't. Use it for readiness probes and load balancers. |
| `GET /metrics` | The metrics below, in the Prometheus text exposition format, version 0.0.4. |

> **Warning:** These endpoints need no key. The metrics name your providers and models and show
> your costs, so don't expose the gateway's port to the internet without a reverse proxy that
> blocks `/metrics`.

## `chowki_requests_total`

| | |
|---|---|
| Type | Counter |
| Labels | `family` (`openai` or `anthropic`), `provider`, `model`, `status` (the HTTP status sent to the client), `cache` (`hit`, `miss`, `bypass`, or empty for a request rejected earlier) |
| Since | v0.1 |

Requests that the gateway finished. A request rejected before it has a provider, for example for
an invalid key, has an empty `provider` and `model`. After 2,000 label sets, new ones count under
the `provider` and `model` `other`, so that clients can't grow the metrics without bound.

## `chowki_tokens_total`

| | |
|---|---|
| Type | Counter |
| Labels | `type`: `input`, `output`, `cache_read`, `cache_write`, `reasoning` |
| Since | v0.1 |

Tokens that providers reported. `input` counts every prompt token, including those read from or
written to a provider's prompt cache; `output` includes `reasoning`.

## `chowki_cost_usd_total`

| | |
|---|---|
| Type | Counter |
| Labels | None |
| Since | v0.1 |

Cost of the priced requests, in US dollars. Requests to models without a price in the catalog add
nothing.

## `chowki_savings_usd`

| | |
|---|---|
| Type | Gauge |
| Labels | `method`: `prompt_cache` or `exact_cache` |
| Since | v0.1 |

Net savings, in US dollars. It's a gauge because savings from prompt caching can be negative when
a request pays to write the cache and no later request reads it.

## `chowki_redactions_total`

| | |
|---|---|
| Type | Counter |
| Labels | `type`: the type of data found, such as `email` or `aws_access_key` |
| Since | v0.1 |

Secrets and personal data that redaction found in requests, in every mode but `off`.

## `chowki_upstream_latency_seconds`

| | |
|---|---|
| Type | Histogram |
| Buckets | 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300 and 600 seconds |
| Since | v0.1 |

Time that providers took to answer, from the upstream call to the end of the response, streams
included. Requests that didn't reach a provider, such as cache hits, aren't counted.

## `chowki_overhead_seconds`

| | |
|---|---|
| Type | Histogram |
| Buckets | 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1 and 0.25 seconds |
| Since | v0.1 |

Time that the gateway itself added to a request: its whole time minus the provider's.

## Related

- [Monitor Chowki](../operations/monitoring.md) · [Architecture](../concepts/architecture.md)
