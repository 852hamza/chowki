# Chowki documentation

This folder is the source of the Chowki developer guide. Pages are written together with the code: a feature is not done until its page is.

## How the guide is organized
| Section | Page type | Answers |
|---|---|---|
| `overview/` | Concept | What is Chowki? How does it work? Which edition do I need? |
| `get-started/` | Tutorial | How do I install Chowki and send my first request in five minutes? |
| `tutorials/` | Tutorial | End-to-end scenarios: cut coding-agent costs, secure team access to AI, run local models |
| `how-to/` | How-to | How do I do one specific task, such as adding a provider, setting a budget or enabling caching? |
| `concepts/` | Concept | Why does it work this way? Architecture, request pipeline, security model, savings method |
| `reference/` | Reference | Exact details: CLI, configuration, API, metrics, error codes, limits — mostly generated from code |
| `operations/` | How-to | Running in production: monitoring, upgrades, backup, performance, troubleshooting |
| `security/` | Concept, how-to | Security overview, hardening checklist, reporting a vulnerability |
| `release-notes/` | Reference | What changed in each version |
| `contributing/` | How-to | Development setup, code structure, testing, adding a provider, writing docs |
| `adr/` | Record | Architecture decision records |

A folder appears when its first page ships. We don't publish placeholder pages.

## Writing a page
1. Pick the page type, then copy its template from [_templates/](_templates/): concept, how-to, tutorial, reference or troubleshooting.
2. Follow the [style guide](STYLE_GUIDE.md).
3. Run every command and code sample against the current version before you open the pull request.

Reference pages marked "generated" are produced from the code. Don't edit them by hand; change the code or its doc comments instead.

## Editions
This guide covers Chowki Community, the open-source edition. Chowki Enterprise has its own guide. Community pages mention an Enterprise feature only with an **Enterprise** badge and a link.

## Publishing
The site is built with Docusaurus (MIT license), versioned per minor release, searchable offline, and published to GitHub Pages. Its source is in `website/`; `npm ci && npm run start` there serves it locally, and `npm run build` checks every link.
