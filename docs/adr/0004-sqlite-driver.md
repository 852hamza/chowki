---
title: "ADR 0004: modernc.org/sqlite as the SQLite driver"
description: Chowki uses modernc.org/sqlite, a pure-Go SQLite driver, so that it builds without cgo.
type: record
last_reviewed: 2026-09-27
---

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

[ADR 0002](0002-go-single-binary-sqlite.md) chose SQLite as the default database and a build without
cgo, which keeps cross-compiling to every platform simple. The Go standard library has
`database/sql` but no SQLite driver, so Chowki needs one that is written in Go.

The options:

- `modernc.org/sqlite`: SQLite's C source translated to Go. Latest release: v1.59.0, September 2026.
  BSD-3-Clause.
- `github.com/ncruces/go-sqlite3`: SQLite compiled to WebAssembly and run by the wazero runtime. Its
  version is still below 1.0, so its API may change.
- `github.com/mattn/go-sqlite3`: the most used driver, but it needs cgo.

## Decision

Chowki uses `modernc.org/sqlite`, through `database/sql`.

- It needs no cgo and runs on every platform that Chowki ships for.
- It's actively maintained and has a stable API.
- It and the modules it links into the binary have permissive licenses: BSD-3-Clause
  (`modernc.org/libc`, `modernc.org/mathutil`, `modernc.org/memory`, `github.com/google/uuid`,
  `github.com/remyoudompheng/bigfft`, `golang.org/x/sys`) and MIT (`github.com/dustin/go-humanize`).
  SQLite itself is in the public domain.
- Chowki opens the database in WAL mode with a busy timeout, so readers never block the one
  writer, and it writes request metadata in batches from a background goroutine.

## Consequences

- The driver adds about ten modules to the build and several megabytes to the binary.
- A translated driver can be slower than the C library. Chowki writes only metadata, in batches,
  so this is unlikely to matter; the overhead benchmark would show it.
- Binary releases must include the license texts of these modules. The release process will
  generate that file.
