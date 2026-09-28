---
title: Use the dashboard
description: See spend, savings, budgets and each request in a browser, and download requests as CSV.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

The dashboard shows what flows through Chowki: spend, requests, tokens, savings, cache hits,
redactions, budgets and the health of your providers, by day, key and model, and it lists each
request. It runs inside the gateway at `/ui/`, so there's nothing else to install.

## Before you begin

- Chowki running with `chowki serve`. See [Send your first request](../get-started/quickstart.md).
- A browser that can reach the gateway's address, such as `http://localhost:8080`.
- Access to the Chowki folder, to create an admin token. Anyone with an admin token can read the
  metadata of every request and manage every key, so give tokens only to administrators.

> **Warning:** Signing in sends the admin token to the gateway. When the dashboard is reachable
> from other machines, serve it over HTTPS, for example behind a reverse proxy that terminates TLS.

## Sign in

1. In the Chowki folder, create an admin token:

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

2. Open `http://localhost:8080/ui/` in a browser. Replace `localhost:8080` with your gateway's
   address if it's different.

   The sign-in page opens.

3. In **Admin token**, paste the token from step 1, and select **Sign in**.

   The dashboard opens with the current month.

A session lasts 12 hours. **Sign out** ends it. Revoking the admin token with
`chowki admin revoke <PREFIX>` ends its sessions at once, and so does restarting Chowki.

## Choose what to see

1. Choose a range in the first row of buttons: **Month to date**, **7 days**, **30 days** or
   **90 days**.
2. Choose a measure: **Spend**, **Requests** or **Tokens**. The measure sets the large number, the
   chart, and the order of the top keys and models.

   Without a choice, the dashboard measures spend, or requests when no request in the range had a
   price, such as when all your models are local.
3. To see the numbers of one day, point at its column in the chart. With a keyboard, move to the
   chart with Tab, then use the arrow keys.
4. To see every day as numbers, select **Show as a table** under the chart.

## See each request

The **Requests** tab lists requests, the newest first: when each one came, its key and model, its
status and error, how long it took, its tokens and cost, and what the exact cache and redaction
did. It shows what each request was, never what it said: Chowki doesn't keep prompts or answers.

1. Select **Requests** at the top of the page.
2. To narrow the list, choose a **Key**, a **Model** or a **Result**, **Failed** or **Succeeded**,
   and select **Show**.
3. To see older requests, select **Older requests** at the end of the list.

The overview links to the list, too: select a key or a model in **Top keys** or **Top models** to
see its requests, or a provider in **Providers** to see its requests, or its number of failed
requests to see those.

## Download requests as CSV

On the **Requests** tab, select **Download CSV**. The file has the requests that the filters
select, up to the newest 10,000, with these columns:

```text
time,request_id,key_name,key_prefix,provider,model,endpoint,stream,status,error,latency_ms,ttfb_ms,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,reasoning_tokens,cost_usd,savings_usd,savings_method,cache,redactions
```

Times are in UTC. `cost_usd` is empty for a request to a model without a price, and the token
columns are empty when the provider reported no usage. A text that starts with `=`, `+`, `-` or
`@`, such as a model name that a client made up, starts with an apostrophe in the file, so that a
spreadsheet shows it rather than running it as a formula.

## What the dashboard shows

| Part | Shows |
|---|---|
| Spend | The cost of the range's requests, at the prices in Chowki's model catalog |
| Requests, Tokens | How many requests, and their input and output tokens as providers reported them |
| Failed | Requests that got an error status, 400 or above, from Chowki or the provider |
| Net savings | What the exact cache and provider prompt caching saved, minus what cache writes cost |
| Cache hit rate | The share of exact-cache lookups that found an answer |
| Redactions | The secrets and personal data that redaction found |
| Top keys, Top models | The eight keys and models with the most of the chosen measure |
| Providers | For each provider, its requests, how many failed because of it, and the median and 95th percentile of its latency |
| Budgets | Each key and project with a monthly budget, and what it spent this month |
| Redactions by type, Net savings by method | Where those totals come from |

Budgets show their state with an icon and a label as well as a color: under 80% used, 80% used
or more, and used up, when Chowki rejects new requests until the next month.

**Providers** counts only the requests that reached a provider: not the answers of the exact
cache, nor the requests that Chowki refused, such as over a budget. A request failed because of
its provider when the provider didn't answer, broke off its answer, or answered with 429 or a 5xx
status, even after a fallback. A provider gets a warning icon when 5% of its requests failed, and
a critical one from 25%. Its latency is that of its successful requests, Chowki's own time
included.

How Chowki counts:

- Days and months are in UTC, and the dashboard counts whole days.
- A request to a model without a price in the catalog, such as a local model in Ollama, counts as
  $0. The **Spend** tile says how many such requests there were.
- Chowki saves request records about once a second, so a request shows up within a second or two
  of its end. Reload the page to update it.
- Chowki keeps the sums of each day after it deletes old request records (`retention_days`), so
  the numbers of past months don't change.

## Verify

Check that the gateway serves the dashboard and asks for a sign-in:

```sh
curl -sS -o /dev/null -w '%{http_code} %header{location}\n' http://localhost:8080/ui/
```

Output:

```text
303 /ui/login
```

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| "That admin token isn't valid" | The token is revoked or mistyped, or it's a virtual key | Create a new token with `chowki admin create`. Virtual keys can't sign in. |
| "The form expired. Sign in again." | The sign-in page was open for more than 12 hours, or the browser blocks cookies | Reload the page, allow cookies for the gateway's address, and sign in again. |
| Signing in returns to the sign-in page | The browser dropped the session cookie. Chowki marks cookies Secure when a proxy sends `X-Forwarded-Proto: https`, and browsers drop Secure cookies over plain HTTP. | Open the dashboard over HTTPS through the proxy, or correct the proxy's `X-Forwarded-Proto` header. |
| A link from another site or app opens the sign-in page, though you signed in | For safety, browsers don't send the dashboard's cookies on visits that start on another site | Open the dashboard from a bookmark or by typing its address. |
| Spend stays at $0.00 | The models have no price in the catalog | Choose **Requests** or **Tokens**. |

## Next steps

- [Set monthly budgets](set-budgets.md)
- [Cache responses](cache-responses.md)

## Related

- [Admin API reference](../reference/admin-api.md): the same numbers as JSON, for scripts
- [Monitor Chowki](../operations/monitoring.md): Prometheus metrics and health checks
