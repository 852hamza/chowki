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

> **Status:** early development. Chowki relays OpenAI and Anthropic requests with virtual keys,
> records their cost, enforces budgets and rate limits, and caches repeated answers, but redaction
> and prompt-cache optimization aren't built yet, so don't use it in production. Follow the
> [changelog](CHANGELOG.md) to see what has landed.

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
- [Manage virtual keys](docs/how-to/manage-virtual-keys.md): create, list and revoke keys.
- [Set monthly budgets](docs/how-to/set-budgets.md): limit what a key or a project spends each
  month.
- [Set rate limits](docs/how-to/set-rate-limits.md): limit a key's requests and tokens per minute.
- [Cache responses](docs/how-to/cache-responses.md): answer repeated requests from the exact
  cache.
- [Configuration reference](docs/reference/configuration.md): every setting and its environment
  variable.
- [Architecture](docs/concepts/architecture.md): how a request flows through Chowki.
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
