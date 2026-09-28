---
title: Admin API reference
description: The JSON API for reports on spend, savings and requests, and for managing virtual keys and projects.
type: reference
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

The admin API reports what flows through Chowki and manages its keys and projects, for scripts.
It's served under `/admin/v1/` on the gateway's address. To see the same reports in a browser, see
[Use the dashboard](../how-to/use-the-dashboard.md).

## Authentication

Every request needs an admin token in the `Authorization: Bearer` header. Create one with the CLI:

```sh
chowki admin create --name ops
```

Output:

```text
Created admin token "ops":

  chowki_admin_EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE

Copy it now. Chowki stores only a hash of it and can't show it again.
It can read and change every key and project; keep it as safe as the master key.
```

`chowki admin list` lists the tokens by prefix, and `chowki admin revoke <PREFIX>` revokes one.
Virtual keys don't work here, and admin tokens don't work on the API endpoints.

> **Warning:** An admin token can create keys and read every request's metadata. Keep it in a
> secret store, and don't expose the gateway to the internet without TLS in front of it.

The examples use `curl` with the token in `$ADMIN_TOKEN`:

```sh
curl -sS http://localhost:8080/admin/v1/summary -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Time ranges

Reports count whole days in UTC. They take `from` and `to` query parameters: dates such as
`2026-09-01`, where `to` includes its day. They default to the current month in UTC, from its first
day to today. Chowki keeps the sums of each day when it deletes request records older than
`retention_days`, so reports reach further back than `/admin/v1/requests`.

## Reports

### `GET /admin/v1/summary`

Totals over a range of days: requests, errors (status 400 or more), unpriced requests (usage
but no price in the catalog, counted as $0), cost, net savings by method, tokens, exact-cache hits
and misses, and redactions by type.

```json
{"cache":{"hit_rate":0,"hits":0,"misses":0},"cost_usd":2,"errors":0,"from":"2026-09-01","redactions":{},"requests":2,"savings_usd":{},"to":"2026-09-27","tokens":{"cache_read":0,"cache_write":0,"input":500000,"output":100000,"reasoning":0},"unpriced":0}
```

### `GET /admin/v1/breakdown?by=<GROUP>`

The same sums, grouped by `key` (the default, with each key's prefix and name), `model`, or `day`
(in UTC). Keys and models come with the highest cost first; days in order.

```json
{"by":"model","from":"2026-09-01","groups":[{"id":"openai/gpt-6-sol","requests":2,"cost_usd":2,"savings_usd":0,"input_tokens":500000,"output_tokens":100000}],"to":"2026-09-27"}
```

### `GET /admin/v1/requests?limit=<N>&before=<TIME>`

Request records, newest first: metadata only, never prompts or answers. `limit` is 1 to 1000,
100 by default; `before` pages back from a time.

```json
{"requests":[{"id":"req_88e1aff49cd7f419b25fc197","time":"2026-09-27T10:23:05.132Z","key_id":1,"project_id":1,"family":"openai","endpoint":"/v1/chat/completions","provider":"openai","model":"gpt-6-sol","stream":false,"status":200,"error":"","latency_ms":5,"ttfb_ms":5,"tokens":{"input":250000,"output":50000,"cache_read":0,"cache_write":0,"reasoning":0},"cost_usd":1,"savings_usd":0,"savings_method":"","cache":"bypass","redactions":{}}]}
```

## Keys

A key in a response shows its settings and `spent_usd`, what it spent this month in UTC; never
the key itself, except once when it's created. `cache` and `redaction` are `default` when the key
follows `chowki.yaml`, and an empty `models` list allows every model.

### `GET /admin/v1/keys`

```json
{"keys":[{"prefix":"chowki_RHEEH","name":"alice","project":"team-a","created_at":"2026-09-27T10:23:03.542Z","revoked_at":null,"budget_usd":50,"project_budget_usd":0,"rpm":0,"tpm":0,"cache":"default","redaction":"default","models":[],"spent_usd":2}]}
```

### `POST /admin/v1/keys`

Creates a key. `name` is required, and `project` defaults to `default`. The other fields are the
key's settings: `budget_usd`, `rpm`, `tpm`, `cache`, `redaction` and `models`, as for
`chowki key create`.

```sh
curl -sS http://localhost:8080/admin/v1/keys -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "ci-bot", "project": "team-a", "rpm": 60, "models": ["openai/*"]}'
```

The answer, with status 201, is the only one that holds the key:

```json
{"key":"chowki_EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE","prefix":"chowki_yPCIx","name":"ci-bot","project":"team-a","created_at":"2026-09-27T10:23:06.728Z","revoked_at":null,"budget_usd":0,"project_budget_usd":0,"rpm":60,"tpm":0,"cache":"default","redaction":"default","models":["openai/*"],"spent_usd":0}
```

### `PATCH /admin/v1/keys/<PREFIX>`

Changes the settings in the body and keeps the others; `0` removes a limit, `default` makes a
mode follow `chowki.yaml`, and `[]` allows every model. For example,
`{"budget_usd": 25}` answers:

```json
{"prefix":"chowki_yPCIx","name":"ci-bot","project":"team-a","created_at":"2026-09-27T10:23:06.728Z","revoked_at":null,"budget_usd":25,"project_budget_usd":0,"rpm":60,"tpm":0,"cache":"default","redaction":"default","models":["openai/*"],"spent_usd":0}
```

### `DELETE /admin/v1/keys/<PREFIX>`

Revokes the key, for good; revoking it again changes nothing. The answer is the key with its
`revoked_at`.

## Projects

### `GET /admin/v1/projects`

```json
{"projects":[{"name":"team-a","budget_usd":200,"spent_usd":2,"active_keys":1}]}
```

### `PATCH /admin/v1/projects/<NAME>`

Sets the project's monthly budget, `{"budget_usd": 200}`; `0` removes it. The answer is the
project, as in the list.

## Errors

Errors have a status and a JSON body with a stable code:

```json
{"error":{"code":"missing_admin_token","message":"Send an admin token in the Authorization: Bearer header; chowki admin create makes one."}}
```

| Code | Status | Meaning |
|---|---|---|
| `missing_admin_token` | 401 | The request has no admin token. |
| `invalid_admin_token` | 401 | The admin token is malformed, unknown or revoked. |
| `invalid_request` | 400 | A parameter or the body is invalid; the message says which. |
| `key_not_found` | 404 | No key has the prefix. |
| `project_not_found` | 404 | No project has the name. |
| `not_found` | 404 | The path isn't an admin endpoint. |
| `internal_error` | 500 | Chowki failed; its log has the details. |

Chowki records every change in its audit log, with the admin token's prefix as the actor.

## Related

- [API reference](api.md) · [Manage virtual keys](../how-to/manage-virtual-keys.md) ·
  [Set monthly budgets](../how-to/set-budgets.md)
