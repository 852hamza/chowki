---
title: Back up and restore Chowki
description: Back up the database and the master key while the gateway runs, and restore them after a loss.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

Chowki keeps its state in two places: the database, with keys, budgets, spend, request records and
the exact cache, and the master key, which encrypts stored provider keys and cached answers. Back
up both, and keep them apart from the gateway: a backup of the database without its master key
can't open its encrypted parts.

`chowki.yaml` and `.env` are yours to keep too, such as in version control and a secret manager.

## Before you begin

- Access to the Chowki folder, or to its Compose project for a container.

## Back up

1. Copy the database to a new file. `chowki backup` works while the gateway runs, and writes a
   consistent copy that only you can read:

   ```sh
   chowki backup chowki-2026-09-28.db
   ```

   Output:

   ```text
   Backed up the database to chowki-2026-09-28.db (0.1 MiB).
   Keep it with the master key, .chowki/master.key: the stored provider keys and cached answers in it open only with that key.
   ```

2. Copy the master key once, and again whenever you replace it. It's the file that
   `security.master_key_file` names, `.chowki/master.key` by default, unless `CHOWKI_MASTER_KEY`
   holds it.

Store both where the gateway's machine can't delete them, such as in object storage with
versioning. Run the backup on a schedule, for example daily with cron.

### With Docker Compose

In the folder with `compose.yaml`, stream the backup out of the running container to a file that
only you can read, and copy the master key out once:

```sh
(umask 077; docker compose exec -T chowki chowki backup - > chowki-2026-09-28.db)
docker compose cp chowki:/var/lib/chowki/master.key ./master.key
```

With `-`, `chowki backup` writes the backup to stdout, and its message to stderr.

## Restore

1. Stop the gateway.
2. Put the master key of the backup in place, if it isn't there.
3. Restore the database:

   ```sh
   chowki restore chowki-2026-09-28.db
   ```

   Output:

   ```text
   Restored the database from chowki-2026-09-28.db.
   The database that it replaced is in data/chowki.db.before-restore-20260928-020212.
   Check it with chowki doctor, then start chowki serve.
   ```

   `chowki restore` checks the backup with SQLite's integrity check first, and refuses a file that
   isn't a Chowki database, or one from a newer Chowki. It keeps the database that it replaces.

4. Run `chowki doctor`, then start `chowki serve`.

### With Docker Compose

Stop the gateway, stream the backup into a one-off container, and start the gateway again:

```sh
docker compose stop
docker compose run --rm -T chowki restore - < chowki-2026-09-28.db
docker compose start
```

When the volume is new, as after a lost server, give the container the backup's master key through
`.env`, before the restore:

```sh
echo "CHOWKI_MASTER_KEY=$(cat master.key)" >> .env
```

The gateway then reads the master key from the environment; `chowki doctor` shows
`master key  from CHOWKI_MASTER_KEY`. A key file copied into the container with
`docker compose cp` belongs to another user than the container's, which can't read it.

## What a restore brings back

Everything in the database as it was at the backup: keys and their settings, projects, admin
tokens, stored provider keys, spend, request records and cached answers. Spend and requests since
the backup are lost, so budgets count from the restored spend. Keys created since the backup no
longer work.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `chowki backup` says `<FILE> exists; name a new file` | Backups never overwrite a file. | Name a new file, such as one with the date. |
| `chowki restore` says `use a newer Chowki` | The backup comes from a newer Chowki, whose database this one can't read. | Restore it with that version or a newer one. |
| `chowki doctor` says a stored key `doesn't open with this master key` | The master key isn't the backup's. | Put the backup's master key in place. |

## Related

- [Upgrade Chowki](upgrade.md) · [Check your setup](../how-to/check-your-setup.md)
