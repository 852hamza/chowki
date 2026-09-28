# Chowki

Chowki is an open-source, self-hosted AI gateway. Your apps, SDKs and coding agents send their LLM
traffic through one endpoint, and Chowki:

- **Saves tokens** with an exact response cache, automatic provider prompt caching, and model
  aliases with fallback.
- **Keeps data safe** with virtual keys instead of shared provider keys, and redacts secrets and
  personal data before a prompt leaves your network.
- **Controls spend** with a budget and rate limits for every key.
- **Reports honest numbers**: usage, cost and savings per key, project and model, each with its
  calculation method.

Chowki speaks the OpenAI, Anthropic and Google Gemini APIs and works with any OpenAI-compatible
provider, including local models in Ollama. Clients change only their base URL and key. Chowki is
a single Go binary that stores its data in SQLite.

*Chowki* (چوکی) means "checkpoint" in Urdu.

> **Status:** early development. Chowki relays OpenAI, Anthropic and Google Gemini requests with
> virtual keys, translates OpenAI-format requests for Anthropic and Gemini models, records their
> cost, enforces budgets and rate limits, caches repeated answers, marks repeated Anthropic prompts
> for caching, redacts secrets and personal data, falls back between providers, shows it all in a
> dashboard, and scans repositories for secrets. Don't use it in production before v0.1.0. Follow
> the [changelog](CHANGELOG.md) to see what has landed.

## Quickstart

Build Chowki from source with Go 1.27 or later, Git and GNU Make, then follow
[Send your first request](docs/get-started/quickstart.md) to create a virtual key and send a request
through the gateway:

```sh
git clone https://github.com/852hamza/chowki.git
cd chowki
make build
./bin/chowki version
```

## Documentation

- [Send your first request](docs/get-started/quickstart.md): set up Chowki in about ten minutes.
- [Install Chowki](docs/get-started/install.md): run it with Docker Compose or Docker, or build it
  from source.
- [Manage virtual keys](docs/how-to/manage-virtual-keys.md): create, list and revoke keys.
- [Store provider keys in Chowki](docs/how-to/manage-provider-keys.md): keep provider keys in the
  database, encrypted, instead of in environment variables.
- [Set monthly budgets](docs/how-to/set-budgets.md): limit what a key or a project spends each
  month.
- [Set rate limits](docs/how-to/set-rate-limits.md): limit a key's requests and tokens per minute.
- [Cache responses](docs/how-to/cache-responses.md): answer repeated requests from the exact
  cache.
- [Use Anthropic prompt caching](docs/how-to/anthropic-prompt-caching.md): let Chowki mark
  repeated prompt prefixes for Anthropic's cache.
- [Redact secrets and personal data](docs/how-to/redact-sensitive-data.md): mask, block or report
  keys and personal data in prompts.
- [Use Google Gemini](docs/how-to/use-google-gemini.md): send Gemini requests from the Google
  Gen AI SDKs through Chowki.
- [Call Anthropic and Gemini models with OpenAI SDKs](docs/how-to/call-any-model-with-openai-sdks.md):
  use any provider's models from OpenAI-format clients.
- [Connect OpenAI-compatible providers](docs/how-to/connect-openai-compatible-providers.md): add
  DeepSeek, xAI, Mistral, Groq, OpenRouter and others.
- [Use the dashboard](docs/how-to/use-the-dashboard.md): see spend, savings, redactions and
  budgets in a browser.
- [Report usage and cost](docs/how-to/report-usage.md): print a range of days in the terminal,
  by key, model or day.
- [Scan for secrets](docs/how-to/scan-for-secrets.md): find keys and passwords in repositories,
  locally and in GitHub Actions.
- [Connect Claude Code](docs/how-to/connect-claude-code.md),
  [the Codex CLI](docs/how-to/connect-codex.md) and
  [the Gemini CLI](docs/how-to/connect-gemini-cli.md): route coding agents through Chowki.
- [Connect the OpenAI, Anthropic and Gemini SDKs](docs/how-to/connect-sdks.md): point apps at
  Chowki with two environment variables.
- [Use local models with Ollama](docs/how-to/use-local-models-with-ollama.md): reach local models
  with virtual keys.
- [Check your setup](docs/how-to/check-your-setup.md): find what stops the gateway from starting
  or working, with `chowki doctor`.
- [API reference](docs/reference/api.md): endpoints, headers and error codes.
- [CLI reference](docs/reference/cli.md): every `chowki` command and its options.
- [Admin API reference](docs/reference/admin-api.md): reports, and managing keys and projects.
- [Configuration reference](docs/reference/configuration.md): every setting and its environment
  variable.
- [Route requests with aliases and fallback](docs/how-to/routing-and-fallback.md): give a list of
  models one name, and fall back when a provider fails.
- [Monitor Chowki](docs/operations/monitoring.md): health checks and Prometheus metrics.
- [Serve over HTTPS](docs/how-to/serve-over-https.md): serve TLS with your certificate, or behind a
  reverse proxy.
- [Measure the gateway's overhead](docs/operations/performance.md): run the load test on your
  hardware.
- [Back up and restore Chowki](docs/operations/backup-and-restore.md) and
  [upgrade it](docs/operations/upgrade.md): keep the database and the master key safe.
- [Architecture](docs/concepts/architecture.md): how a request flows through Chowki.
- [Security model](docs/concepts/security.md): what Chowki stores, encrypts and logs, and what it
  guards against.
- [Set up a development environment](docs/contributing/development-setup.md): build and test
  Chowki.
- [Code structure](docs/contributing/code-structure.md): where each part of the code lives.
- [Architecture decision records](docs/adr/): why Chowki is built the way it is.

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) to learn how to propose a
change, and follow the [code of conduct](CODE_OF_CONDUCT.md). To report a vulnerability, see
[SECURITY.md](SECURITY.md).

## License

Chowki is licensed under the [Apache License 2.0](LICENSE).
