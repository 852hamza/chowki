---
title: "ADR 0001: Record architecture decisions"
description: Chowki records each significant architecture decision as a short, numbered document in docs/adr.
type: record
last_reviewed: 2026-09-27
---

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

Chowki is built in the open, and contributors join over time. They need to know why the code works
the way it does, which options were considered and what was traded away, without reading every
past discussion.

## Decision

We record every significant architecture decision as an architecture decision record (ADR), in the
format that Michael Nygard describes in
[Documenting Architecture Decisions](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).

- An ADR is a Markdown file in `docs/adr/`, named `NNNN-title-with-dashes.md`, where `NNNN` is the
  next four-digit number.
- It has a status and the sections Context, Decision and Consequences. The status is Proposed,
  Accepted, Deprecated, or Superseded by another ADR.
- Every new dependency gets an ADR that names its license and explains why the Go standard library
  isn't enough.
- An ADR merges in the same pull request as the change it describes.
- We don't rewrite an accepted ADR. To change a decision, we write a new ADR and mark the old one as
  superseded.

## Consequences

- The reasons behind the design live next to the code and are reviewed like code.
- Each significant change takes a little longer, because someone writes its ADR.
