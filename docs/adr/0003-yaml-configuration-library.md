---
title: "ADR 0003: go.yaml.in/yaml/v3 for the configuration file"
description: Chowki reads chowki.yaml with go.yaml.in/yaml/v3, the YAML library maintained by the YAML organization.
type: record
last_reviewed: 2026-09-27
---

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

Chowki's configuration file, `chowki.yaml`, is YAML because people edit it by hand: YAML allows
comments and reads well for nested settings such as the provider list. The Go standard library has
no YAML parser, and a correct YAML parser is too large to write and maintain ourselves.

The options:

- `gopkg.in/yaml.v3`, the most used Go YAML library. Its repository was archived in 2025, and its
  last release is from 2022.
- `go.yaml.in/yaml/v3`, the continuation of `gopkg.in/yaml.v3` by the YAML organization, with the
  same API. Latest release: v3.0.5, July 2026.
- `go.yaml.in/yaml/v4`, the next major version, still a release candidate.
- `github.com/goccy/go-yaml`, an independent implementation. Latest release: v1.19.2, January 2026.

## Decision

Chowki uses `go.yaml.in/yaml/v3`.

- It's maintained, and its API is the one that most Go developers know.
- It has no dependencies of its own.
- Its license is permissive: MIT for the files ported from libyaml, Apache-2.0 for the rest. Its
  notice is part of Chowki's [NOTICE](https://github.com/852hamza/chowki/blob/main/NOTICE) file.
- The configuration loader decodes strictly: an unknown setting, such as a misspelled name or a
  provider key written into the file, is an error.

## Consequences

- Chowki depends on one more module, and we watch it with govulncheck like the rest of the build.
- We can move to v4 once it's released; the configuration loader is the only code that uses the
  library.
