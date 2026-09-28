# Chowki documentation

This folder is the source of the Chowki guide, published at https://852hamza.github.io/chowki.
Pages are written with the code: a feature isn't done until its page is.

## How the guide is organized

| Folder | Page type | Answers |
|---|---|---|
| `get-started/` | Tutorial | How do I install Chowki and send my first request? |
| `how-to/` | How-to | How do I do one task, such as adding a provider, setting a budget or connecting a tool? |
| `concepts/` | Concept | Why does it work this way? Architecture, security model, savings method |
| `reference/` | Reference | Exact details: the API, CLI, admin API, configuration and metrics, partly generated from code |
| `operations/` | How-to | Running Chowki: monitoring, backups, upgrades and performance |
| `contributing/` | How-to | Development setup, code structure and releasing |
| `adr/` | Record | Architecture decision records |

## Writing a page

1. Pick the page type, and copy its template from [_templates/](_templates/).
2. Follow the [style guide](STYLE_GUIDE.md).
3. Run every command and code sample against the current code before you open the pull request,
   and show the output that it printed.

The parts of reference pages between `<!-- generated:NAME -->` and `<!-- end generated:NAME -->`
come from the code. Don't edit them by hand: change the code, then run `make docs`.

## Publishing

The site is built with Docusaurus from this folder; its source is in `website/`. There,
`npm ci && npm run start` serves it locally, and `npm run build` checks every link. The Docs
workflow publishes it to GitHub Pages from `main`.
