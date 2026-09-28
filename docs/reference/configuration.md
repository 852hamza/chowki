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
2. `chowki.yaml`, or the file that `--config` or `CHOWKI_CONFIG` names.
3. Environment variables: `CHOWKI_` plus the setting's path in capitals, with `_` between the
   parts. For example, `CHOWKI_SERVER_LISTEN` sets `server.listen`. Lists, such as `providers`, can
   only be set in the file.

Chowki also reads a `.env` file from the folder where it runs. Variables that are set in the real
environment win over `.env`.

An unknown setting in `chowki.yaml`, such as a misspelled name, is an error. So is a provider key
in the file: keys come from environment variables, or from the database, where
[`chowki provider set-key`](../how-to/manage-provider-keys.md) stores them encrypted.

## Summary

<!-- Generated from the code by TestConfigurationSummary in internal/config; run make docs. -->
<!-- generated:summary -->

| Setting | Environment variable | Default |
|---|---|---|
| [`server.listen`](#serverlisten) | `CHOWKI_SERVER_LISTEN` | `:8080` |
| [`server.max_body_mb`](#servermax_body_mb) | `CHOWKI_SERVER_MAX_BODY_MB` | `20` |
| [`server.upstream_timeout`](#serverupstream_timeout) | `CHOWKI_SERVER_UPSTREAM_TIMEOUT` | `10m` |
| [`server.read_timeout`](#serverread_timeout) | `CHOWKI_SERVER_READ_TIMEOUT` | `1m` |
| [`server.tls_cert_file`](#servertls_cert_file) | `CHOWKI_SERVER_TLS_CERT_FILE` | None |
| [`server.tls_key_file`](#servertls_key_file) | `CHOWKI_SERVER_TLS_KEY_FILE` | None |
| [`storage.driver`](#storagedriver) | `CHOWKI_STORAGE_DRIVER` | `sqlite` |
| [`storage.dsn`](#storagedsn) | `CHOWKI_STORAGE_DSN` | `file:data/chowki.db` |
| [`storage.cache_max_mb`](#storagecache_max_mb) | `CHOWKI_STORAGE_CACHE_MAX_MB` | `256` |
| [`security.master_key_file`](#securitymaster_key_file) | `CHOWKI_SECURITY_MASTER_KEY_FILE` | `.chowki/master.key` |
| [`security.allow_private_upstreams`](#securityallow_private_upstreams) | `CHOWKI_SECURITY_ALLOW_PRIVATE_UPSTREAMS` | `true` |
| [`log.level`](#loglevel) | `CHOWKI_LOG_LEVEL` | `info` |
| [`retention_days`](#retention_days) | `CHOWKI_RETENTION_DAYS` | `90` |
| [`defaults.cache`](#defaultscache) | `CHOWKI_DEFAULTS_CACHE` | `off` |
| [`defaults.cache_ttl`](#defaultscache_ttl) | `CHOWKI_DEFAULTS_CACHE_TTL` | `1h` |
| [`defaults.prompt_cache`](#defaultsprompt_cache) | `CHOWKI_DEFAULTS_PROMPT_CACHE` | `auto` |
| [`defaults.redaction`](#defaultsredaction) | `CHOWKI_DEFAULTS_REDACTION` | `mask` |
| [`providers`](#providers) | None: only in the file | None |
| [`aliases`](#aliases) | None: only in the file | None |

<!-- end generated:summary -->

<!-- The sections below are written by hand; a test checks that there is one for every setting. -->

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

## `server.read_timeout`

| | |
|---|---|
| Type | Duration, such as `30s` or `2m` |
| Default | `60s` |
| Allowed values | Greater than zero |
| Environment variable | `CHOWKI_SERVER_READ_TIMEOUT` |
| Since | v0.1 |

How long a client may take to send a request, its headers and its body, so that slow clients can't
hold connections open. It doesn't limit the response: a stream lasts as long as
`server.upstream_timeout` allows. Raise it for clients that send large requests over slow links.

## `server.tls_cert_file`

| | |
|---|---|
| Type | File path |
| Default | None |
| Allowed values | A PEM file with the certificate, then any intermediate certificates |
| Environment variable | `CHOWKI_SERVER_TLS_CERT_FILE` |
| Since | v0.1 |

With `server.tls_key_file`, makes the gateway serve HTTPS instead of HTTP, with TLS 1.2 or later.
Set both or neither. When the files change, as when a certificate is renewed, the gateway loads
them again within 30 seconds, without a restart. Without them, run the gateway behind a reverse
proxy that serves HTTPS, unless clients reach it only from the same machine. See
[Serve over HTTPS](../how-to/serve-over-https.md).

## `server.tls_key_file`

| | |
|---|---|
| Type | File path |
| Default | None |
| Allowed values | A PEM file with the certificate's private key |
| Environment variable | `CHOWKI_SERVER_TLS_KEY_FILE` |
| Since | v0.1 |

The private key of `server.tls_cert_file`. Make the file readable only by the user that runs the
gateway.

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

## `defaults.prompt_cache`

| | |
|---|---|
| Type | String |
| Default | `auto` |
| Allowed values | `auto`, `off` |
| Environment variable | `CHOWKI_DEFAULTS_PROMPT_CACHE` |
| Since | v0.1 |

Whether Chowki marks the prompt prefix of Anthropic requests for the provider's prompt cache when
the same tools and system prompt repeat. `off` forwards every request as the client sent it. See
[Use Anthropic prompt caching](../how-to/anthropic-prompt-caching.md).

## `defaults.redaction`

| | |
|---|---|
| Type | String |
| Default | `mask` |
| Allowed values | `off`, `mask`, `block`, `alert` |
| Environment variable | `CHOWKI_DEFAULTS_REDACTION` |
| Since | v0.1 |

What Chowki does with secrets and personal data in the text of requests from virtual keys without
their own redaction setting. `mask` replaces them with placeholders, `block` rejects the request,
`alert` forwards it and logs what it found, and `off` doesn't look. See
[Redact secrets and personal data](../how-to/redact-sensitive-data.md).

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
| `base_url` | Yes | The API's base URL, as its SDKs use it: with `/v1` for OpenAI-compatible APIs, without a version for Anthropic and Gemini. |
| `api_key_env` | No | The environment variable, or `.env` entry, that holds the provider key. When it isn't set, Chowki uses the key that `chowki provider set-key` stored, if any. Leave it out for a provider that needs no key, such as a local Ollama server. |
| `free_tier` | No | `true` when the provider doesn't bill your requests, such as on the free tier of the Gemini API. Chowki then records their cost as $0, and they spend no budget. Default `false`. |

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
  - name: gemini
    type: gemini
    base_url: https://generativelanguage.googleapis.com
    api_key_env: GEMINI_API_KEY
    free_tier: true
  - name: local
    type: openai
    base_url: http://localhost:11434/v1
```

When a provider's key variable isn't set, Chowki starts but logs a warning, and requests for that
provider fail with a `provider_key_missing` error.

## `aliases`

| | |
|---|---|
| Type | Map of names to lists of `<provider>/<model>` targets |
| Default | None |
| Environment variable | None; set it in the file |
| Since | v0.1 |

Model names that stand for lists of targets, tried in order: when a target fails with a rate
limit, a server error or a timeout, Chowki tries the next one. Every target must name a provider
from `providers`.

```yaml
aliases:
  fast: ["openai/gpt-6-luna", "backup/gpt-6-luna"]
```

See [Route requests with aliases and fallback](../how-to/routing-and-fallback.md).

## Other environment variables

| Variable | Description |
|---|---|
| `CHOWKI_CONFIG` | The configuration file that commands read without `--config`, instead of `chowki.yaml`. The container image sets it to `/etc/chowki/chowki.yaml`. Set it in the environment: Chowki chooses the file before it reads `.env`. |
| `CHOWKI_MASTER_KEY` | The master key: 32 random bytes in base64. Takes precedence over `security.master_key_file`. |
| `CHOWKI_PUBLIC_URL` | The gateway's address that `chowki setup` prints, such as `https://gateway.example.com`. It defaults to `http://localhost:8080`. |
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
