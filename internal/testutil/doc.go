// Package testutil provides test helpers, most importantly fake OpenAI-
// compatible, Anthropic and Gemini servers. Tests never call real providers;
// they point the gateway at these fakes, which return fixed replies and
// exactly the token usage the test configures, in JSON or as SSE streams.
package testutil
