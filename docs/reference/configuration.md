---
title: Configuration reference
description: Every setting of chowki.yaml, its default, and the environment variable that overrides it.
type: reference
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

This page lists every setting of `chowki.yaml`, the file that `chowki init` creates and that
`chowki serve` and the other commands read.

Chowki builds its configuration in this order, and a later source wins:

1. The defaults on this page.
2. `chowki.yaml`, or the file that `--config` names.
3. Environment variables: `CHOWKI_` plus the setting's path in capitals, with `_` between the
   parts. For example, `CHOWKI_SERVER_LISTEN` sets `server.listen`. Lists, such as `providers`, can
   only be set in the file.

Chowki also reads a `.env` file from the folder where it runs. Variables that are set in the real
environment win over `.env`.

An unknown setting in `chowki.yaml`, such as a misspelled name, is an error. So is a provider key in
the file: keys come only from environment variables.

<!-- Written by hand for now; a test checks that it lists every environment variable. -->

## `server.listen`

| | |
|---|---|
| Type | String, `host:port` |
| Default | `:8080` |
| Environment variable | `CHOWKI_SERVER_LISTEN` |
| Since | v0.1 |

The address and port that the gateway listens on. `:8080` listens on every network interface;
`127.0.0.1:8080` listens only on this computer.

Example:

```yaml
server:
  listen: "127.0.0.1:8080"
```

## `server.max_body_mb`

| | |
|---|---|
| Type | Integer |
| Default | `20` |
| Allowed values | 1 to 1024 |
| Environment variable | `CHOWKI_SERVER_MAX_BODY_MB` |
| Since | v0.1 |

The largest request body that the gateway accepts, in MiB. A larger request gets a 413 error.

## `server.upstream_timeout`

| | |
|---|---|
| Type | Duration, such as `90s` or `10m` |
| Default | `600s` |
| Allowed values | Greater than zero |
| Environment variable | `CHOWKI_SERVER_UPSTREAM_TIMEOUT` |
| Since | v0.1 |

How long one call to a provider may take, including a whole streamed response. Long agent calls
need several minutes. A call that takes longer fails with a 504 error. A duration needs a unit: `s`
for seconds, `m` for minutes or `h` for hours.

## `storage.driver`

| | |
|---|---|
| Type | String |
| Default | `sqlite` |
| Allowed values | `sqlite` |
| Environment variable | `CHOWKI_STORAGE_DRIVER` |
| Since | v0.1 |

The database that stores virtual keys and request metadata.

## `storage.dsn`

| | |
|---|---|
| Type | String |
| Default | `file:data/chowki.db` |
| Environment variable | `CHOWKI_STORAGE_DSN` |
| Since | v0.1 |

Where the database is. For SQLite, a file path, relative to the folder where Chowki runs, with an
optional `file:` prefix. Chowki creates the file and its folder, readable only by the user that
runs Chowki.

## `storage.cache_max_mb`

| | |
|---|---|
| Type | Integer, in MiB |
| Default | `256` |
| Allowed values | 0 to 1048576 |
| Environment variable | `CHOWKI_STORAGE_CACHE_MAX_MB` |
| Since | v0.1 |

The largest total size of the responses in the exact cache. When the cache grows past it, Chowki
deletes the least recently used responses. `0` turns the cache off for every key. See
[Cache responses](../how-to/cache-responses.md).

## `security.master_key_file`

| | |
|---|---|
| Type | String, a file path |
| Default | `.chowki/master.key` |
| Environment variable | `CHOWKI_SECURITY_MASTER_KEY_FILE` |
| Since | v0.1 |

The file that holds the master key, which encrypts secrets that Chowki stores. `chowki init`
creates it. Chowki refuses to start when other users can read the file. The `CHOWKI_MASTER_KEY`
environment variable, if set, takes precedence over the file.

## `security.allow_private_upstreams`

| | |
|---|---|
| Type | Boolean |
| Default | `true` |
| Environment variable | `CHOWKI_SECURITY_ALLOW_PRIVATE_UPSTREAMS` |
| Since | v0.1 |

