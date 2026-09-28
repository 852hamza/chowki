---
title: Upgrade Chowki
description: Move to a newer version of Chowki, with a backup to return to.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

Upgrading Chowki replaces its binary or image. When the new version starts, it upgrades the
database's schema, and the gateway runs on. A newer schema can't be read by an older Chowki, so
back up before you upgrade: the backup is the way back.

## Before you begin

- Read the changes of the new version in `CHANGELOG.md`, for anything that asks you to act.

## Upgrade the binary

1. Back up the database with the version that runs now:

   ```sh
   chowki backup chowki-before-upgrade.db
   ```

   A backup never upgrades the database, so it keeps the current schema.

2. Put the new version in place of the old binary. If you installed Chowki with the
   [install script](../get-started/install.md#install-the-binary), run it again: it installs the
   latest release, or the version in `CHOWKI_VERSION`, over the old binary.
3. Run the new version's checks. When the database needs an upgrade, `chowki doctor` says so:

   ```text
   ok    database            data/chowki.db has schema version 8; chowki serve migrates it to 9 when it starts, so back it up first
   ```

4. Restart `chowki serve`. It stops taking requests, waits up to 30 seconds for those in flight,
   and exits; the new version upgrades the database when it starts, and logs it:

   ```text
   {"time":"2026-09-28T07:02:40.285711+05:00","level":"INFO","msg":"upgraded the database","path":"data/chowki.db","from_schema":8,"to_schema":9}
   ```

Clients get connection errors while the gateway restarts, for a second or two, so upgrade when a
failed request can be retried.

## Upgrade with Docker Compose

In the folder with `compose.yaml`, back up the database, then get the new image and start it:

```sh
(umask 077; docker compose exec -T chowki chowki backup - > chowki-before-upgrade.db)
docker compose pull
docker compose up -d
```

When `CHOWKI_IMAGE` in `.env` names a version, such as `ghcr.io/852hamza/chowki:0.1.0`, change it
to the new version before `docker compose pull`. `docker compose up -d` replaces the container
with one of the new image, which upgrades the database in the volume when it starts.
`docker compose logs chowki` shows the upgrade.

## Go back to the earlier version

Chowki can't downgrade a database: an older version refuses to start on a newer schema, with
`use a newer Chowki`. To go back, stop the gateway, put the earlier version back, restore the
backup from before the upgrade, and start it:

```sh
chowki restore chowki-before-upgrade.db
```

Requests and spend since the upgrade are lost with the rollback. See
[Back up and restore Chowki](backup-and-restore.md).

## Related

- [Install Chowki](../get-started/install.md) · [Check your setup](../how-to/check-your-setup.md)
