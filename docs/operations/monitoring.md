---
title: Monitor Chowki
description: Check that Chowki is up and ready, and scrape its metrics with Prometheus.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Point your health checks at Chowki's `/healthz` and `/readyz` endpoints, and your Prometheus at
`/metrics`, to see traffic, tokens, cost, savings and the gateway's own overhead.

## Before you begin

- A running gateway: `chowki serve`. See [Send your first request](../get-started/quickstart.md).
- The examples use `http://localhost:8080`, the default address.

## Check health

1. Check that the process runs:

   ```sh
   curl -sS http://localhost:8080/healthz
   ```

   Output:

   ```text
   {"status":"ok"}
   ```

2. Check that the gateway can serve requests. It answers `503` when its database doesn't answer:

   ```sh
   curl -sS http://localhost:8080/readyz
   ```

   Output:

   ```text
   {"status":"ready"}
   ```

In Kubernetes, use `/healthz` for the liveness probe and `/readyz` for the readiness probe.

## Scrape the metrics

1. Look at the metrics:

   ```sh
   curl -sS http://localhost:8080/metrics
   ```

   The start of the output, after one request:

   ```text
   # HELP chowki_requests_total Requests that the gateway finished, by API family, provider, model, HTTP status and exact cache status.
   # TYPE chowki_requests_total counter
   chowki_requests_total{family="openai",provider="openai",model="gpt-6-sol",status="200",cache="bypass"} 1
   # HELP chowki_tokens_total Tokens that providers reported, by type. input counts every prompt token, cached or not.
   # TYPE chowki_tokens_total counter
   chowki_tokens_total{type="input"} 1200
   chowki_tokens_total{type="output"} 300
   # HELP chowki_cost_usd_total Cost of priced requests, in US dollars.
   # TYPE chowki_cost_usd_total counter
   chowki_cost_usd_total 0.0054
   ```

2. Add a scrape job for Chowki to your Prometheus configuration:

   ```yaml
   scrape_configs:
     - job_name: chowki
       static_configs:
         - targets: ["localhost:8080"]
   ```

   Replace `localhost:8080` with the address of your gateway.

> **Warning:** `/metrics`, `/healthz` and `/readyz` need no key. Don't expose the gateway's port to
> the internet without a reverse proxy that blocks `/metrics`.

## Verify

After Prometheus scrapes Chowki, the metric `up{job="chowki"}` is `1`, and
`chowki_requests_total` grows as clients send requests. See the
[metrics reference](../reference/metrics.md) for every metric and label.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `/readyz` answers 503 | The database file is missing, not readable, or on a full disk. | Check the gateway's log and the path in `storage.dsn`. |
| The metrics start from zero | The gateway restarted; metrics count since it started. | Use Prometheus functions such as `increase` and `rate`, which handle restarts. |

## Related

- [Metrics and health checks reference](../reference/metrics.md) ·
  [Configuration reference](../reference/configuration.md)
