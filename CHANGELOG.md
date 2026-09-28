# Changelog

All notable changes to Chowki are recorded in this file. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Chowki follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The `chowki` command. `chowki version` prints the version, commit, build date and repository
  URL.
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
  models get 403 `model_not_allowed`, before redaction scans the request. The key settings now
  show the models.
- More endpoints: `POST /v1/embeddings` (with redaction of the input and the exact cache),
  `GET /v1/models` (the aliases and catalog models that a key may use), and
  `POST /anthropic/v1/messages/count_tokens` (free: no budget or token limit).
- An API reference with the endpoints, Chowki's headers and every error code.
- An admin JSON API under `/admin/v1/`: a summary, breakdowns by key, model or day, request
  records, and managing keys and projects. It takes admin tokens (`chowki_admin_…`), which
  `chowki admin create|list|revoke` manage; redaction finds them too. Reports count whole days in
  UTC, from sums that Chowki keeps for each day, so they stay fast as requests pile up and outlive
  the retention of request records. The summary counts unpriced requests.
- A dashboard at `/ui/`, for admin tokens: spend, requests, tokens, failures, net savings, the
  exact-cache hit rate and redactions over a range of days, a chart by day with a table view, the
  top keys and models, and the state of every budget. It needs no external scripts, fonts or
  styles, and it has a dark mode.
- Google Gemini: the Gemini API under `/gemini`, for the Google Gen AI SDKs:
  `generateContent`, `streamGenerateContent` (with `alt=sse`), `countTokens` (free),
  `embedContent` and `batchEmbedContents`, with errors in Google's format. `chowki init` adds a
  `gemini` provider, which takes its key from `GEMINI_API_KEY`.
- Gemini prices in the model catalog, from Gemini 2.5 to Gemini 3.8 Flash: thinking tokens cost
  the output price, cached tokens the context-caching price, and prompts above 200,000 tokens the
  long-context price. Catalog entries can list announced price changes by date. Requests with audio
  input, built-in tools or the Flex and Priority tiers stay unpriced.
- `free_tier: true` for a provider whose requests aren't billed, such as on the Gemini API's free
  tier: their cost is $0.
- Translation: `/v1/chat/completions` serves Anthropic and Gemini models too. Chowki translates
  messages, images, tools and tool calls, streams, finish reasons, usage and errors, and answers
  `400 unsupported_option`, naming the option, for an option that the provider can't honor. It keeps
  Anthropic's thinking and Gemini's thought signatures for an hour, by tool call, to send them back
  in the next request of a tool-use loop. Aliases can mix providers of different APIs, and
  `GET /v1/models` lists their models.
- The model catalog lists each model's context window, maximum output and capabilities: tools,
  vision, JSON output, caching and thinking.
- `POST /v1/responses` relays OpenAI's Responses API to OpenAI-compatible providers, with usage,
  cost, budgets, the exact cache and redaction, for clients such as the Codex CLI.
- `chowki scan` finds secrets in a repository, `.env` files and MCP configurations: keys of AI
  providers and clouds, private keys, tokens, and passwords assigned to settings, and with
  `--pii`, personal data. It never prints the secrets. Reports come as text, JSON or SARIF 2.1.0,
  and it exits with 1 when it finds any. `action.yml` runs it in GitHub Actions and uploads the
  findings to code scanning.
- A guide to DeepSeek, xAI, Mistral, Groq and OpenRouter as providers, with their base URLs.
  Usage from xAI, whose completion tokens exclude reasoning, now counts the reasoning as output,
  and Groq's usage in `x_groq` counts in streams.
- `chowki setup <TOOL>` prints the settings that point Claude Code, the Codex CLI, the Gemini CLI,
  the OpenAI, Anthropic and Google Gen AI SDKs, or Ollama at the gateway, with guides for each.
- `chowki doctor` checks the `.env` file, the configuration, the master key, the providers' keys
  and prices, the catalog, the database, the keys and the listen address, and says what to fix. It
  changes nothing, and exits with 1 when a check fails.
- A container image, built from `deploy/Dockerfile`: a static binary on a distroless base, about
  18 MB, that runs as a non-root user and keeps its data in `/var/lib/chowki`. `deploy/compose.yaml`
  runs it with Docker Compose, and `make docker` builds it. `CHOWKI_CONFIG` sets the configuration
  file that commands read without `--config`.
- `chowki usage` prints the requests, tokens, cost, savings, cache hits and redactions of a range
  of days, in total and by key, model or day.
- `chowki provider list` shows where each provider's key comes from, and `chowki provider set-key`
  stores a key in the database, sealed with AES-256-GCM under the master key and bound to its
  provider, for when the environment has none. It reads keys only from standard input.
  `chowki serve` uses stored keys, and `chowki doctor` checks that they open.
- A CLI reference, and summaries in the configuration, metrics and API references, generated from
  the code; `make docs` rewrites them, and the tests fail while they're out of date. Every command
  prints its usage with `--help`.
- `server.tls_cert_file` and `server.tls_key_file` make the gateway serve HTTPS, with TLS 1.2 or
  later and HTTP/2, and load a renewed certificate without a restart. `server.read_timeout`, 60
  seconds by default, limits how long a client may take to send a request; responses may stream
  for longer. `chowki doctor` checks the certificate and warns 14 days before it expires.
- `chowki backup` writes a consistent copy of the database while the gateway runs, and
  `chowki restore` puts one back after checking it, and keeps the database it replaces. With `-`,
  they stream through stdout and stdin, as `docker compose exec -T` needs. `chowki serve` logs
  when it upgrades the database's schema.
- Releases: a `v*` tag builds archives for Linux, macOS and Windows on amd64 and arm64, with the
  licenses of every module in them, checksums and SPDX SBOMs, and container images for
  `linux/amd64` and `linux/arm64`, all with signed build provenance, as a draft release.
  `tools/licenses` fails the release, and CI, on a module under a license that the binary may not
  include.
- A documentation site, built from `docs/` with Docusaurus in `website/`, with local search. Its
  build fails on a broken link or anchor, and GitHub Pages publishes it from `main` once the
  repository is public.
- `install.sh` installs the latest release, or `CHOWKI_VERSION`, on Linux and macOS with one
  command, after checking the download against the release's checksums. The quickstart starts with
  it, so it no longer needs Go.
- `make loadtest` measures the gateway's overhead at 200 requests per second on 2 CPUs, against a
  fake provider, and fails when the p99 is over 25 ms or a gateway built with `-race` reports a
  race.

[Unreleased]: https://github.com/852hamza/chowki/commits/main
