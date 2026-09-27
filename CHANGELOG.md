# Changelog

All notable changes to Chowki are recorded in this file. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Chowki follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The `chowki` command. `chowki version` prints the version, commit, build date and repository
  URL. The `provider`, `usage`, `scan`, `setup` and `doctor` commands exist but aren't
  implemented yet.
- Project identity in `project.env`. `make sync` applies it to the whole repository, and
  `make sync-check` finds leftovers of an earlier identity and hard-coded URLs in Go code.
- Fake OpenAI-compatible, Anthropic and Gemini providers for tests, with JSON and streaming
  replies and configurable usage.
- Continuous integration: build, tests, race detector, golangci-lint, govulncheck, the project
  identity check, and a check of commit messages and DCO sign-offs.
- Developer guide pages: a quickstart, managing virtual keys, the configuration reference,
  architecture, development setup and code structure.
- `chowki init` creates `chowki.yaml`, a master key readable only by you, and the SQLite database.
  It never replaces an existing file.
- `chowki key create`, `list` and `revoke` manage virtual keys. A key is shown once; Chowki stores
  only its prefix and SHA-256 hash, and every change goes to the audit log.
- `chowki serve` runs the gateway. It relays OpenAI chat completions (`/v1/chat/completions`) and
  Anthropic messages (`/anthropic/v1/messages`), streaming and non-streaming, authenticated with
  virtual keys. It routes `provider/model` names, records each request's tokens and cost in the
  database, and adds the headers `x-chowki-request-id` and `x-chowki-cost-usd`.
- A model catalog, `catalog/models.json`, with the official prices of current OpenAI and Anthropic
  models, including cache writes, 1-hour cache writes and long-context prices.
- Monthly budgets in US dollars for virtual keys (`chowki key create --budget-usd`,
  `chowki key update`) and for projects (`chowki project update`). Once a budget is used up, its
  requests fail with HTTP 429 and the code `budget_exceeded` until the next calendar month in UTC.
  Requests to models without a price count as $0. Spend is saved with the request records and
  survives restarts; `chowki key list` and the new `chowki project list` show this month's spend.
- Rate limits per virtual key: requests per minute (`--rpm`) and input and output tokens per minute
  (`--tpm`) on `chowki key create` and `chowki key update`. A key over a limit gets HTTP 429 with
  the code `rate_limit_exceeded`, and the headers `Retry-After` and `retry-after-ms` say when to
  retry. Limits are kept in memory. `chowki key update` now prints all of a key's limits.
- An exact response cache for non-streaming requests, encrypted with AES-256-GCM. Turn it on per
  key with `--cache exact`, for every key with `defaults.cache`, or per request with the header
  `x-chowki-cache: on`. The response header `x-chowki-cache` shows `hit`, `miss` or `bypass`; a hit
  costs $0 and counts the original cost as savings. Responses expire after `defaults.cache_ttl`,
  and `storage.cache_max_mb` limits the cache's size.
- An Anthropic prompt-cache optimizer. When the same tools and system prompt repeat within five
  minutes and are long enough to cache, Chowki adds one `cache_control` breakpoint at the end of
  the system prompt, or of the tools. It never changes requests that set `cache_control`
  themselves. `defaults.prompt_cache: off` turns it off. The catalog now lists the minimum
  cacheable prompt of each Anthropic model.
- Redaction of secrets and personal data in the text of requests: private keys, AWS, GitHub,
  Slack, OpenAI, Anthropic, Google and Stripe keys, JWTs, Chowki keys, `password = …` pairs, email
  addresses, card numbers, IBANs, Pakistani CNIC and mobile numbers, and international phone
  numbers. Modes `mask` (the default), `block`, `alert` and `off`, per key with `--redaction` or for
  all keys with `defaults.redaction`. Placeholders are deterministic, such as
  `[REDACTED:email:b443e741]`; the `x-chowki-redactions` header, logs and records count findings by
  type and never hold the values.
- Health checks and metrics: `GET /healthz` (liveness), `GET /readyz` (the database answers) and
  `GET /metrics` in the Prometheus text format, with requests, tokens, cost, savings, redactions,
  and histograms of provider latency and the gateway's own overhead.
- Aliases and fallback: `aliases` in `chowki.yaml` gives a list of `<provider>/<model>` targets one
  name. On a 429, a 5xx, a connection error or a timeout, and only before the first byte reaches
  the client, Chowki tries the next target, at most two more. A target that fails three times in a
  row is skipped for 30 seconds.
- Model allowlists per virtual key: `chowki key create|update --models "fast,openai/*"`. Other
  models get 403 `model_not_allowed`. The key settings now show the models.
- More endpoints: `POST /v1/embeddings` (with redaction of the input and the exact cache),
  `GET /v1/models` (the aliases and catalog models that a key may use), and
  `POST /anthropic/v1/messages/count_tokens` (free: no budget or token limit).
- An API reference with the endpoints, Chowki's headers and every error code.

[Unreleased]: https://github.com/852hamza/chowki/commits/main
