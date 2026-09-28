---
title: Report usage and cost from the command line
description: Print the requests, tokens, cost and savings of a range of days with chowki usage, in total or by key, model or day.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

`chowki usage` prints what the gateway served in a range of days: requests, tokens, cost, savings,
exact-cache hits and redactions, in total and, if you ask, by key, model or day. It reads the
database, so it works whether the gateway runs or not.

## Before you begin

- A Chowki folder that has served requests. Run the command there, or name the configuration with
  `--config <FILE>`.

## Print a report

1. In the Chowki folder, run:

   ```sh
   chowki usage --by model
   ```

   Output, for a key with the exact cache on, after three requests of which one repeated another:

   ```text
   Usage from 2026-09-01 to 2026-09-28, in UTC

   Requests     3
   Cost         $0.0000054
   Savings      $0.0000027: exact cache $0.0000027
   Tokens       input 24, output 6, cache read 0, cache write 0, reasoning 0
   Exact cache  1 hit, 2 misses (33% hits)
   Redactions   none

   MODEL              REQUESTS  COST        SAVINGS     INPUT TOKENS  OUTPUT TOKENS
   openai/gpt-6-luna  3         $0.0000054  $0.0000027  24            6
   ```

   Without `--by`, only the totals print.

The report covers this month so far by default. Name other days with `--from` and `--to`, both
included, in UTC:

```sh
chowki usage --from 2026-08-01 --to 2026-08-31 --by key
```

| Option | Effect |
|---|---|
| `--from <DATE>` | The first day, such as `2026-08-01`. By default the first of this month. |
| `--to <DATE>` | The last day. By default today. |
| `--by key` | A row for each virtual key, with its name and prefix. |
| `--by model` | A row for each provider and model. |
| `--by day` | A row for each day. |

## Read the numbers

- **Requests** count every request with a virtual key, failed ones too. When some failed or had no
  price, the line says how many: `3 (1 error, 0 unpriced)`.
- **Cost** is what the providers charge for the requests, from the prices in Chowki's model
  catalog. Unpriced requests count as $0.
- **Savings** are what the exact cache and the providers' prompt caches saved, by method: cached
  input tokens cost less than new ones.
- Amounts under a cent show to eight decimals, as the `x-chowki-cost-usd` header does.

The [dashboard](use-the-dashboard.md) shows the same numbers in a browser, and the
[admin API](../reference/admin-api.md) returns them as JSON.

## Related

- [Set monthly budgets](set-budgets.md) · [Cache responses](cache-responses.md)
