# Contributing to Chowki

Thank you for helping to improve Chowki. This page explains how to propose a change and what every
change needs before it can merge.

## Before you start

- To report a bug, open an [issue](https://github.com/852hamza/chowki/issues) with the steps to
  reproduce it and the output of `chowki version`.
- For a new feature or a larger change, open an issue first, so that we can agree on the approach
  before you write code.
- Report security problems privately, never in a public issue. See [SECURITY.md](SECURITY.md).

To set up your computer, follow
[Set up a development environment](docs/contributing/development-setup.md).
To find where a change belongs, read [Code structure](docs/contributing/code-structure.md).

## Rules for code

- Use the Go standard library first. A new dependency must have a free, permissive license (MIT,
  BSD, Apache-2.0, ISC or MPL-2.0) and needs an [architecture decision record](docs/adr/) that
  explains why the standard library isn't enough.
- Tests never call real LLM providers. Use the fake providers in `internal/testutil`.
- Never log or store prompt or response bodies, API keys or authorization headers. Log metadata
  only.
- Don't hard-code the project domain or repository URLs in Go code. Read them from
  `internal/buildinfo`. `make sync-check` fails otherwise.
- Pass `context.Context` through the request path, and give every upstream call a timeout.
- Write table-driven tests with the standard library (`testing` and `net/http/httptest`).

## Commit messages

Every commit subject follows [Conventional Commits](https://www.conventionalcommits.org/):
`type(scope): description`.

- `type` is one of `feat`, `fix`, `docs`, `refactor`, `perf`, `test`, `build`, `ci`, `chore` or
  `revert`. Add `!` after the type or scope for a breaking change.
- `scope` is optional, in lowercase, and names the area, such as a package.
- The description is at most 72 characters and starts after one space.

Examples:

```text
feat(auth): add virtual key revocation
fix(sse): relay chunks without buffering
docs(guide): add the budgets how-to
```

## Sign off your commits

Chowki uses the [Developer Certificate of Origin](https://developercertificate.org/) (DCO) instead
of a contributor license agreement. When you sign off a commit, you certify that you wrote the
change or otherwise have the right to submit it under the project's license.

Sign off each commit with `-s`:

```sh
git commit -s -m "fix(sse): relay chunks without buffering"
```

Git adds a line with the name and email from your Git configuration:

```text
Signed-off-by: Your Name <you@example.com>
```

The email in the sign-off must match the commit's author email. To sign off commits that you
already made on your branch, run `git rebase --signoff main`.

## Pull requests

CI checks every pull request: build, tests with the race detector, lint, known vulnerabilities, the
project identity, and each commit's subject and sign-off. Before you open a pull request:

1. Run `make build test lint sync-check`.
2. Update the developer guide pages for any user-visible change, following the
   [style guide](docs/STYLE_GUIDE.md).
3. Add a line to the "Unreleased" section of [CHANGELOG.md](CHANGELOG.md).

## Code of conduct

Everyone who takes part in the project follows the [code of conduct](CODE_OF_CONDUCT.md).

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
