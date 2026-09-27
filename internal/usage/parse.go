package usage

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Family is the API format of a request and its response.
type Family string

// API families.
const (
	OpenAI    Family = "openai"
	Anthropic Family = "anthropic"
	Gemini    Family = "gemini"
)

// Usage is the token usage of one request, the same for every family.
type Usage struct {
	// Input counts every prompt token, including CacheRead and CacheWrite.
	Input int64
	// Output counts every generated token, including Reasoning.
	Output int64
	// CacheRead counts prompt tokens read from the provider's prompt cache.
	CacheRead int64
	// CacheWrite counts prompt tokens written to the prompt cache.
	CacheWrite int64
	// CacheWrite1h is the part of CacheWrite that went to Anthropic's
	// 1-hour cache, which costs more than the 5-minute cache.
	CacheWrite1h int64
	// Reasoning is the part of Output spent on reasoning, for information.
	Reasoning int64
}

// Report is what a provider says about its response.
type Report struct {
	// Model is the model that served the request, as the response names it.
	Model string
	// Usage is nil when the response reported none.
	Usage *Usage
	// Modifier names a pricing condition that Chowki doesn't model yet, such
	// as a priority service tier. The cost is then unknown.
	Modifier string
}

// OpenAI chat completions usage, with the fields documented in
// https://github.com/openai/openai-openapi (CompletionUsage).
type openAIUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens     int64 `json:"cached_tokens"`
		CacheWriteTokens int64 `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type openAIBody struct {
	Model       string       `json:"model"`
	ServiceTier string       `json:"service_tier"`
	Usage       *openAIUsage `json:"usage"`
}

func (b openAIBody) apply(r *Report) {
	if b.Model != "" {
		r.Model = b.Model
	}
	// "default" is the standard price; flex, fast (formerly priority) and
	// scale are priced differently.
	if b.ServiceTier != "" && b.ServiceTier != "default" {
		r.Modifier = "service tier " + b.ServiceTier
	}
	if u := b.Usage; u != nil {
		r.Usage = &Usage{
			Input:      u.PromptTokens,
			Output:     u.CompletionTokens,
			CacheRead:  u.PromptTokensDetails.CachedTokens,
			CacheWrite: u.PromptTokensDetails.CacheWriteTokens,
			Reasoning:  u.CompletionTokensDetails.ReasoningTokens,
		}
	}
}

// Anthropic Messages usage, documented in
// https://platform.claude.com/docs/en/api/messages. Pointers tell a missing
// field from zero, because message_delta events carry only some fields.
type anthropicUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreation            *struct {
		Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	OutputTokensDetails *struct {
		ThinkingTokens int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
	ServiceTier  string `json:"service_tier"`
	Speed        string `json:"speed"`
	InferenceGeo string `json:"inference_geo"`
}

// merge copies the fields that later sets.
func (a *anthropicUsage) merge(later anthropicUsage) {
	for _, f := range []struct{ dst, src **int64 }{
		{&a.InputTokens, &later.InputTokens}, {&a.OutputTokens, &later.OutputTokens},
		{&a.CacheCreationInputTokens, &later.CacheCreationInputTokens},
		{&a.CacheReadInputTokens, &later.CacheReadInputTokens},
	} {
		if *f.src != nil {
			*f.dst = *f.src
		}
	}
	if later.CacheCreation != nil {
		a.CacheCreation = later.CacheCreation
	}
	if later.OutputTokensDetails != nil {
		a.OutputTokensDetails = later.OutputTokensDetails
	}
	for _, f := range []struct{ dst, src *string }{
		{&a.ServiceTier, &later.ServiceTier}, {&a.Speed, &later.Speed}, {&a.InferenceGeo, &later.InferenceGeo},
	} {
		if *f.src != "" {
			*f.dst = *f.src
		}
	}
}

func (a *anthropicUsage) apply(r *Report) {
	n := func(p *int64) int64 {
		if p == nil {
			return 0
		}
		return *p
	}
	u := &Usage{
		Output:     n(a.OutputTokens),
		CacheRead:  n(a.CacheReadInputTokens),
		CacheWrite: n(a.CacheCreationInputTokens),
	}
	// input_tokens excludes the tokens read from or written to the cache.
	u.Input = n(a.InputTokens) + u.CacheRead + u.CacheWrite
	if a.CacheCreation != nil {
		u.CacheWrite1h = a.CacheCreation.Ephemeral1h
	}
	if a.OutputTokensDetails != nil {
		u.Reasoning = a.OutputTokensDetails.ThinkingTokens
	}
	r.Usage = u
	switch {
	case a.ServiceTier != "" && a.ServiceTier != "standard":
		r.Modifier = "service tier " + a.ServiceTier
	case a.Speed == "fast":
		r.Modifier = "fast mode"
	case a.InferenceGeo == "us":
		r.Modifier = "US-only inference"
	}
}

type anthropicBody struct {
	Model string          `json:"model"`
	Usage *anthropicUsage `json:"usage"`
}

// Gemini usage metadata: UsageMetadata, and EmbeddingUsageMetadata for
// embeddings, in the discovery document of the Gemini API,
// https://generativelanguage.googleapis.com/$discovery/rest?version=v1beta.
type geminiUsage struct {
	// PromptTokenCount includes the cached tokens.
	PromptTokenCount        int64 `json:"promptTokenCount"`
	CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
	// CandidatesTokenCount excludes the thoughts, which are billed as output
	// too.
	CandidatesTokenCount    int64            `json:"candidatesTokenCount"`
	ThoughtsTokenCount      int64            `json:"thoughtsTokenCount"`
	ToolUsePromptTokenCount int64            `json:"toolUsePromptTokenCount"`
	ServiceTier             string           `json:"serviceTier"`
	PromptTokensDetails     []modalityTokens `json:"promptTokensDetails"`
	PromptTokenDetails      []modalityTokens `json:"promptTokenDetails"` // in embeddings
}

type modalityTokens struct {
	Modality   string `json:"modality"`
	TokenCount int64  `json:"tokenCount"`
}

type geminiBody struct {
	ModelVersion  string       `json:"modelVersion"`
	UsageMetadata *geminiUsage `json:"usageMetadata"`
}

func (b geminiBody) apply(r *Report) {
	if b.ModelVersion != "" {
		r.Model = b.ModelVersion
	}
	u := b.UsageMetadata
	if u == nil {
		return
	}
	r.Usage = &Usage{
		Input:     u.PromptTokenCount,
		CacheRead: u.CachedContentTokenCount,
		Output:    u.CandidatesTokenCount + u.ThoughtsTokenCount,
		Reasoning: u.ThoughtsTokenCount,
	}
	var audio int64
	for _, d := range append(u.PromptTokensDetails, u.PromptTokenDetails...) {
		if d.Modality == "AUDIO" {
			audio += d.TokenCount
		}
	}
	switch {
	case u.ServiceTier != "" && u.ServiceTier != "standard" && u.ServiceTier != "unspecified":
		r.Modifier = "service tier " + u.ServiceTier
	case audio > 0:
		// Audio input has its own price for most models.
		r.Modifier = "audio input"
	case u.ToolUsePromptTokenCount > 0:
		// Built-in tools, such as Google Search, have prices of their own.
		r.Modifier = "built-in tool use"
	}
}

// ParseResponse reads the report from a complete, non-streaming response
// body of the given family.
func ParseResponse(f Family, body []byte) (Report, error) {
	var r Report
	switch f {
	case OpenAI:
		var b openAIBody
		if err := json.Unmarshal(body, &b); err != nil {
			return r, fmt.Errorf("parse %s response: %w", f, err)
		}
		b.apply(&r)
	case Anthropic:
		var b anthropicBody
		if err := json.Unmarshal(body, &b); err != nil {
			return r, fmt.Errorf("parse %s response: %w", f, err)
		}
		r.Model = b.Model
		if b.Usage != nil {
			b.Usage.apply(&r)
		}
	case Gemini:
		var b geminiBody
		if err := json.Unmarshal(body, &b); err != nil {
			return r, fmt.Errorf("parse %s response: %w", f, err)
		}
		b.apply(&r)
	default:
		return r, fmt.Errorf("unknown API family %q", f)
	}
	return r, nil
}

// Stream collects the report from the events of a streamed response. It
// ignores events it can't parse, because a stream must never fail for
// accounting's sake.
type Stream struct {
	family    Family
	report    Report
	seen      bool // an OpenAI or Gemini chunk was decoded
	anthropic *anthropicUsage
}

// NewStream returns a Stream for a response of the given family.
func NewStream(f Family) *Stream { return &Stream{family: f} }

// Event takes one server-sent event: its name, empty for OpenAI and Gemini
// streams, and its data.
func (s *Stream) Event(name string, data []byte) {
	switch s.family {
	case OpenAI:
		// The first chunk names the model and tier. After it, only the
		// chunk with a usage object matters; the others have "usage": null
		// at most, so skip decoding them.
		if s.seen && !bytes.Contains(data, []byte(`"usage":{`)) && !bytes.Contains(data, []byte(`"usage": {`)) {
			return
		}
		var b openAIBody
		if json.Unmarshal(data, &b) == nil {
			s.seen = true
			b.apply(&s.report)
		}
	case Anthropic:
		switch name {
		case "message_start":
			var e struct {
				Message anthropicBody `json:"message"`
			}
			if json.Unmarshal(data, &e) == nil {
				s.report.Model = e.Message.Model
				if e.Message.Usage != nil {
					s.anthropic = e.Message.Usage
				}
			}
		case "message_delta":
			// Counts here are cumulative, so the last event wins.
			var e struct {
				Usage *anthropicUsage `json:"usage"`
			}
			if json.Unmarshal(data, &e) == nil && e.Usage != nil {
				if s.anthropic == nil {
					s.anthropic = &anthropicUsage{}
				}
				s.anthropic.merge(*e.Usage)
			}
		}
	case Gemini:
		// Chunks may repeat usageMetadata with growing counts: the last one
		// wins. The first chunk names the model.
		if s.seen && !bytes.Contains(data, []byte(`"usageMetadata"`)) {
			return
		}
		var b geminiBody
		if json.Unmarshal(data, &b) == nil {
			s.seen = true
			b.apply(&s.report)
		}
	}
}

// Report returns what the stream reported so far.
func (s *Stream) Report() Report {
	r := s.report
	if s.anthropic != nil {
		s.anthropic.apply(&r)
	}
	return r
}
