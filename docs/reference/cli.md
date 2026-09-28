---
title: CLI reference
description: Every chowki command, with its usage as chowki prints it.
type: reference
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

`chowki` is the gateway's server and its command-line tool. Each command prints its usage with
`--help`, as below. Commands that read the configuration use `chowki.yaml` in the current folder,
the file that `CHOWKI_CONFIG` names, or the one that `--config` names; see the
[configuration reference](configuration.md).

Exit codes: `0` on success, `1` when a command fails, and `2` for a mistake in its arguments.
`chowki scan` exits with `1` when it finds secrets and `2` when it can't scan, and `chowki doctor`
with `1` when a check fails.

<!-- Generated from each command's --help by TestCLIReference in cmd/chowki; run make docs. -->
<!-- generated:commands -->

```text
$ chowki help
Chowki is a self-hosted AI gateway.

Usage:
  chowki <command> [arguments]

Commands:
  serve      Run the gateway
  init       Create a configuration, a master key and the database
  provider   List providers, and store their keys encrypted
  key        Create, list, update and revoke virtual keys
  project    List projects and set their budgets
  admin      Create, list and revoke admin tokens
  usage      Report token usage, cost and savings
  backup     Copy the database to a file, while the gateway runs
  restore    Replace the database with a backup
  scan       Find secrets in a repository, .env files or MCP configurations
  setup      Connect an app or coding agent to the gateway
  doctor     Check the installation and configuration
  version    Print version information

Documentation: https://github.com/852hamza/chowki/tree/main/docs
Report a bug:  https://github.com/852hamza/chowki/issues
```

## `chowki serve`

Run the gateway.

```text
Usage:
  chowki serve [--config <FILE>]

Runs the gateway. It listens on server.listen and relays requests to the
providers, until it gets an interrupt or SIGTERM; then it stops taking
requests and waits up to 30 seconds for those in flight. It reads .env from
the current folder, and writes its log to stderr, in JSON.

Flags:
  --config  the configuration file; by default $CHOWKI_CONFIG, or chowki.yaml
```


## `chowki init`

Create a configuration, a master key and the database.

```text
Usage:
  chowki init [--config <FILE>]

Creates what the gateway needs, and keeps what exists: the configuration
file, a master key that only you can read, where security.master_key_file
says, and the database. Run it in the folder where you run chowki serve.

Flags:
  --config  the configuration file to create or use; by default
            $CHOWKI_CONFIG, or chowki.yaml
```


## `chowki provider`

List providers, and store their keys encrypted.

```text
Usage:
  chowki provider list [--config <FILE>]
  chowki provider set-key [--config <FILE>] <NAME>
  chowki provider remove-key [--config <FILE>] <NAME>

The providers are those of the configuration. Each gets its key from the
environment variable that its api_key_env names, or from a key stored in
the database with set-key, encrypted with the master key. A variable that
is set wins over a stored key.

set-key reads the key from standard input, never from the command line,
which shell history and process lists show:

  read -rs KEY && echo "$KEY" | chowki provider set-key <NAME>

Restart chowki serve after a change, so that it uses the key.
```


## `chowki key`

Create, list, update and revoke virtual keys.

```text
Usage:
  chowki key create --name <NAME> [--project <PROJECT>] [SETTINGS] [--config <FILE>]
  chowki key list [--config <FILE>]
  chowki key update [--config <FILE>] SETTINGS <PREFIX>
  chowki key revoke [--config <FILE>] <PREFIX>

SETTINGS are one or more of these:
  --budget-usd <USD>  monthly budget in US dollars; 0 means none
  --rpm <N>           limit of requests per minute; 0 means none
  --tpm <N>           limit of input and output tokens per minute; 0 means none
  --cache <MODE>      exact cache: exact, off, or default to follow chowki.yaml
  --redaction <MODE>  secrets and personal data in prompts: mask, block, alert,
                      off, or default to follow chowki.yaml
  --models <LIST>     comma-separated models, aliases and patterns such as
                      openai/* that the key may use; all allows every model

A virtual key is shown once, when you create it. Update or revoke a key by
its prefix, the first 12 characters, as "chowki key list" shows them.
```


## `chowki project`

List projects and set their budgets.

```text
Usage:
  chowki project list [--config <FILE>]
  chowki project update [--config <FILE>] --budget-usd <USD> <NAME>

A project groups virtual keys; "chowki key create --project" creates it.
--budget-usd sets a monthly budget in US dollars that all the project's keys
share; 0 means no budget.
```


