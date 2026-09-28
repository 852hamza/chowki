---
slug: introducing-chowki
title: Introducing Chowki, a self-hosted AI gateway
description: Chowki puts one checkpoint between your apps, agents and AI providers, to save tokens, keep secrets in and report honest costs.
tags: [release]
# Remove at the release, with the Blog link of the navbar; see docs/contributing/releasing.md.
draft: true
---

Most teams now send prompts to several AI providers, from apps, scripts and coding agents. The
provider keys end up in many places, secrets end up in prompts, and the bill arrives at the end of
the month with no way to tell which team, key or model it came from. Chowki is a checkpoint for
that traffic, which you run yourself.

<!-- truncate -->

## What Chowki does

Your apps and agents send their requests to Chowki instead of the provider, with a virtual key.
Chowki speaks the OpenAI, Anthropic and Gemini APIs, and any OpenAI-compatible one, so the official
SDKs and tools such as Claude Code, the Codex CLI and the Gemini CLI work unchanged:
`chowki setup` prints their settings.

On the way, Chowki:

- **saves tokens**, with an exact response cache, and by marking repeated prompt prefixes for
  Anthropic's prompt cache;
- **keeps secrets in**: the provider keys stay in the gateway, and redaction masks, blocks or
  reports secrets and personal data in prompts before they leave;
- **keeps spending in check**, with monthly budgets and rate limits for each key and project;
- **reports honest costs and savings**, from the providers' official prices, with the method of
  every saving, and marks a request as unpriced rather than guess its price.

It also finds secrets in repositories and MCP configurations, with `chowki scan`, locally and in
GitHub Actions.

## Small and fast

Chowki is one static binary with a SQLite database. The container image is about 18 MB, and runs
as a non-root user. In our load test, at 200 requests per second on two vCPUs, the gateway added
at most 0.5 ms to the median request and 2.5 ms to the 99th percentile. `make loadtest` measures
it on your hardware.

## Try it

[Send your first request](/docs/get-started/quickstart), or
[install Chowki](/docs/get-started/install) with Docker Compose. Then
[connect your coding agent](/docs/how-to/connect-claude-code), and watch its costs in the
dashboard.

Chowki is open source, under the Apache License 2.0. Bug reports, questions and pull requests are
welcome on GitHub.
