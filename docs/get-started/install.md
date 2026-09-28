---
title: Install Chowki
description: Build Chowki from source, or run it in a container with Docker Compose or Docker.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

Chowki is a single binary. Build it from source, or run it in a container with Docker Compose or
Docker. Binary releases and published images come with the first release.

| Way | You need | Good for |
|---|---|---|
| [Docker Compose](#run-with-docker-compose) | Docker Engine with the Compose plugin, and Git | A gateway on a server or on your computer |
| [Docker](#run-with-docker) | Docker Engine, and Git | Your own container setup |
| [From source](#build-from-source) | Go 1.27 or later, Git and GNU Make | Development, or a machine without Docker |

## Run with Docker Compose

The Compose file in `deploy/` builds the image, keeps the database and the master key in a volume,
and publishes the gateway on port 8080 of this machine only.

1. Get the repository, and go to its `deploy` folder:

   ```sh
   git clone https://github.com/852hamza/chowki.git
   cd chowki/deploy
   ```

2. Put your provider keys in a `.env` file in this folder, readable only by you:

   ```sh
   echo 'OPENAI_API_KEY=<OPENAI_API_KEY>' > .env
   chmod 600 .env
   ```

   Replace `<OPENAI_API_KEY>` with your key. Add `ANTHROPIC_API_KEY` and `GEMINI_API_KEY` the same
   way for those providers.

3. Build the image, and create the master key and the database in the volume:

   ```sh
   docker compose build
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

## Run with Docker

Build the image from the repository's root folder, then run each command with the data volume:

```sh
docker build -f deploy/Dockerfile -t chowki:dev .
docker volume create chowki-data
docker run --rm -v chowki-data:/var/lib/chowki chowki:dev init
docker run --rm -v chowki-data:/var/lib/chowki chowki:dev key create --name my-app
docker run -d --name chowki --env-file .env -p 127.0.0.1:8080:8080 \
  -v chowki-data:/var/lib/chowki chowki:dev
```

With GNU Make, `make docker` builds the same image, with the version and commit in it.

## What the image holds

| Path | What |
|---|---|
| `/usr/local/bin/chowki` | The binary, the image's entry point. Without arguments, it runs `chowki serve`. |
| `/etc/chowki/chowki.yaml` | The configuration, from [`deploy/chowki.yaml`](../../deploy/chowki.yaml): the settings that `chowki init` writes, with the data files in `/var/lib/chowki`. `CHOWKI_CONFIG` points every command at it. |
| `/var/lib/chowki` | The data folder, for a volume: the database and the master key. Back it up. |

The image is about 18 MB. It's built on the distroless static image, which has no shell, and runs
as the user 65532, not as root. It listens on port 8080.

To change the configuration, mount your own file on `/etc/chowki/chowki.yaml`. The container's
user must be able to read it, so make it readable by all with `chmod 644`: it holds no keys. With
Compose, add the mount to the `chowki` service:

```yaml
    volumes:
      - data:/var/lib/chowki
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
| `docker compose up` fails with `port is already allocated` or `address already in use` | Another program or container uses port 8080. | Change the published port in `compose.yaml`, such as `127.0.0.1:8081:8080`, and send requests to it. |
| Requests to Ollama on the Docker host fail with `upstream_unavailable` | Ollama listens only on 127.0.0.1 by default, which containers can't reach. | Set `OLLAMA_HOST=0.0.0.0:11434` for Ollama, as its FAQ explains, and use `http://host.docker.internal:11434/v1` as the provider's `base_url`. Allow only trusted machines to reach that port. |

## Related

- [Send your first request](quickstart.md) · [Check your setup](../how-to/check-your-setup.md) ·
  [Configuration reference](../reference/configuration.md)
