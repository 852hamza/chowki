---
title: Release Chowki
description: How a maintainer tags a version, and what the release workflow builds, signs and publishes.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

A release starts from a `v*` tag on `main`. The Release workflow builds it in GitHub Actions, as a
draft that a maintainer reviews and publishes:

- archives of the binary for Linux, macOS and Windows, on amd64 and arm64, each with the licenses
  of every module in the binary;
- `checksums.txt`, with the SHA-256 of every archive and SBOM;
- an SPDX SBOM of each archive;
- container images for `linux/amd64` and `linux/arm64` at `ghcr.io/852hamza/chowki`, tagged with
  the version and, unless it's a prerelease, `latest`, with their own SBOMs;
- signed build provenance for the archives and the images, which anyone can verify.

## Before you begin

- Maintainer access to the repository.
- CI passes on the commit of `main` to release.

## Try the release on your computer

GoReleaser and Syft build the archives the way the workflow does, without publishing anything:

```sh
goreleaser release --snapshot --clean
make docker
```

The archives, their checksums and SBOMs land in `dist/`.

## Release a version

1. In `CHANGELOG.md`, rename the `## [Unreleased]` section to the version and today's date, such
   as `## [0.1.0] - 2026-10-01`. Add an empty `## [Unreleased]` above it, and update the link
   definitions at the end of the file. The workflow uses the version's section as the release
   notes, followed by instructions to install and verify that version, and fails without a
   section.
2. Commit the change on `main`, and push it.
3. Tag the commit, and push the tag:

   ```sh
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

   A tag with a hyphen, such as `v0.1.0-rc.1`, makes a prerelease, which doesn't move the
   `latest` image.

4. Wait for the Release workflow. On the Releases page, open the draft release that it made with
   its pencil icon, check its notes and its files, and publish it as the latest release, not as a
   pre-release: the install script installs the latest release. Don't create a release with
   **Draft a new release**, which has none of the files. When a job of the workflow fails, re-run
   it; a re-run replaces the draft.
5. The first time, GitHub may create the image's package as private: make it public in the
   package's settings, so that `docker pull` works without signing in.
6. For the first release, publish the launch post: in `website/blog`, remove `draft: true` from the
   post, and add the Blog link to the navbar in `website/docusaurus.config.js`.

## Verify a release

Anyone can check that an archive is the one that the workflow built, with the GitHub CLI:

```sh
gh attestation verify chowki_0.1.0_linux_amd64.tar.gz --repo 852hamza/chowki
sha256sum --check --ignore-missing checksums.txt
```

For an image, name it instead of the file:

```sh
gh attestation verify oci://ghcr.io/852hamza/chowki:0.1.0 --repo 852hamza/chowki
```

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| The workflow fails with `CHANGELOG.md has no section for <version>` | The changelog has no section named after the tag. | Add the section, move the tag to the new commit, and push the tag again. |
| The Container images job fails with `denied: permission_denied: write_package` | The image's package belongs to another repository, such as a deleted one with the same name, so the workflow may not write to it. | In the package's settings, give this repository's Actions write access, or delete the package if nothing uses it. Then re-run the failed job. |
| GoReleaser fails in the `go run ./tools/licenses` hook | A module has no license file, or a license that the binary may not include. | Replace the dependency; see the dependency rules in `CONTRIBUTING.md`. |

## Related

- [Set up a development environment](development-setup.md) ·
  [Install Chowki](../get-started/install.md)
