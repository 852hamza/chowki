# Changelog

All notable changes to Chowki are recorded in this file. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Chowki follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The `chowki` command. `chowki version` prints the version, commit, build date and repository
  URL. The `serve`, `init`, `provider`, `key`, `usage`, `scan`, `setup` and `doctor` commands
  exist but aren't implemented yet.
- Project identity in `project.env`. `make sync` applies it to the whole repository, and
  `make sync-check` finds leftovers of an earlier identity and hard-coded URLs in Go code.
- Fake OpenAI-compatible, Anthropic and Gemini providers for tests, with JSON and streaming
  replies and configurable usage.
- Continuous integration: build, tests, race detector, golangci-lint, govulncheck, the project
  identity check, and a check of commit messages and DCO sign-offs.
- Developer guide pages: architecture, development setup and code structure.

[Unreleased]: https://github.com/852hamza/chowki/commits/main
