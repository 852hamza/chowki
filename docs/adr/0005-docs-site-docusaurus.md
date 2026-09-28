---
title: "ADR 0005: Docusaurus for the documentation site"
description: The documentation site is built with Docusaurus from docs/, with a local search index, and published to GitHub Pages.
type: record
last_reviewed: 2026-09-28
---

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

The developer guide in `docs/` is Markdown that reads well on GitHub. It also needs a website:
navigation, search that works without a service, versions for each minor release, and a check of
every link. The site's tools are not part of the gateway: nothing of them reaches the binary or the
container image.

## Decision

`website/` builds the site with Docusaurus 3 (MIT), from `docs/` as it is, with
`docusaurus-lunr-search` (MIT) for a search index that the site serves itself. The pages stay plain
Markdown (`markdown.format: detect`), so that placeholders such as `<VIRTUAL_KEY>` stay text. The
site reads the project's identity from `project.env`. The build fails on a broken link or anchor,
and the Docs workflow builds it on every change and publishes it to GitHub Pages.

## Alternatives

- **Hugo, MkDocs:** mature too, but Docusaurus has versioned docs built in, which the guide needs
  for each minor release.
- **@easyops-cn/docusaurus-search-local:** now needs a peer package for an AI chat widget, which
  the site doesn't want.
- **A hosted search service:** needs an account and sends readers' searches elsewhere.

## Consequences

- The site's dependencies are pinned in `website/package-lock.json`, and only CI and writers
  install them.
- GitHub Pages must be set to deploy from GitHub Actions in the repository's settings, once.
