---
title: Check your setup with chowki doctor
description: Find what stops the gateway from starting or working, and how to fix it, before you run chowki serve.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

`chowki doctor` checks what `chowki serve` needs: the `.env` file, the configuration, the master
key, the providers' keys and prices, the model catalog, the database, the keys and the listen
address. It says what to fix, and changes nothing: it doesn't create or migrate the database, and
it sends no request to a provider.

## Before you begin

- The `chowki` binary, in the folder where you run `chowki serve`, such as one set up with
  `chowki init`. `chowki doctor` reads `.env` from the current folder, as `chowki serve` does.

## Run the checks

1. In the Chowki folder, run:

   ```sh
   chowki doctor
   ```

   Output, for a folder set up with `chowki init`, an OpenAI key in `.env`, and one virtual key:

   ```text
   ok    .env                .env is readable only by you
   ok    config              chowki.yaml loads, with 3 providers and 0 aliases
   ok    master key          .chowki/master.key is readable only by you
   ok    provider openai     key in OPENAI_API_KEY; 3 models priced
   warn  provider anthropic  ANTHROPIC_API_KEY isn't set, so its requests fail until it is; 4 models priced
   warn  provider gemini     GEMINI_API_KEY isn't set, so its requests fail until it is; 12 models priced
   ok    catalog             19 models, with prices checked on 2026-09-27 or later
   ok    database            data/chowki.db, schema version 9, up to date
   ok    keys                1 active virtual key
   ok    admin tokens        none; chowki admin create --name <NAME> makes one for the dashboard and the admin API
   ok    listen              :8080 is free for chowki serve

   2 checks warned; none failed.
   ```

   Each line has a status, the check and what it found:

   - `ok`: nothing to do.
   - `warn`: the gateway runs, but something doesn't work, or isn't safe. Here, requests to
     Anthropic and Gemini models fail until their keys are set, in the environment or with
     [`chowki provider set-key`](manage-provider-keys.md); if you don't use those providers, remove
     them from `chowki.yaml`.
   - `fail`: `chowki serve` can't start until you fix it.

2. Fix what the lines say, then run `chowki doctor` again.

To check another configuration file, name it: `chowki doctor --config <FILE>`.

`chowki doctor` exits with `0` when no check fails, and with `1` when one does, so scripts can run
it before `chowki serve`.

## What each check looks for

| Check | Warns or fails when |
|---|---|
| `.env` | The file can't be parsed, or other users can read it. |
| `config` | The configuration file is missing or invalid. Every problem is listed, each with its setting. |
| `master key` | The master key is missing, isn't 32 bytes in base64, or other users can read its file. |
| `provider <NAME>` | The provider has no key, from its variable or stored; its stored key doesn't open with the master key; its `base_url` sends a key over plain `http` to a host that isn't local; or the catalog has no prices for a provider of that name. |
| `catalog` | Some prices were checked more than 90 days ago. A newer Chowki has newer prices. |
| `database` | The database is newer than this Chowki, is in memory, or other users can read its file. |
| `keys` | There's no active virtual key, so no app can use the gateway. |
| `listen` | Another program listens on `server.listen`, or the address can't be used. A running `chowki serve` there passes. |

The catalog finds prices by the provider's name, so name the providers of the OpenAI, Anthropic and
Gemini APIs `openai`, `anthropic` and `gemini`. Requests to a provider without prices are
unpriced: they count as $0 toward budgets. See
[Set monthly budgets](set-budgets.md).

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `fail  listen  another program listens on :8080` | Another program uses the port. | Stop it, or set `server.listen` in `chowki.yaml`, or `CHOWKI_SERVER_LISTEN`, to another address, such as `:8081`. |
| `fail  config  read config: open chowki.yaml: no such file or directory` | There's no configuration in the current folder. | Run `chowki doctor` in the Chowki folder, run `chowki init`, or pass `--config <FILE>`. |
| `warn  provider <NAME>` says the key isn't set, but it is in `.env` | `.env` isn't in the current folder. | Run `chowki doctor` and `chowki serve` in the folder that holds `.env`. |

## Related

- [Send your first request](../get-started/quickstart.md) ·
  [Configuration reference](../reference/configuration.md)
