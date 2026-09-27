---
title: "ADR 0002: Single Go binary with SQLite by default"
description: Chowki ships as one statically linked Go binary that stores its data in SQLite by default.
type: record
last_reviewed: 2026-09-27
---

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

Chowki sits in the path of every LLM request, so it must add little latency and relay many
concurrent streams. Our target for the gateway's own overhead, without the provider's time, is
under 5 ms at the median and under 25 ms at the 99th percentile.

It must also be easy to self-host. A developer should be able to run it on a laptop in minutes, and
a team on one small server, without installing a separate database server first.

## Decision

- Chowki is written in Go, using the latest stable release, and ships as one statically linked
  binary per platform. The dashboard's templates and assets are embedded in the binary.
- Chowki uses the Go standard library wherever it can: `net/http` for the server and upstream
  calls, `log/slog` for logs, and the `crypto` packages for keys and encryption.
- Chowki stores its data in SQLite by default, in one file. Storage sits behind an interface in
  `internal/store`, so that a PostgreSQL store can be added later for high availability.
- Chowki builds without cgo, which keeps cross-compiling simple. The SQLite driver must therefore be
  pure Go. The candidate is `modernc.org/sqlite`, and it gets its own ADR when it's added.

## Consequences

- Installing Chowki means downloading one file or running one container image, with no runtime
  dependencies.
- Go's HTTP stack and goroutines handle streaming and many concurrent connections with little code.
- SQLite allows one writer, so a deployment runs as one node until the PostgreSQL store exists.
- A pure-Go SQLite driver can be slower than the C library. Chowki writes only request metadata, so
  we expect it to stay within the overhead target, and we measure it against that target.
