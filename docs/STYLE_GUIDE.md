# Documentation style guide

Our bar is the best cloud-provider documentation: every page helps a reader finish a real task, is correct for the version they run, and works by copy and paste.

## 1. Principles
1. **Task first.** Start from what the reader wants to do, not from how the code is organized.
2. **One page, one purpose.** Each page has exactly one type: tutorial, how-to, concept, reference or troubleshooting. Don't mix them.
3. **Correct and tested.** Every command and code sample is run before merge. Output shown on a page is real output.
4. **Complete.** State prerequisites, permissions, limits, cost impact and security impact wherever they matter.
5. **Current.** A change in behavior updates its pages in the same pull request.

## 2. Voice and language
- Address the reader as "you". Use the present tense and the active voice: "Chowki stores the key", not "The key will be stored".
- Keep sentences short, one idea each. Prefer plain words to jargon; define a term the first time you use it.
- Use American English spelling and sentence-case titles: "Set a budget for a key".
- Be direct: "Run", "Open", "Select". Avoid "simply", "just", "easy" and "obviously".
- No marketing language in the guide.

## 3. Standard terms
| Use | Don't use |
|---|---|
| virtual key | token, API token (when you mean a Chowki key) |
| provider key | real key, master key |
| provider | vendor, backend |
| alias | model group |
| project | team (until teams exist) |
| Chowki Community, Chowki Enterprise | free version, paid version |

Write product names the way their owners do: OpenAI, Anthropic, Claude Code, Google Gemini, Ollama, GitHub Actions.

## 4. Page anatomy
Every page starts with front matter:
```yaml
---
title: Set a budget for a key
description: Limit how much a virtual key can spend each month.   # one sentence; used by search
type: how-to            # tutorial | how-to | concept | reference | troubleshooting
since: v0.1             # first version with this behavior
edition: community      # community | enterprise
last_reviewed: 2026-09-27
---
```
Then these parts, in this order (skip the ones that don't apply):
1. **Introduction** — one or two sentences: what the reader will achieve and why it matters.
2. **Before you begin** — prerequisites as a list: versions, permissions, keys, cost impact.
3. **Steps** — numbered, one action per step, each followed by its result.
4. **Verify** — how to confirm it worked, with the expected output.
5. **Troubleshooting** — the two or three most likely failures.
6. **Next steps** and **Related** — links.

## 5. Procedures
- Number the steps and start each one with a verb. Name the UI element or command first: "In **Keys**, select **Create key**."
- After an important step, show what the reader sees.
- When a step differs by platform, use tabs (Linux, macOS, Windows; Docker, binary) instead of "if you use…" paragraphs.
- Keep a procedure to about nine steps; split longer ones.

## 6. Code samples
- Complete and runnable: include imports, variables and the command that runs the sample.
- Placeholders use `<UPPER_SNAKE_CASE>` and are explained right after the block: "Replace `<VIRTUAL_KEY>` with the key from step 2."
- Never include a real key, token or secret. Use `chowki_EXAMPLE…` and `sk-EXAMPLE…`.
- Write commands without a leading `$` so they copy cleanly. Show output in a separate block labeled "Output".
- When a page is about calling the API, give the same example for curl and the main SDKs (Python, TypeScript, Go) in tabs.
- Keep lines under 100 characters.

## 7. Callouts
- **Note** — useful, not required.
- **Tip** — a better or faster way.
- **Important** — required to succeed.
- **Warning** — risk of data loss, security exposure or unexpected cost.

Use at most two callouts per section.

## 8. Reference pages
- Generate them from code wherever possible: CLI, configuration, metrics, error codes, API. Generated sections sit between markers and are never edited by hand.
- Every option lists its name, type, default, allowed values, the version that introduced it, and an example.

## 9. Versions and editions
- Mark new behavior with `since`. When behavior changes, say which version changed it.
- For a deprecation, say what replaces it and when it will be removed.
- An Enterprise-only feature gets one sentence in Community pages, an **Enterprise** badge and a link to the Enterprise guide — no Enterprise instructions.

## 10. Diagrams, accessibility and links
- Prefer Mermaid diagrams (text) over screenshots. Every image has alt text, and every diagram has a short text explanation.
- Link text says where it goes: "See [Configure caching](…)", never "click here".
- Don't rely on color alone to convey meaning.

## 11. Checklist before merging a docs change
- [ ] The page type is clear and matches its template.
- [ ] Every command and sample was run against the current version.
- [ ] No secrets, internal URLs or business information.
- [ ] Front matter is complete and `last_reviewed` is updated.
- [ ] All links work (the CI link check passes).
