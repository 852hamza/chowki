---
title: Scan for secrets
description: Find API keys, private keys, tokens and passwords in a repository, its .env files and MCP configurations, locally and in CI.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

`chowki scan` finds secrets before they leak: API keys of AI providers and clouds, private keys,
tokens and passwords, in a repository, its `.env` files and the MCP configurations of coding
agents. Run it on your machine, or in CI on every change.

## Before you begin

- The `chowki` binary. `chowki scan` needs no configuration, gateway or database.
- The scan reads files only; it sends nothing anywhere.

## Scan a repository

1. In the repository's folder, run:

   ```sh
   chowki scan
   ```

   Output, for a repository with a key in its settings and a token in its MCP configuration:

   ```text
   .mcp.json:6:31: secret: A password or secret assigned to a setting.
   config/settings.yaml:4:18: aws_access_key: An AWS access key ID.
   Found 2 secrets in 2 files (3 files scanned).
   ```

   Each line gives the file, the line and the column where a secret starts, and its type. Chowki
   never prints the secrets themselves.

2. For each finding, revoke the secret where it was issued and create a new one, then remove it
   from the file. A secret that was committed stays in the repository's history, so revoke it even
   after you delete it.

In a git repository, `chowki scan` reads the files that git tracks or would track, and skips the
files that `.gitignore` ignores, such as your local `.env`. To scan a file or a folder anyway,
name it:

```sh
chowki scan .env ~/.claude.json
```

Outside git, it reads every file, except folders such as `.git`, `node_modules` and `.venv`. It
skips binary files and files larger than 5 MiB.

## What the scan finds

- Keys and tokens with a known format: AWS, GitHub, Slack, OpenAI, Anthropic, Google, Stripe, JSON
  Web Tokens, Chowki virtual keys and admin tokens, and private keys.
- Passwords assigned to a setting, such as `password = …`.
- In `.env` files, the value of every setting whose name says it's secret, such as
  `DATABASE_PASSWORD` or `API_TOKEN`, when the value looks random enough to be one.
- In MCP configurations, the `env` and `headers` values of servers, in `mcpServers` or VS Code's
  `servers`, the same way, at any depth: `~/.claude.json` lists servers for each project.

Values that stand for a secret aren't findings: references such as `${API_KEY}` or `$API_KEY`,
placeholders such as `<API_KEY>`, `your-key-here` or `changeme`, keys that contain the word
`example` in any case, as providers' documentation uses them, and short or repetitive values.

With `--pii`, the scan reports personal data too: email addresses, phone numbers, payment card and
bank account numbers, and Pakistani CNIC and mobile numbers.

## Choose the output

| Option | Effect |
|---|---|
| `--format text` | One line per finding, then a summary. The default. |
| `--format json` | A JSON document, with the start and end of each finding. |
| `--format sarif` | A SARIF 2.1.0 log for GitHub code scanning and other tools. |
| `--output <FILE>` | Writes the report to a file instead of the terminal. |
| `--exit-zero` | Exits with 0 even when there are findings. |

`chowki scan` exits with `0` when it finds no secrets, `1` when it finds any, and `2` when it
can't scan, for example because a path doesn't exist. CI systems fail the job on `1`.

## Scan in GitHub Actions

Add a workflow, such as `.github/workflows/secret-scan.yml`:

```yaml
name: Secret scan
on: [push, pull_request]
permissions:
  contents: read
  security-events: write
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: 852hamza/chowki@<VERSION>
```

Replace `<VERSION>` with a Chowki release, such as `v0.1.0`. The action builds Chowki, scans the
repository, uploads the findings to the repository's **Security** tab, and fails the job when it
finds secrets. Its inputs:

| Input | Default | Effect |
|---|---|---|
| `path` | `.` | The file or folder to scan. |
| `version` | `latest` | The Chowki version to install. |
| `upload-sarif` | `true` | Uploads the findings to GitHub code scanning. It needs the `security-events: write` permission. |
| `fail-on-findings` | `true` | Fails the step when the scan finds secrets. |

In other CI systems, run `chowki scan` as a step: it fails the job when it finds secrets.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| A finding is an example, not a real secret | The value looks like a real key. | Replace it with a value that contains `EXAMPLE`, or with a reference such as `${API_KEY}`. |
| A secret in `.env` isn't found | `.gitignore` ignores `.env`, so the scan of the repository skips it. | Scan the file by name: `chowki scan .env`. |
| The upload to code scanning fails with `Resource not accessible by integration` | The job lacks the `security-events: write` permission. | Add it to the workflow's `permissions`. |

## Related

- [Redact secrets and personal data](redact-sensitive-data.md): mask them in prompts, at the
  gateway
