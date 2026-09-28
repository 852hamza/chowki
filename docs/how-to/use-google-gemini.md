---
title: Use Google Gemini
description: Send Gemini API requests through Chowki from the Google Gen AI SDKs or REST, with virtual keys and cost tracking.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Send your Gemini API requests through Chowki: your apps keep the Google Gen AI SDKs and use a
virtual key, while Chowki holds your Gemini API key and applies budgets, rate limits, the exact
cache and redaction, and records what each request costs.

## Before you begin

- A Chowki folder set up with `chowki init`. See
  [Send your first request](../get-started/quickstart.md).
- A Gemini API key from [Google AI Studio](https://aistudio.google.com/apikey).
- Requests are billed to your Gemini API key as usual, unless your project uses the free tier.

## Connect Chowki to Gemini

1. In the Chowki folder, add your key to `.env`:

   ```text
   GEMINI_API_KEY=<GEMINI_API_KEY>
   ```

   Replace `<GEMINI_API_KEY>` with the key from Google AI Studio.

2. Check the `gemini` provider in `chowki.yaml`. `chowki init` adds it:

   ```yaml
   providers:
     - name: gemini
       type: gemini
       base_url: https://generativelanguage.googleapis.com
       api_key_env: GEMINI_API_KEY
   ```

   If your folder is older, add these lines to its `providers` list.

3. If your Gemini project uses the free tier, add `free_tier: true` to the provider. Google doesn't
   bill free-tier requests, so Chowki then records them at $0 instead of at the paid prices, and
   they spend no budget:

   ```yaml
     - name: gemini
       type: gemini
       base_url: https://generativelanguage.googleapis.com
       api_key_env: GEMINI_API_KEY
       free_tier: true
   ```

4. Start the gateway, or restart it so that it reads the changes:

   ```sh
   chowki serve
   ```

5. In another terminal, create a virtual key for your app, and put it in `CHOWKI_KEY`:

   ```sh
   chowki key create --name gemini-app
   export CHOWKI_KEY=<VIRTUAL_KEY>
   ```

   Replace `<VIRTUAL_KEY>` with the key that the command prints.

## Send a request

Point the base URL at `http://localhost:8080/gemini`, and send the virtual key as the API key.

**curl**

```sh
curl http://localhost:8080/gemini/v1beta/models/gemini-2.5-flash:generateContent \
  -H "x-goog-api-key: $CHOWKI_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"Say hello."}]}]}'
```

The answer is Gemini's, unchanged. From a local stand-in provider:

```json
{"candidates": [{"content": {"role": "model", "parts": [{"text": "Hello!"}]}, "finishReason": "STOP", "index": 0}], "usageMetadata": {"promptTokenCount": 1200, "candidatesTokenCount": 150, "thoughtsTokenCount": 150, "totalTokenCount": 1500}, "modelVersion": "gemini-2.5-flash", "responseId": "resp-1"}
```

**Python**, with the `google-genai` package:

```python
import os

from google import genai
from google.genai import types

client = genai.Client(
    api_key=os.environ["CHOWKI_KEY"],
    http_options=types.HttpOptions(base_url="http://localhost:8080/gemini"),
)
response = client.models.generate_content(model="gemini-2.5-flash", contents="Say hello.")
print(response.text)
```

**TypeScript**, with the `@google/genai` package:

```typescript
import { GoogleGenAI } from "@google/genai";

const ai = new GoogleGenAI({
  apiKey: process.env.CHOWKI_KEY,
  httpOptions: { baseUrl: "http://localhost:8080/gemini" },
});
const response = await ai.models.generateContent({
  model: "gemini-2.5-flash",
  contents: "Say hello.",
});
console.log(response.text);
```

Streaming, token counting and embeddings work the same way: in the SDKs, with
`generate_content_stream`, `count_tokens` and `embed_content`. Over REST, stream with
`:streamGenerateContent?alt=sse`: Chowki relays streams as server-sent events only.

### Name the model

Use a Gemini model ID, such as `gemini-2.5-flash`, when `chowki.yaml` has one Gemini provider. With
several, name the provider too, such as `gemini/gemini-2.5-flash`, or use an alias. See
[Route requests with aliases and fallback](routing-and-fallback.md).

## How Chowki prices Gemini

Chowki prices Gemini models at the paid-tier prices in its model catalog, from Google's
[pricing page](https://ai.google.dev/gemini-api/docs/pricing):

- Thinking tokens (`thoughtsTokenCount`) cost the output price, as Google bills them.
- Tokens that Gemini's cache served (`cachedContentTokenCount`) cost the context-caching price.
  The difference to the input price counts as savings of provider prompt caching.
- Gemini 2.5 Pro and Gemini 3.1 Pro Preview cost more for prompts above 200,000 tokens, cached
  tokens included. Chowki applies the higher prices to those requests.
- Gemini 3.6 Flash, 3.7 Flash and 3.8 Flash get new prices on January 1, 2027. Chowki prices each
  request at the prices of its day.

Some requests stay unpriced, and show as such in the log, because their price depends on more than
tokens: requests with audio input, requests that use built-in tools such as Google Search, and
requests on the Flex or Priority tiers. Chowki doesn't count the hourly storage fee of explicit
context caches.

## Verify

The response of a priced, non-streaming request carries its cost:

```sh
curl -sS -o /dev/null -D - \
  http://localhost:8080/gemini/v1beta/models/gemini-2.5-flash:generateContent \
  -H "x-goog-api-key: $CHOWKI_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"Say hello."}]}]}' | grep -i '^x-chowki'
```

Output, for 1,200 input tokens and 300 output tokens of Gemini 2.5 Flash:

```text
X-Chowki-Cache: bypass
X-Chowki-Cost-Usd: 0.00111000
X-Chowki-Request-Id: req_EXAMPLE
```

With `free_tier: true`, the cost is `0.00000000`.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `"status":"UNAUTHENTICATED"` with the reason `INVALID_API_KEY` | The client sent your Gemini key instead of a virtual key. | Send the virtual key: Chowki holds the Gemini key. |
| `"reason":"PROVIDER_KEY_MISSING"` | Chowki can't read `GEMINI_API_KEY`. | Check `.env` in the folder where you run `chowki serve`, then restart the gateway. |
| `Add ?alt=sse to stream` | A REST client streamed without `alt=sse`. | Add `?alt=sse` to the URL. The SDKs do. |
| `UNAVAILABLE` with `This model is currently experiencing high demand` | Google has more requests for that model than it can take, for now. | Retry later, or name another model. An alias can fall back to other models on its own; see [Fall back to other models of one provider](routing-and-fallback.md#fall-back-to-other-models-of-one-provider). |
| `"status":"NOT_FOUND"` | The path isn't a method that Chowki relays, such as the Files API. | Call that API directly. Chowki relays `generateContent`, `streamGenerateContent`, `countTokens`, `embedContent` and `batchEmbedContents`. |

## Next steps

- [Set monthly budgets](set-budgets.md)
- [Cache responses](cache-responses.md)

## Related

- [API reference](../reference/api.md) · [Configuration reference](../reference/configuration.md)
