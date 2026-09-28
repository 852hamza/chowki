---
title: Security model
description: What Chowki stores, encrypts and logs, how it checks keys, and what it protects against.
type: concept
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

Chowki sits between your apps and your AI providers, so it holds provider keys and sees every
prompt. This page describes what it stores, what it encrypts, what it never logs, and which attacks
its design guards against, so that you can judge where it fits in your own threat model.

## Keys

- **Virtual keys** are `chowki_`, 32 random bytes in base62, and a 6-character checksum, so that
  secret scanners, including `chowki scan`, recognize them. Chowki stores only the first 12
  characters, to identify a key, and its SHA-256 hash, and compares hashes in constant time. A key
  shows once, when it's created.
- **Admin tokens**, which authorize the admin API and the dashboard, start with `chowki_admin_` and
  are stored and checked the same way.
- **Provider keys** come from environment variables, or from the database, where
  `chowki provider set-key` stores them encrypted. They leave the gateway only in requests to their
  provider, and never in errors, logs or answers.

## The master key and encryption

`chowki init` creates a master key: 32 random bytes, in a file that only its owner can read. Chowki
refuses to start when other users can read the file. `CHOWKI_MASTER_KEY` can hold the key instead.

From the master key, Chowki derives a separate key with HKDF-SHA256 for each use, so that data
encrypted for one use never decrypts as another:

| Use | Protection |
|---|---|
| The exact cache | Each cached answer is encrypted with AES-256-GCM, with a random nonce, and bound to its request's hash. |
| Stored provider keys | Each key is encrypted with AES-256-GCM, and bound to its provider's name. |
| Redaction placeholders | The 8 hex digits in `[REDACTED:<type>:<digits>]` are an HMAC-SHA256 of the value, so that the same value gets the same placeholder without revealing it. |

Encrypted data carries the version of the master key, to allow the master key to be rotated later.
Back up the master key with the database: without it, cached answers and stored provider keys can't
be read.

## What Chowki stores and logs

Chowki records metadata of each request: the time, the key, the model and provider, the status,
the tokens, the cost and the latency. It never stores or logs prompts, answers, keys or
`Authorization` headers. The one exception is the exact cache, which you turn on per key or by
default, and which stores answers encrypted. Cached answers are kept apart by project, so that
projects never share them.

Redaction masks, blocks or reports secrets and personal data in prompts before they reach a
provider. Its log lines and records count what it found, by type, and never hold the values.

## The network

- **Server-side request forgery.** Provider addresses come only from the configuration. Chowki
  never connects to link-local or cloud metadata addresses, such as `169.254.169.254`, and
  connects to loopback and private addresses only while `security.allow_private_upstreams` is
  `true`. It checks every address after DNS resolution, so a name that resolves to a forbidden
  address is refused too. It doesn't follow redirects, and doesn't use proxies from the
  environment, either of which could lead around these rules.
- **Only the needed headers reach providers:** the provider's key, and the API version and beta
  headers of Anthropic. The client's own key never does.
- **Limits.** Chowki accepts request bodies up to `server.max_body_mb`, headers up to 64 KiB, and
  gives clients `server.read_timeout` to send a request and 10 seconds to send its headers. Each
  provider call ends after `server.upstream_timeout`.
- **TLS.** Chowki serves HTTPS with TLS 1.2 or later when you give it a certificate, or runs behind
  a reverse proxy that does. See [Serve over HTTPS](../how-to/serve-over-https.md).

> **Warning:** `/healthz`, `/readyz` and `/metrics` answer without a key, and the metrics name your
> providers and models and show your costs. Don't expose the gateway's port to the internet without
> a reverse proxy that blocks `/metrics`.

## The dashboard

The dashboard signs in with an admin token. Its session cookie is `HttpOnly` and `SameSite=Strict`,
and `Secure` over HTTPS; sessions last 12 hours, and end when their token is revoked. Forms carry a
CSRF token. The pages allow no inline scripts or styles and no other sites, through a strict
Content Security Policy, and can't be framed.

## The supply chain

Chowki is one static binary with two direct dependencies beyond the Go standard library: a pure Go
SQLite and a YAML parser. With the modules that they need, nine modules are linked in, all under
the MIT, BSD or Apache 2.0 licenses. Continuous
integration verifies downloaded modules against `go.sum`, checks for known vulnerabilities with
`govulncheck`, and pins every GitHub Action to a commit. The container image is built on a
distroless base, pinned by digest, and runs as a non-root user.

## Reporting a vulnerability

See [SECURITY.md](../../SECURITY.md) for how to report a vulnerability privately.

## Related

- [Architecture](architecture.md) ·
  [Redact secrets and personal data](../how-to/redact-sensitive-data.md) ·
  [Store provider keys in Chowki](../how-to/manage-provider-keys.md)
