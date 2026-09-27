---
title: Use the dashboard
description: See spend, requests, tokens, savings, redactions and budgets in a browser.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

The dashboard shows what flows through Chowki: spend, requests, tokens, savings, cache hits,
redactions and budgets, by day, key and model. It runs inside the gateway at `/ui/`, so there's
nothing else to install.

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
| Budgets | Each key and project with a monthly budget, and what it spent this month |
| Redactions by type, Net savings by method | Where those totals come from |

Budgets show their state with an icon and a label as well as a color: under 80% used, 80% used
or more, and used up, when Chowki rejects new requests until the next month.

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
