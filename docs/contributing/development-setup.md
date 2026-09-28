---
title: Set up a development environment
description: Install the tools, build Chowki from source, and run the same tests and checks as CI.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Build Chowki from source and run the tests and checks that CI runs on every pull request, so that
your change passes them the first time.

## Before you begin

- Go 1.27 or later. Check your version with `go version`.
- Git and GNU Make.
- golangci-lint v2 for `make lint`. CI uses v2.14.0. See
  [Install golangci-lint](https://golangci-lint.run/welcome/install/).
- govulncheck for `make vuln`. Install it with
  `go install golang.org/x/vuln/cmd/govulncheck@latest`.
- You don't need a provider account or key. The tests use fake providers and never call a real LLM
  API, so running them costs nothing.

## Build and test Chowki

1. Clone the repository and change into it:

   ```sh
   git clone https://github.com/852hamza/chowki.git
   cd chowki
   ```

2. Build the binary:

   ```sh
   make build
   ```

   Make writes the binary to `bin/chowki` and sets its version and repository URL at link time.

3. Run the tests:

   ```sh
   make test
   ```

   One test renames the project in a temporary copy of the repository, then builds and tests the
   copy, so it takes longer than the others. To skip it while you work, run
   `go test -short ./...`.

4. Run the tests with the race detector:

   ```sh
   make race
   ```

5. Run the linters, the vulnerability check and the project identity check:

   ```sh
   make lint vuln sync-check
   ```

   Each check prints what it found and fails if there is a problem. CI runs the same checks, and
   also checks each commit message and sign-off; see the
   [contributing guide](https://github.com/852hamza/chowki/blob/main/CONTRIBUTING.md).

6. If you changed a command's usage, a setting, a metric or an error code, rewrite the reference
   pages that are generated from the code:

   ```sh
   make docs
   ```

   `make test` fails while a generated part of `docs/reference` is out of date, and names the
   page.

## Verify

Run the binary that you built:

```sh
./bin/chowki version
```

Output:

```text
chowki 08c0c7f
commit: 08c0c7ffe62ca789f3fadfe3b089076845597558
date:   2026-09-27T01:33:08Z
go:     go1.27.1 linux/amd64
repo:   https://github.com/852hamza/chowki
```

Your version, commit and date match your checkout. Until the first release, the version is the
short commit hash, followed by `-dirty` when you have uncommitted changes.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `golangci-lint: command not found` or `govulncheck: command not found` | The tool isn't installed, or its folder isn't in your `PATH`. | Install the tool. Add the `bin` folder inside the path that `go env GOPATH` prints to your `PATH`. |
| `make sync-check` prints `project.env changed since the last sync` | `project.env` was edited without running `make sync`. | Run `make sync` and commit the files it changed. |
| `make sync-check` prints that a Go file `hard-codes` a value | Go code contains the project domain or a project URL. | Read the value from `internal/buildinfo` instead. |

## Next steps

- Find where your change belongs in [Code structure](code-structure.md).
- Read the [contributing guide](https://github.com/852hamza/chowki/blob/main/CONTRIBUTING.md) before
  you open a pull request.

## Related

- [Architecture](../concepts/architecture.md)
