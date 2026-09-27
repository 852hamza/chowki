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

> **Status:** early development. Chowki doesn't relay requests yet, so it isn't ready for use.
> Follow the [changelog](CHANGELOG.md) to see what has landed.

## Quickstart

A five-minute quickstart comes with the first release. Until then, you can build Chowki from
source with Go 1.27 or later, Git and GNU Make:

```sh
git clone https://github.com/852hamza/chowki.git
cd chowki
make build
./bin/chowki version
```

## Documentation

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
