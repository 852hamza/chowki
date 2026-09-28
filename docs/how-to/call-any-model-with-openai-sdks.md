---
title: Call Anthropic and Gemini models with OpenAI SDKs
description: Send OpenAI-format requests to Anthropic and Google Gemini models, and get OpenAI-format answers back.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Apps and tools that speak the OpenAI API can use Anthropic and Google Gemini models through Chowki,
without a change of SDK. Chowki translates the request to the provider's API, and the answer back:
text, images, tool calls, streams, usage and errors.

## Before you begin

- Chowki running with an `anthropic` or a `gemini` provider in `chowki.yaml`, as `chowki init`
  adds them. See [Use Google Gemini](use-google-gemini.md) for Gemini's key.
- A virtual key in `CHOWKI_KEY`. See [Manage virtual keys](manage-virtual-keys.md).

## Send a request

1. Point your OpenAI client at `http://localhost:8080/v1`, with the virtual key as its API key.
2. Name the model with its provider, such as `anthropic/claude-sonnet-5` or
   `gemini/gemini-2.5-flash`, or with an alias. A model name without a provider goes to the
   provider that Chowki's model catalog lists for it, or else to your OpenAI-compatible provider.

   ```python
   import os

   from openai import OpenAI

   client = OpenAI(api_key=os.environ["CHOWKI_KEY"], base_url="http://localhost:8080/v1")

   for model in ["anthropic/claude-sonnet-5", "gemini/gemini-2.5-flash"]:
       response = client.chat.completions.create(
           model=model,
           messages=[{"role": "user", "content": "Say hello."}],
       )
       print(model, response.choices[0].message.content, response.usage.prompt_tokens,
             response.usage.completion_tokens)
   ```

   Output, from a local stand-in provider:

   ```text
   anthropic/claude-sonnet-5 Hello! 1200 300
   gemini/gemini-2.5-flash Hello! 1200 300
   ```

Streams work the same way, with the usage at the end when you set
`stream_options={"include_usage": True}`. An alias can mix providers of different APIs: when a
target fails before the answer starts, Chowki falls back to the next one. See
[Route requests with aliases and fallback](routing-and-fallback.md).

## What Chowki translates

| OpenAI option | Anthropic | Gemini |
|---|---|---|
| `messages`: system and developer messages | The system prompt | The system instruction |
| `messages`: text, and images as `data:` URLs | Yes | Yes |
| `messages`: images as `https://` URLs | Yes | No |
| `tools`, `tool_choice`, tool calls and results | Yes | Yes |
| `tools[].function.strict` | Yes | Yes, as the `VALIDATED` calling mode, except with a forced tool call |
| `parallel_tool_calls: false` | Yes | No |
| `max_tokens`, `max_completion_tokens` | Yes; without them, the model's maximum from the catalog, or 4096 | Yes |
| `temperature` | From 0 to 1 | Yes |
| `top_p`, `stop` | Yes | Yes; up to 5 stop sequences |
| `n` | 1 only | Yes |
| `seed`, `presence_penalty`, `frequency_penalty` | No | Yes |
| `response_format` | `json_schema` only | `json_object` and `json_schema` |
| `reasoning_effort` | `none` turns thinking off; `low` to `max` set the effort | Google's mapping for Gemini 2.5 and later; see below |
| `user`, `safety_identifier` | As `metadata.user_id` | Not sent; Gemini has no such field |

For Gemini, `reasoning_effort` follows the table in Google's
[OpenAI compatibility guide](https://ai.google.dev/gemini-api/docs/openai): a thinking budget of
1,024 tokens for `minimal` and `low`, 8,192 for `medium` and 24,576 for `high` on Gemini 2.5, where
`none` turns thinking off; and the thinking level of the same name on later models, which can't
turn thinking off.

Chowki answers `400` with the code `unsupported_option` when a request uses an option that the
provider can't honor, and `param` names it. It never drops an option silently. For example:

```sh
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $CHOWKI_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"anthropic/claude-sonnet-5","seed":7,"messages":[{"role":"user","content":"Say hello."}]}'
```

Output:

```json
{"error":{"code":"unsupported_option","message":"Anthropic doesn't take a seed. The gateway translates this request for the provider \"anthropic\", which speaks another API.","param":"seed","type":"invalid_request_error"}}
```

Options that only steer OpenAI's own services, such as `prompt_cache_key`, `metadata` and
`service_tier: "auto"`, have no effect. Audio, files, log probabilities, predicted outputs and web
search aren't translated.

## Tool calls and thinking

Anthropic and Gemini models think before they call tools, and both APIs want that thinking back in
the next request of a tool-use loop: Anthropic as thinking blocks, Gemini as thought signatures.
The OpenAI format has no place for them, so Chowki keeps them in memory for an hour, by the ID of
each tool call, and puts them back when your client sends the tool results. It keeps only the
thinking blocks and signatures, never your prompts, the model's text or the tool arguments.

Gemini tool calls also carry their thought signature in
`tool_calls[].extra_content.google.thought_signature`, as Gemini's own OpenAI-compatible API does.
Clients that send it back don't depend on Chowki's memory. When the thinking is lost, for example
after a restart, a Gemini 3 request gets the placeholder signature that Google documents for that
case, and the loop continues, with lower quality for that step.

## Errors and usage

- A provider's errors reach the client in the OpenAI format, with the provider's status and
  message, and its error type or status in `code`.
- Usage counts as OpenAI counts it: `prompt_tokens` includes cached tokens, and
  `completion_tokens` includes thinking. Chowki prices each request from the provider's own report,
  so Gemini's thinking and cached tokens cost what Google charges.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `"code":"unsupported_option"` | The provider can't honor an option of the request. | Remove the option that `param` names, or use a model that supports it. |
| `"code":"wrong_endpoint"` | The request went to an endpoint that doesn't translate, such as `/v1/embeddings` or `/anthropic/v1/messages`. | Send chat requests for other APIs to `/v1/chat/completions`. |
| Answers stop early with `finish_reason: "length"` | Anthropic needs an output limit, and Chowki used 4096 for a model that its catalog doesn't know. | Set `max_completion_tokens`. |

## Related

- [API reference](../reference/api.md) · [Use Google Gemini](use-google-gemini.md)
