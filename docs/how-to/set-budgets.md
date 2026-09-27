---
title: Set monthly budgets
description: Limit how much a virtual key or a project can spend each month, and see what each has spent.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Give a virtual key, or a whole project, a monthly budget in US dollars. When a budget is used up,
Chowki rejects the requests it covers until the next month starts, so an agent stuck in a loop
can't run up a surprise bill.

## Before you begin

- A Chowki folder set up with `chowki init`. See
  [Send your first request](../get-started/quickstart.md).
- Run the commands in that folder, or pass `--config <FILE>` with the path of its `chowki.yaml`.
- The commands change the database directly. The gateway can keep running: a new budget applies
  to the next request.

## How Chowki counts spend

- A budget covers one calendar month in UTC. Spend starts again from $0 at 00:00 UTC on the first
  day of each month.
- The spend of a request is its cost: the token usage that the provider reports, at the prices in
  Chowki's model catalog.
- A request to a model that has no price in the catalog, such as a local model in Ollama, counts
  as $0.
- A key's budget and its project's budget both apply. Whichever is used up first stops the key's
  requests.
- Chowki checks the budgets before each request, and counts the requests in progress at their
  estimated input cost. A request's actual cost is known only when its response ends, so spend
  can end up a little above the budget.
- Spend survives restarts: Chowki saves it in the database about once a second and when it stops.

## Set a budget for a key

1. Create a key with a budget:

   ```sh
   chowki key create --name alice --project team-a --budget-usd 50
   ```

   Output:

   ```text
   Created virtual key "alice" in project "team-a":

     chowki_EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE

   Copy it now. Chowki stores only a hash of it and can't show it again.

   Limits:
     Monthly budget:       $50.00
     Requests per minute:  none
     Tokens per minute:    none
   ```

2. To change the budget of an existing key, pass its prefix, as `chowki key list` shows it:

   ```sh
   chowki key update --budget-usd 100 chowki_IRXOs
   ```

   Output:

   ```text
   Updated the limits of virtual key chowki_IRXOs ("alice"):
     Monthly budget:       $100.00
     Requests per minute:  none
     Tokens per minute:    none
   ```

To remove a budget, set it to `0`. Chowki records every budget change in its audit log. To limit
requests and tokens per minute too, see [Set rate limits](set-rate-limits.md).

## Set a budget for a project

A project budget limits the total spend of all the project's keys, in addition to each key's own
budget. Chowki creates a project with its first key.

1. Set the budget by the project's name:

   ```sh
   chowki project update --budget-usd 200 team-a
   ```

   Output:

   ```text
   Project "team-a" now has a monthly budget of $200.00, shared by its keys.
   ```

## Check spend

List the keys with their spend this month:

```sh
chowki key list
```

Output:

```text
PREFIX        NAME    PROJECT  SPENT 2026-09  BUDGET  RPM   TPM     CREATED               STATUS
chowki_IRXOs  alice   team-a   $3.00          $50.00  none  none    2026-09-27 06:04 UTC  active
chowki_z5J3e  ci-bot  team-a   $1.00          none    60    100000  2026-09-27 06:04 UTC  active
```

List the projects with the spend of their keys:

```sh
chowki project list
```

Output:

```text
NAME    ACTIVE KEYS  SPENT 2026-09  BUDGET
team-a  2            $4.00          $200.00
```

An amount below one cent shows as `<$0.01`.

## Verify

When a budget is used up, requests fail with HTTP status 429 in the error format of the API that
the client speaks. For example, after `chowki key update --budget-usd 2 chowki_IRXOs` for a key
that has spent $3.00 this month, an OpenAI-format request gets:

```json
{"error":{"code":"budget_exceeded","message":"The monthly budget of this key, $2.00, is used up: $3.00 spent in September 2026. It resets on 2026-10-01 at 00:00 UTC; the gateway's admin can raise it.","param":null,"type":"rate_limit_error"}}
```

An Anthropic-format request gets:

```json
{"error":{"message":"The monthly budget of this key, $2.00, is used up: $3.00 spent in September 2026. It resets on 2026-10-01 at 00:00 UTC; the gateway's admin can raise it.","type":"rate_limit_error"},"request_id":"req_1284c4f5daaa5ac1e1d5bef4","type":"error"}
```

When a project budget is used up, the message names the project instead of the key. The response
has the header `x-should-retry: false`, so the official OpenAI and Anthropic SDKs don't retry the
request. The gateway also logs a warning when a key or project reaches 80% and 100% of its budget.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Requests fail with `budget_exceeded` | The key's or the project's budget is used up; the message says which. | Raise the budget with `chowki key update` or `chowki project update`, or wait for the next month. |
| The spend stays at $0.00 | The model has no price in the catalog, so its requests count as $0. | Check the gateway log: a request without a price has an `unpriced` field that says why. |
| Spend is above the budget | Requests that were in progress when the budget ran out finished, and their cost counts. | Set the budget a little below the amount that you must not exceed. |

## Related

- [Manage virtual keys](manage-virtual-keys.md) · [Set rate limits](set-rate-limits.md) ·
  [Architecture](../concepts/architecture.md)