Whether provider base URLs may point to this computer or a private network, such as a local Ollama
server. Set it to `false` on a shared server whose providers are all on the internet. Link-local
and cloud metadata addresses, such as `169.254.169.254`, are always blocked.

## `log.level`

| | |
|---|---|
| Type | String |
| Default | `info` |
| Allowed values | `debug`, `info`, `warn`, `error` |
| Environment variable | `CHOWKI_LOG_LEVEL` |
| Since | v0.1 |

The least severe log messages that Chowki writes. Logs contain request metadata, never prompts,
responses or keys.

## `retention_days`

| | |
|---|---|
| Type | Integer |
| Default | `90` |
| Allowed values | 1 or more |
| Environment variable | `CHOWKI_RETENTION_DAYS` |
| Since | v0.1 |

How many days Chowki keeps request metadata. It deletes older records when it starts and once a
day.

## `defaults.cache`

| | |
|---|---|
| Type | String |
| Default | `off` |
| Allowed values | `off`, `exact` |
| Environment variable | `CHOWKI_DEFAULTS_CACHE` |
| Since | v0.1 |

Whether the non-streaming requests of virtual keys without their own cache setting use the exact
cache. `exact` answers a request that is identical to an earlier one with the stored response,
without calling the provider. A key's own setting, and a request's `x-chowki-cache` header, win
over this default. See [Cache responses](../how-to/cache-responses.md).

## `defaults.cache_ttl`

| | |
|---|---|
| Type | Duration, such as `30m` or `1h` |
| Default | `1h` |
| Allowed values | More than 0 |
| Environment variable | `CHOWKI_DEFAULTS_CACHE_TTL` |
| Since | v0.1 |

How long the exact cache serves a response after it was stored.

## `providers`

| | |
|---|---|
| Type | List |
| Default | None |
| Since | v0.1 |

The APIs that Chowki forwards requests to. Each provider has these fields:

| Field | Required | Description |
|---|---|---|
| `name` | Yes | A unique name of lowercase letters, digits, `-` and `_`. Clients name a provider in the model, such as `openai/gpt-6-luna`. |
| `type` | Yes | The API the provider speaks: `openai` for OpenAI and every OpenAI-compatible API, `anthropic`, or `gemini`. |
| `base_url` | Yes | The API's base URL, as its SDKs use it: with `/v1` for OpenAI-compatible APIs, without it for Anthropic. |
| `api_key_env` | No | The environment variable, or `.env` entry, that holds the provider key. Leave it out for a provider that needs no key, such as a local Ollama server. |

Example:

```yaml
providers:
  - name: openai
    type: openai
    base_url: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY
  - name: local
    type: openai
    base_url: http://localhost:11434/v1
```

When a provider's key variable isn't set, Chowki starts but logs a warning, and requests for that
provider fail with a `provider_key_missing` error.

## Other environment variables

| Variable | Description |
|---|---|
| `CHOWKI_MASTER_KEY` | The master key: 32 random bytes in base64. Takes precedence over `security.master_key_file`. |
| The variables named by `api_key_env` | Provider keys, such as `OPENAI_API_KEY`. |

## The `.env` file

A `.env` file holds one `KEY=VALUE` per line:

```sh
# Comment lines start with #.
OPENAI_API_KEY=<OPENAI_API_KEY>
export ANTHROPIC_API_KEY='<ANTHROPIC_API_KEY>'
CHOWKI_LOG_LEVEL=debug   # a comment after a value needs a space before the #
```

- A value in single quotes is taken literally. A value in double quotes can use `\n`, `\r`, `\t`,
  `\"` and `\\`.
- Chowki doesn't expand variables such as `$HOME` in values.
- Errors name the line but never show a value, because values are usually secrets.

## Related

- [Send your first request](../get-started/quickstart.md) ·
  [Manage virtual keys](../how-to/manage-virtual-keys.md)
