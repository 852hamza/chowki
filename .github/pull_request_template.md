<!--
Thank you for contributing. For a larger change, open an issue first, so that we can agree on the
approach. The contributing guide explains each item below:
https://github.com/852hamza/chowki/blob/main/CONTRIBUTING.md
-->

## What this changes and why

<!-- Link the issue that it fixes, such as "Fixes #123". -->

## Checklist

- [ ] Each commit subject is a Conventional Commit, and each commit is signed off (`git commit -s`).
- [ ] `make build test lint sync-check` passes.
- [ ] Tests cover the change, and no test calls a real LLM provider.
- [ ] The developer guide covers any user-visible change, and `CHANGELOG.md` has a line for it
      under "Unreleased".
- [ ] Nothing logs or stores prompt or response bodies, keys or authorization headers.
- [ ] A new dependency has a permissive license and an ADR in `docs/adr/`.
