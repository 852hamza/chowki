// Package providers calls the upstream APIs: OpenAI and every
// OpenAI-compatible API, and Anthropic. It swaps the client's virtual key
// for the provider key and forwards only the headers a provider needs.
package providers
