---
title: Chowki
description: A self-hosted AI gateway that saves tokens, blocks secrets and reports honest costs.
hide_table_of_contents: true
---

# Chowki

Chowki is an open-source AI gateway that you run yourself. Your apps, developers and coding agents
send their LLM requests through it, to OpenAI, Anthropic, Google Gemini or any OpenAI-compatible
provider, and Chowki:

- **saves tokens**, with an exact response cache and provider prompt caching;
- **keeps secrets in**: virtual keys instead of provider keys, and redaction of secrets and
  personal data in prompts;
- **keeps spending in check**, with budgets and rate limits per key and project;
- **reports honest costs and savings**, from official prices, and never guesses a price.

It's a single binary with a SQLite database, and runs on a laptop, a server or in a container.

## Start here

- [Send your first request](/docs/get-started/quickstart), in a few minutes.
- [Install Chowki](/docs/get-started/install) with Docker Compose, Docker or from source.
- [Connect Claude Code](/docs/how-to/connect-claude-code), [the Codex CLI](/docs/how-to/connect-codex),
  [the Gemini CLI](/docs/how-to/connect-gemini-cli) or [your SDKs](/docs/how-to/connect-sdks).
