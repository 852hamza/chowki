---
title: Install Chowki
description: Install the Chowki binary, run it in a container with Docker Compose or Docker, or build it from source.
type: how-to
since: v0.1
edition: community
sidebar_position: 2
last_reviewed: 2026-09-28
---

Chowki is a single binary. Install a release, run it in a container with Docker Compose or
Docker, or build it from source.

| Way | You need | Good for |
|---|---|---|
| [Install script](#install-the-binary) | Linux or macOS, and curl | The quickest start, on your computer or a server |
| [Docker Compose](#run-with-docker-compose) | Docker Engine with the Compose plugin | A gateway on a server or on your computer |
| [Docker](#run-with-docker) | Docker Engine | Your own container setup |
| [From source](#build-from-source) | Go 1.27 or later, Git and GNU Make | Development, or a machine without Docker |

## Install the binary

Install the latest release with one command:

```sh
curl -fsSL https://github.com/852hamza/chowki/raw/main/install.sh | sh
```

The [script](https://github.com/852hamza/chowki/blob/main/install.sh) downloads the archive for
your system from the latest GitHub release, checks it against the release's `checksums.txt`, and
installs `chowki` in `/usr/local/bin`, or in `~/.local/bin` when you can't write to
`/usr/local/bin`. Run it again to upgrade. These environment variables change what it does:

| Variable | Effect |
|---|---|
| `CHOWKI_VERSION` | Installs this version, such as `v0.1.0`, instead of the latest. |
| `CHOWKI_INSTALL_DIR` | Installs the binary in this folder. |
| `CHOWKI_RELEASES_URL` | Downloads from this mirror of the releases page. |

For example, to install a version for yourself only:

```sh
curl -fsSL https://github.com/852hamza/chowki/raw/main/install.sh |
  CHOWKI_VERSION=v0.1.0 CHOWKI_INSTALL_DIR="$HOME/bin" sh
```

To install by hand, or on Windows, download the archive for your system from the
[releases page](https://github.com/852hamza/chowki/releases), check it against `checksums.txt`,
and put the `chowki` binary in a folder in your `PATH`. Each release also has an SBOM of every
archive and signed build provenance; see
[Verify a release](../contributing/releasing.md#verify-a-release).

Then set up a folder for the gateway, as [Send your first request](quickstart.md) shows.

## Run with Docker Compose

The [Compose file](https://github.com/852hamza/chowki/blob/main/deploy/compose.yaml) runs the
published image, `ghcr.io/852hamza/chowki`, keeps the database and the master key in a volume, and
publishes the gateway on port 8080 of this machine only.

1. Make a folder for the gateway, and download the Compose file into it:

   ```sh
   mkdir chowki && cd chowki
   curl -fsSLO https://github.com/852hamza/chowki/raw/main/deploy/compose.yaml
   ```

2. Put your provider keys in a `.env` file in this folder, readable only by you:

   ```sh
   echo 'OPENAI_API_KEY=<OPENAI_API_KEY>' > .env
   chmod 600 .env
   ```

   Replace `<OPENAI_API_KEY>` with your key. Add `ANTHROPIC_API_KEY` and `GEMINI_API_KEY` the same
   way for those providers.

3. Create the master key and the database in the volume. The first command downloads the image:

   ```sh
   docker compose run --rm chowki init
   ```

   Output of `chowki init`:

   ```text
   Using the existing /etc/chowki/chowki.yaml.
   Created the master key in /var/lib/chowki/master.key. Keep it private and back it up.
   The database is ready: file:/var/lib/chowki/data/chowki.db.

   Next steps:
     1. Put your provider keys in .env or the environment, as named by api_key_env.
     2. Create a virtual key: chowki key create --name <NAME>
     3. Start the gateway:    chowki serve
   ```

4. Create a virtual key for your app:

   ```sh
   docker compose run --rm chowki key create --name my-app
   ```

   Output:

   ```text
   Created virtual key "my-app" in project "default":

     chowki_EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE

   Copy it now. Chowki stores only a hash of it and can't show it again.
   ```

5. Start the gateway in the background:

   ```sh
   docker compose up -d
   ```

6. Check that it's ready:

   ```sh
   curl http://localhost:8080/readyz
   ```

   Output:

   ```text
   {"status":"ready"}
   ```

Send requests to `http://localhost:8080` with the virtual key, as in
[Send your first request](quickstart.md). Run other commands the same way, such as
`docker compose run --rm chowki key list`, or check the running gateway with
`docker compose exec chowki chowki doctor`. `docker compose logs chowki` shows its log.

After you edit `.env`, run `docker compose up -d` again, so that the container gets the change.

Besides provider keys, `.env` can hold two settings for Compose itself:

| Variable | Effect |
|---|---|
| `CHOWKI_IMAGE` | The image to run; `ghcr.io/852hamza/chowki:latest` by default. Name a version, such as `ghcr.io/852hamza/chowki:0.1.0`, to stay on it until you [upgrade](../operations/upgrade.md). |
| `CHOWKI_PORT` | The port on this machine that the gateway listens on; `8080` by default. |

## Run with Docker

Run each command with the data volume:

```sh
docker volume create chowki-data
docker run --rm -v chowki-data:/var/lib/chowki ghcr.io/852hamza/chowki init
docker run --rm -v chowki-data:/var/lib/chowki ghcr.io/852hamza/chowki key create --name my-app
docker run -d --name chowki --env-file .env -p 127.0.0.1:8080:8080 \
  -v chowki-data:/var/lib/chowki ghcr.io/852hamza/chowki
```

The image has a tag for each version, such as `ghcr.io/852hamza/chowki:0.1.0`, and `latest` for
the newest release. It's built for `linux/amd64` and `linux/arm64`.

To build the image from a clone of the repository instead, run `make docker`, which tags it
`chowki:dev`. To run that image with Compose, set `CHOWKI_IMAGE=chowki:dev` in `.env`.

## What the image holds

| Path | What |
|---|---|
| `/usr/local/bin/chowki` | The binary, the image's entry point. Without arguments, it runs `chowki serve`. |
| `/etc/chowki/chowki.yaml` | The configuration, from [`deploy/chowki.yaml`](https://github.com/852hamza/chowki/blob/main/deploy/chowki.yaml): the settings that `chowki init` writes, with the data files in `/var/lib/chowki`. `CHOWKI_CONFIG` points every command at it. |
| `/var/lib/chowki` | The data folder, for a volume: the database and the master key. Back it up. |

The image is about 18 MB. It's built on the distroless static image, which has no shell, and runs
as the user 65532, not as root. It listens on port 8080.

To change the configuration, mount your own file on `/etc/chowki/chowki.yaml`. The container's
user must be able to read it, so make it readable by all with `chmod 644`: it holds no keys. With
Compose, put the mount in a `compose.override.yaml` file next to `compose.yaml`. Compose reads both
files, so the Compose file that you downloaded stays as it is:

```yaml
services:
  chowki:
    volumes:
      - ./chowki.yaml:/etc/chowki/chowki.yaml:ro
```

The Compose file also runs the container with a read-only file system and without Linux
capabilities, and lets it reach the Docker host as `host.docker.internal`, for a provider such as
Ollama that runs there.

## Build from source

```sh
git clone https://github.com/852hamza/chowki.git
cd chowki
make build
./bin/chowki version
```

`make build` writes the binary to `bin/chowki`, with the version and the commit in it. Copy it to a
folder in your `PATH`, such as `/usr/local/bin`. Then set up a folder for the gateway, as
[Send your first request](quickstart.md) shows.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `master key file /var/lib/chowki/master.key doesn't exist; run chowki init, or set CHOWKI_MASTER_KEY` | The volume is new. | Run `docker compose run --rm chowki init`. |
| `read config: open /etc/chowki/chowki.yaml: permission denied` | The mounted configuration is readable only by its owner. | Run `chmod 644` on it. |
| `docker compose up` fails with `port is already allocated` or `address already in use` | Another program or container uses port 8080. | Set another port in `.env`, such as `CHOWKI_PORT=8081`, run `docker compose up -d`, and send requests to that port. |
| Requests to Ollama on the Docker host fail with `upstream_unavailable` | Ollama listens only on 127.0.0.1 by default, which containers can't reach. | Set `OLLAMA_HOST=0.0.0.0:11434` for Ollama, as its FAQ explains, and use `http://host.docker.internal:11434/v1` as the provider's `base_url`. Allow only trusted machines to reach that port. |

## Related

- [Send your first request](quickstart.md) · [Check your setup](../how-to/check-your-setup.md) ·
  [Configuration reference](../reference/configuration.md)
