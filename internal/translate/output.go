package translate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/852hamza/chowki/internal/usage"
)

// The OpenAI chat completion and its chunks, as translation writes them:
// ChatCompletion and ChatCompletionChunk in the openai-python SDK.

type completion struct {
	ID      string    `json:"id"`
	Object  string    `json:"object"`
	Created int64     `json:"created"`
	Model   string    `json:"model"`
	Choices []choice  `json:"choices"`
	Usage   *outUsage `json:"usage,omitempty"`
}

type choice struct {
	Index        int      `json:"index"`
	Message      *message `json:"message,omitempty"`
	Delta        *delta   `json:"delta,omitempty"`
	Logprobs     any      `json:"logprobs"`
	FinishReason *string  `json:"finish_reason"`
}

type message struct {
	Role      string    `json:"role"`
	Content   *string   `json:"content"`
	Refusal   *string   `json:"refusal"`
	ToolCalls []outCall `json:"tool_calls,omitempty"`
}

type delta struct {
	Role      string    `json:"role,omitempty"`
	Content   *string   `json:"content,omitempty"`
	ToolCalls []outCall `json:"tool_calls,omitempty"`
}

type outCall struct {
	Index        *int          `json:"index,omitempty"` // in chunks only
	ID           string        `json:"id,omitempty"`
	Type         string        `json:"type,omitempty"`
	Function     outFunction   `json:"function"`
	ExtraContent *extraContent `json:"extra_content,omitempty"`
}

type outFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

// extraContent carries Gemini's thought signature, as Gemini's own
// OpenAI-compatible API does.
type extraContent struct {
	Google struct {
		ThoughtSignature string `json:"thought_signature"`
	} `json:"google"`
}

type outUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens     int64 `json:"cached_tokens"`
		CacheWriteTokens int64 `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// openAIUsage writes usage as OpenAI reports it: the prompt counts every
// input token, cached or not, and the completion includes the reasoning.
func openAIUsage(u *usage.Usage) *outUsage {
	if u == nil {
		return nil
	}
	out := &outUsage{PromptTokens: u.Input, CompletionTokens: u.Output, TotalTokens: u.Input + u.Output}
	out.PromptTokensDetails.CachedTokens = u.CacheRead
	out.PromptTokensDetails.CacheWriteTokens = u.CacheWrite
	out.CompletionTokensDetails.ReasoningTokens = u.Reasoning
	return out
}

func ptr[T any](v T) *T { return &v }

// callID returns a new ID for a tool call whose provider gave it none.
func callID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return "call_" + hex.EncodeToString(b)
}

// chunkWriter builds the chunks of a translated stream.
type chunkWriter struct {
	id, model string
	created   int64
}

func (w *chunkWriter) chunk(choices []choice, u *outUsage) []byte {
	if choices == nil {
		choices = []choice{}
	}
	b, _ := json.Marshal(completion{ID: w.id, Object: "chat.completion.chunk", Created: w.created, Model: w.model,
		Choices: choices, Usage: u}) // these types always marshal
	return b
}

func (w *chunkWriter) delta(index int, d delta, finish *string) []byte {
	return w.chunk([]choice{{Index: index, Delta: &d, FinishReason: finish}}, nil)
}

// errorChunk is an error in the middle of a stream, which the official
// OpenAI SDKs raise as an API error.
func errorChunk(message, typ string) []byte {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": message, "type": typ, "param": nil,
		"code": nil}}) // a map of strings always marshals
	return b
}
