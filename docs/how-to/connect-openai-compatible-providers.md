---
title: Connect OpenAI-compatible providers
description: Add DeepSeek, xAI, Mistral, Groq, OpenRouter or any other OpenAI-compatible API as a provider.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-27
---

Many providers serve the OpenAI chat completions API. Chowki reaches all of them as providers of
type `openai`, with virtual keys, budgets, caching and redaction like every other provider.

## Before you begin

- A Chowki folder set up with `chowki init`. See
  [Send your first request](../get-started/quickstart.md).
- An API key of each provider that you add.

## Add the providers

1. Put each provider's key in `.env`, in the Chowki folder:

   ```text
   DEEPSEEK_API_KEY=<DEEPSEEK_API_KEY>
   XAI_API_KEY=<XAI_API_KEY>
   MISTRAL_API_KEY=<MISTRAL_API_KEY>
   GROQ_API_KEY=<GROQ_API_KEY>
   OPENROUTER_API_KEY=<OPENROUTER_API_KEY>
   ```

   Replace each placeholder with the provider's key, and leave out the providers that you don't
   use.

2. Add the providers to the `providers` list in `chowki.yaml`:

   <!-- The test in internal/config checks that this block loads. -->

   ```yaml
   providers:
     - name: deepseek
       type: openai
       base_url: https://api.deepseek.com
       api_key_env: DEEPSEEK_API_KEY
     - name: xai
       type: openai
       base_url: https://api.x.ai/v1
       api_key_env: XAI_API_KEY
     - name: mistral
       type: openai
       base_url: https://api.mistral.ai/v1
       api_key_env: MISTRAL_API_KEY
     - name: groq
       type: openai
       base_url: https://api.groq.com/openai/v1
       api_key_env: GROQ_API_KEY
     - name: openrouter
       type: openai
       base_url: https://openrouter.ai/api/v1
       api_key_env: OPENROUTER_API_KEY
   ```

   Each `base_url` is the one that the provider's documentation gives for the OpenAI SDKs. For
   another OpenAI-compatible API, use its base URL the same way.

3. Restart `chowki serve`, so that it reads the changes.

## Send requests

Name the model with the provider, as `<provider>/<model>`, on `/v1/chat/completions`:

| Provider | Model name through Chowki |
|---|---|
| DeepSeek | `deepseek/deepseek-flash` |
| xAI | `xai/grok-4.7` |
| Mistral | `mistral/mistral-small-latest` |
| Groq | `groq/openai/gpt-oss-20b` |
| OpenRouter | `openrouter/anthropic/claude-sonnet-4.6` |

Chowki removes the provider's name and passes the rest on, so model names that contain a `/`, as
on Groq and OpenRouter, reach the provider as they are. For example:

```sh
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $CHOWKI_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek/deepseek-flash","messages":[{"role":"user","content":"Say hello."}]}'
```

The answer is the provider's, unchanged. From a local stand-in provider:

```json
{"id": "chatcmpl-1", "object": "chat.completion", "created": 1790000000, "model": "deepseek-flash", "choices": [{"index": 0, "message": {"role": "assistant", "content": "Hello!"}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 1200, "completion_tokens": 300, "total_tokens": 1500}}
```

## How Chowki reads each provider

- **DeepSeek** reports cache hits in `prompt_tokens_details.cached_tokens`, as OpenAI does.
- **xAI** counts reasoning tokens apart from `completion_tokens`, unlike OpenAI. Chowki adds them
  to the output, as the total in xAI's answers shows. xAI now recommends its Responses API for new
  work; Chowki relays its chat completions API.
- **Mistral** counts cached tokens inside `prompt_tokens`, as OpenAI does.
- **Groq** doesn't support `logprobs`, `logit_bias`, `top_logprobs`, `messages[].name` or `n`
  above 1, as its [OpenAI compatibility page](https://console.groq.com/docs/openai) says. In
  streams, Chowki also reads usage from `x_groq.usage`.
- **OpenRouter** reports its own charge in `usage.cost`, in credits. Chowki doesn't use it.

Chowki's model catalog has no prices for these providers yet, so their requests are unpriced: the
dashboard and records show their tokens, and they count as $0 toward budgets.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `"code":"unknown_provider"` | The model name has no provider, and more than one provider could serve it. | Name the model as `<provider>/<model>`. |
| The provider answers 404 for the model | The model name reached the provider without its own prefix, such as `anthropic/` on OpenRouter. | Keep the provider's full model name after Chowki's provider name. |
| `"code":"provider_key_missing"` | Chowki can't read the provider's key variable. | Check `.env` and the provider's `api_key_env`, then restart the gateway. |

## Related

- [Configuration reference](../reference/configuration.md) ·
  [Route requests with aliases and fallback](routing-and-fallback.md)
