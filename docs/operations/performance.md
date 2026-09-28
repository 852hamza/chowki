---
title: Measure the gateway's overhead
description: Run the load test that measures the time Chowki adds to requests, and read its results.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

Chowki's target is to add less than 5 ms to the median request and less than 25 ms to the 99th
percentile, at 200 requests per second on 2 vCPUs, not counting the provider's own time. The load
test in `tools/loadtest` measures it on your hardware.

## Before you begin

- Go 1.27 or later, GNU Make and a clone of the repository.
- Linux, for `taskset`, which pins the gateway to 2 CPUs. Elsewhere, pass `--cpus ''` to use every
  CPU.

## Run the load test

1. In the repository, run:

   ```sh
   make loadtest
   ```

   The test builds Chowki, starts it with a fake provider that answers after 20 ms, and sends chat
   requests at 200 per second for a minute, half of them streamed, each with a prompt of about
   1,400 characters that redaction scans. It pins the gateway to CPUs 0 and 4, which on most
   machines are the two hyperthreads of one core, as 2 vCPUs are on most clouds.

   Output, on an Intel Core i7-6820HQ laptop:

   ```text
   Requests:  12000 in 1m0.018s (199.9 per second), 0 failed
   Overhead seen by the client (latency minus the provider's 20ms):
     p50 2.25 ms   p90 4.38 ms   p99 13.23 ms   max 39.10 ms
   Overhead that the gateway measured (chowki_overhead_seconds, bucket bounds):
     p50 ≤ 0.50 ms   p99 ≤ 2.50 ms

   Passed: the p99 overhead is under 25.00 ms.
   ```

2. Read the two measurements:

   - **Seen by the client** is each request's latency minus the provider's 20 ms. It includes the
     load generator and the fake provider, which share the machine, so it's higher than the
     gateway's own time and varies from run to run.
   - **Measured by the gateway** comes from the `chowki_overhead_seconds` histogram, which counts
     the time of each request minus the time of its provider call. Its percentiles are bucket
     bounds, such as `≤ 2.50 ms`.

The test fails when a request fails, or when the client's p99 overhead is over 25 ms. Pin the test
itself to other CPUs, such as with `taskset -c 1-3,5-7 make loadtest`, so that it doesn't compete
with the gateway.

## Change the load

Run the tool directly to change the rate, the duration, the provider's delay or the CPUs:

```sh
go run ./tools/loadtest --rps 500 --duration 2m --delay 50ms --cpus 0-3
```

To look for data races under load, test a gateway built with the race detector, at a lower rate,
since the detector makes it many times slower. The test fails if the gateway reports a race:

```sh
go build -race -o bin/chowki-race ./cmd/chowki
go run ./tools/loadtest --binary bin/chowki-race --rps 20 --max-p99 1s
```

## Related

- [Monitor Chowki](monitoring.md) · [Metrics reference](../reference/metrics.md)