## `chowki admin`

Create, list and revoke admin tokens.

```text
Usage:
  chowki admin create --name <NAME> [--config <FILE>]
  chowki admin list [--config <FILE>]
  chowki admin revoke [--config <FILE>] <PREFIX>

An admin token authorizes the admin API and the dashboard. It's shown once,
when you create it. Revoke a token by its prefix, the first 18 characters,
as "chowki admin list" shows them.
```


## `chowki usage`

Report token usage, cost and savings.

```text
Usage:
  chowki usage [--from <DATE>] [--to <DATE>] [--by key|model|day] [--config <FILE>]

Reports the requests, tokens, cost and savings of the days from --from to
--to, both included, in UTC. By default it reports this month so far.

Flags:
  --from    the first day, such as 2026-09-01
  --to      the last day, such as 2026-09-30
  --by      also break the totals down by key, model or day
  --config  the configuration file; by default $CHOWKI_CONFIG, or chowki.yaml
```


## `chowki backup`

Copy the database to a file, while the gateway runs.

```text
Usage:
  chowki backup [--config <FILE>] <BACKUP>

Writes a consistent copy of the database to BACKUP, a new file that only
you can read, or to stdout when BACKUP is -. It works while chowki serve
runs, and doesn't upgrade the database, so back up before you upgrade
Chowki.

The copy holds stored provider keys and cached answers, which open only
with the master key: keep the two together, and apart from the gateway.

Flags:
  --config  the configuration file; by default $CHOWKI_CONFIG, or chowki.yaml
```


## `chowki restore`

Replace the database with a backup.

```text
Usage:
  chowki restore [--config <FILE>] <BACKUP>

Replaces the database with BACKUP, a file that chowki backup wrote, or
stdin when BACKUP is -, after checking it. Stop chowki serve first, and
start it again after. The replaced database stays next to it, as
<NAME>.before-restore-<TIME>.

Flags:
  --config  the configuration file; by default $CHOWKI_CONFIG, or chowki.yaml
```


## `chowki scan`

Find secrets in a repository, .env files or MCP configurations.

```text
Usage:
  chowki scan [--format text|json|sarif] [--output <FILE>] [--pii] [--exit-zero] [<PATH>...]

Finds secrets, such as API keys, private keys and passwords, in files: a
repository, a directory, .env files and MCP configurations. <PATH> is a
file or a directory, the current directory by default. In a git
repository, it reads the files that git tracks or would track.

It never prints the secrets, only where they are. It exits with 1 when it
finds any, 0 otherwise, and 2 on an error.

Flags:
  --format     text (default), json or sarif, for GitHub code scanning
  --output     the file to write the report to, instead of stdout
  --pii        report personal data too, such as email addresses
  --exit-zero  exit with 0 even when there are findings
```


## `chowki setup`

Connect an app or coding agent to the gateway.

```text
Usage:
  chowki setup <TOOL> [--url <URL>] [--key <VIRTUAL_KEY>] [--model <MODEL>]

Prints the settings that connect a tool or SDK to the gateway. It changes no
file. <TOOL> is one of:

  claude-code       Claude Code
  codex             the OpenAI Codex CLI
  gemini-cli        the Gemini CLI
  openai-sdk        the OpenAI SDKs
  anthropic-sdk     the Anthropic SDKs
  google-genai-sdk  the Google Gen AI SDKs
  ollama            Ollama, as a provider of the gateway

Flags:
  --url    the gateway's address; by default $CHOWKI_PUBLIC_URL, or
           http://localhost:8080
  --key    a virtual key to put in the settings; by default a placeholder
  --model  the model to put in the settings, for the tools that need one
```


## `chowki doctor`

Check the installation and configuration.

```text
Usage:
  chowki doctor [--config <FILE>]

Checks that chowki serve can start and work with the configuration: the .env
file, the configuration, the master key, the providers' keys and prices, the
model catalog, the database and the listen address. It changes nothing and
calls no provider. It exits with 1 when a check fails.

Flags:
  --config  the configuration file; by default $CHOWKI_CONFIG, or chowki.yaml
```


## `chowki version`

Print version information.

```text
Usage:
  chowki version

Prints the version, commit and build date of chowki, its Go version and
platform, and its repository.
```

<!-- end generated:commands -->
