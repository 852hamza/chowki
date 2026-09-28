---
title: Send your first request through Chowki
description: Install Chowki, create a virtual key, and send a chat request through the gateway in about five minutes.
type: tutorial
since: v0.1
edition: community
sidebar_position: 1
last_reviewed: 2026-09-28
---

In this tutorial you set up Chowki on your computer and send a chat request through it with a
virtual key. It takes about five minutes.

You will:

1. Install Chowki and create its configuration, master key and database.
2. Create a virtual key.
3. Start the gateway, send a request through it, and see what it cost.

## Before you begin

- Linux or macOS, on amd64 or arm64. On Windows, use Windows Subsystem for Linux. To run Chowki in
  a container or build it from source instead, see [Install Chowki](install.md).
- An OpenAI API key. The request in this tutorial is billed to your OpenAI account and costs a
  small fraction of a cent.
- curl.

## Step 1: Install and initialize Chowki

Chowki keeps its configuration, master key and database in the folder where you run it, so give it
a folder of its own.

1. Install the latest release:

   ```sh
   curl -fsSL https://github.com/852hamza/chowki/raw/main/install.sh | sh
   ```

   The script downloads the binary for your system, checks it against the release's checksums, and
   installs it in `/usr/local/bin`, or in `~/.local/bin` when you can't write to `/usr/local/bin`.
   It prints the version that it installed. If it says to add a folder to your `PATH`, do so.

2. Create a folder for the gateway and initialize it:

   ```sh
   mkdir ~/chowki-gateway
   cd ~/chowki-gateway
   chowki init
   ```

   Output:

   ```text
   Created chowki.yaml.
   Created the master key in .chowki/master.key. Keep it private and back it up.
   The database is ready: file:data/chowki.db.

   Next steps:
     1. Put your provider keys in .env or the environment, as named by api_key_env.
     2. Create a virtual key: chowki key create --name <NAME>
     3. Start the gateway:    chowki serve
   ```

You now have `chowki.yaml`, which lists the OpenAI, Anthropic and Gemini providers, a master key
that only you can read, and an empty database.

## Step 2: Add your OpenAI key

Chowki reads provider keys from environment variables or a `.env` file, never from `chowki.yaml`,
so the configuration can be shared without leaking keys.

1. Save your OpenAI key in a `.env` file that only you can read:

   ```sh
   echo 'OPENAI_API_KEY=<OPENAI_API_KEY>' > .env
   chmod 600 .env
   ```

   Replace `<OPENAI_API_KEY>` with your OpenAI API key.

## Step 3: Create a virtual key

Your applications use virtual keys instead of your OpenAI key. You can give each person or app its
own virtual key and revoke it without touching the provider key.

1. Create a virtual key:

   ```sh
   chowki key create --name alice
   ```

   Output:

   ```text
   Created virtual key "alice" in project "default":

     chowki_EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE

   Copy it now. Chowki stores only a hash of it and can't show it again.
   ```

2. Keep the key in a variable for the next step:

   ```sh
   export CHOWKI_KEY=<VIRTUAL_KEY>
   ```

   Replace `<VIRTUAL_KEY>` with the key that the previous command printed.

## Step 4: Start the gateway and send a request

1. Start the gateway:

   ```sh
   chowki serve
   ```

   Chowki prints its log as JSON lines and keeps running. A line with `"msg":"listening"` shows
   the address. A warning that `ANTHROPIC_API_KEY` isn't set is expected when you don't use
   Anthropic.

2. In a second terminal, set `CHOWKI_KEY` again, then send a chat request to the OpenAI-format
   endpoint:

   ```sh
   curl http://localhost:8080/v1/chat/completions \
     -H "Authorization: Bearer $CHOWKI_KEY" \
     -H "Content-Type: application/json" \
     -d '{"model": "openai/gpt-6-luna", "messages": [{"role": "user", "content": "Say hello"}]}'
   ```

   The response is OpenAI's JSON, unchanged, with the model's answer in
   `choices[0].message.content`. The `openai/` prefix tells Chowki which provider to use.

## Verify

Send the same request again, but print only the response headers:

```sh
curl -sS -D - -o /dev/null http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $CHOWKI_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "openai/gpt-6-luna", "messages": [{"role": "user", "content": "Say hello"}]}'
```

Output:

```text
HTTP/1.1 200 OK
Content-Type: application/json
X-Chowki-Cache: bypass
X-Chowki-Cost-Usd: 0.00000570
X-Chowki-Request-Id: req_6fb74b7d836f291f73793b9f
Date: Sun, 27 Sep 2026 04:42:28 GMT
Content-Length: 287
```

`X-Chowki-Cost-Usd` is the cost of the request in US dollars, computed from the tokens that OpenAI
reported. `X-Chowki-Cache: bypass` means that the exact cache is off for this key; see
[Cache responses](../how-to/cache-responses.md). Your numbers and some headers differ. The
gateway's log in the first terminal has a line with `"msg":"request"` for each request, with its
token counts and cost.

## Troubleshooting

When something doesn't work, run `chowki doctor` in the Chowki folder: it checks the configuration,
the keys, the database and the port, and says what to fix. See
[Check your setup](../how-to/check-your-setup.md).

| Symptom | Cause | Fix |
|---|---|---|
| `chowki serve` fails with `address already in use` | Another program uses port 8080. | Start with `CHOWKI_SERVER_LISTEN=localhost:8081 chowki serve`, and send requests to port 8081. |
| The response has `"code":"invalid_api_key"` | `CHOWKI_KEY` isn't set in this terminal, or it holds a different key. | Set `CHOWKI_KEY` to the key from step 3. |
| The response has `"code":"provider_key_missing"` | Chowki can't read `OPENAI_API_KEY`. | Check that `.env` is in the folder where you ran `chowki serve`, then restart the gateway. |

## Use Chowki from your apps

Point an SDK or tool at Chowki by changing two settings:

| Your client speaks | Base URL | API key |
|---|---|---|
| OpenAI format, such as the OpenAI SDKs | `http://localhost:8080/v1` | Your virtual key |
| Anthropic format, such as the Anthropic SDKs | `http://localhost:8080/anthropic` | Your virtual key |
| Gemini format, such as the Google Gen AI SDKs | `http://localhost:8080/gemini` | Your virtual key |

Name the provider in the model, such as `openai/gpt-6-luna`, unless your configuration has only one
provider for that API.

`chowki setup <TOOL>` prints the settings for Claude Code, the Codex CLI, the Gemini CLI, the
official SDKs and Ollama; run `chowki setup` for the list. See
[Connect Claude Code](../how-to/connect-claude-code.md) and
[Connect the SDKs](../how-to/connect-sdks.md).

## Clean up

1. Stop the gateway with Ctrl+C in its terminal.
2. To remove everything the tutorial created, including the master key and the database, delete
   the folder:

   ```sh
   rm -rf ~/chowki-gateway
   ```

3. To uninstall Chowki too, delete the binary from the folder that the install script named, such
   as `/usr/local/bin/chowki` or `~/.local/bin/chowki`.

## What you learned

- Clients use Chowki by changing their base URL and API key; the request format stays the same.
- Provider keys stay with the gateway. Clients hold only virtual keys, which you can revoke.
- Chowki records the tokens and cost of every request.

## Next steps

- [Manage virtual keys](../how-to/manage-virtual-keys.md)
- [Connect Claude Code](../how-to/connect-claude-code.md), or [the SDKs](../how-to/connect-sdks.md)
- [Use the dashboard](../how-to/use-the-dashboard.md)
- [Configuration reference](../reference/configuration.md)
- [Architecture](../concepts/architecture.md)
