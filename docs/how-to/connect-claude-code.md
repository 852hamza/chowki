---
title: Connect Claude Code
description: Point Claude Code at Chowki, so that its requests use a virtual key, budgets and redaction.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

Route Claude Code through Chowki: it then signs in with a virtual key instead of your Anthropic
account, and Chowki applies the key's budget, rate limits and redaction, and records every
request.

## Before you begin

- Chowki running, with an `anthropic` provider and its key. See
  [Send your first request](../get-started/quickstart.md).
- Claude Code installed.
- With the settings, Claude Code sends its requests with the virtual key instead of a claude.ai
  login, and Chowki bills them to your Anthropic API key.

## Connect Claude Code

1. Print the settings:

   ```sh
   chowki setup claude-code
   ```

   Output:

   ```text
   Claude Code reaches the gateway at http://localhost:8080/anthropic.

   Create a virtual key for it, if you haven't:

     chowki key create --name claude-code

   Add the settings to ~/.claude/settings.json, which applies to all your
   projects. Don't put them in a project's .claude/settings.json, which the
   project shares with everyone who clones it.

   {
     "env": {
       "ANTHROPIC_BASE_URL": "http://localhost:8080/anthropic",
       "ANTHROPIC_AUTH_TOKEN": "<VIRTUAL_KEY>"
     }
   }

   Or set them in your shell:

     export ANTHROPIC_BASE_URL=http://localhost:8080/anthropic
     export ANTHROPIC_AUTH_TOKEN=<VIRTUAL_KEY>

   Claude Code sends ANTHROPIC_AUTH_TOKEN as a bearer token. Start claude and
   run /status: it shows the base URL and the auth token.
   ```

2. Create the virtual key, and put it in place of `<VIRTUAL_KEY>` in `~/.claude/settings.json`.
   With `--key <VIRTUAL_KEY>`, `chowki setup` prints the settings with the key filled in.

3. Start `claude`, and run `/status`. The **Status** tab shows an `Anthropic base URL` line with
   the gateway's URL, and an `Auth token` line that names `ANTHROPIC_AUTH_TOKEN`.

The VS Code extension checks for a credential before it starts Claude Code, and reads it from VS
Code's user settings, not from `~/.claude/settings.json`. For the extension, set the two variables
in `claudeCode.environmentVariables` too.

## Verify

Send a request, then find it in the [dashboard](use-the-dashboard.md), or in Chowki's log: each
request logs its key, model, tokens and cost.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| At startup, Claude Code warns that two credential sources are set | You're signed in with a claude.ai account too. | Run `/logout`, so that only the virtual key remains. |
| `/status` shows a `Login method` line instead of an `Auth token` line | The variables didn't reach Claude Code. | Put them in the `env` of `~/.claude/settings.json`, or export them before you start `claude`. |
| Requests fail with `401` and `The virtual key is invalid.` | The token isn't a virtual key. | Create a key with `chowki key create`, and use it. |
| Requests fail with `429` and a message that the monthly budget is used up | The key's budget is used up. | Raise it with `chowki key update --budget-usd`. See [Set monthly budgets](set-budgets.md). |

## Related

- [Use Anthropic prompt caching](anthropic-prompt-caching.md) ·
  [Set rate limits](set-rate-limits.md)
