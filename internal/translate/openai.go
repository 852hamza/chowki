package translate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Request is an OpenAI chat completions request, as translation reads it.
// Fields follow CompletionCreateParams in the openai-python SDK,
// https://github.com/openai/openai-python/blob/main/src/openai/types/chat/completion_create_params.py.
type Request struct {
	Model string
	// System holds the text parts of the system and developer messages, in
	// order: the other APIs take a single system prompt.
	System   []string
	Messages []Message
	Tools    []Tool
	// ToolChoice is nil when the request doesn't set it.
	ToolChoice        *ToolChoice
	ParallelToolCalls *bool
	MaxTokens         *int64
	Temperature, TopP *float64
	// PresencePenalty and FrequencyPenalty are nil when unset or zero.
	PresencePenalty, FrequencyPenalty *float64
	Stop                              []string
	Stream, IncludeUsage              bool
	// N is the number of choices; 0 when unset.
	N      int64
	Seed   *int64
	Format *Format // nil for plain text
	// Effort is reasoning_effort, such as "low"; empty when unset.
	Effort string
	// User is user or safety_identifier: who the end user is.
	User string
}

// Message is a user, assistant or tool message.
type Message struct {
	Role    string // "user", "assistant" or "tool"
	Content []Part
	// ToolCalls are an assistant's calls.
	ToolCalls []ToolCall
	// ToolCallID is the call that a tool message answers.
	ToolCallID string
}

// Part is a piece of a message's content: text, or an image.
type Part struct {
	Text string
	// ImageURL is a data: URL or an http(s) URL, for an image part.
	ImageURL string
}

// ToolCall is a call of a function that an assistant made.
type ToolCall struct {
	ID, Name string
	// Arguments is the arguments as a JSON object, as OpenAI sends them: in
	// a string.
	Arguments string
	// Signature is Gemini's thought signature, from
	// extra_content.google.thought_signature.
	Signature string
}

// Tool is a function that the model may call.
type Tool struct {
	Name, Description string
	// Parameters is a JSON Schema; nil when the request has none.
	Parameters json.RawMessage
	Strict     bool
}

// ToolChoice says whether and which tools the model must call.
type ToolChoice struct {
	Mode string // "auto", "none", "required", or "function" for Name
	Name string
}

// Format is a JSON response format.
type Format struct {
	Type   string // "json_object" or "json_schema"
	Name   string
	Schema json.RawMessage
	Strict bool
}

// Error is a request that can't be translated. The gateway answers it with
// a 400 that names the option.
type Error struct {
	// Param is the option, such as "logit_bias" or "messages[2].content".
	Param   string
	Message string
}

func (e *Error) Error() string { return e.Message }

func errorf(param, format string, args ...any) *Error {
	return &Error{Param: param, Message: fmt.Sprintf(format, args...)}
}

// isNull reports whether a JSON value is null, which OpenAI treats as unset.
func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

// ParseRequest reads an OpenAI chat completions request for translation.
// It rejects options that no other API can honor, and options it doesn't
// know, so that none is dropped silently. Options that only change how
// OpenAI caches, stores or pads a request are accepted and have no effect.
func ParseRequest(body []byte) (*Request, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, errorf("", "The request body isn't a JSON object: %v.", err)
	}
	r := &Request{}
	store := false
	for _, key := range sortedKeys(fields) {
		raw := fields[key]
		if isNull(raw) {
			continue
		}
		var err error
		switch key {
		case "model":
			err = decode(raw, &r.Model)
		case "messages":
			err = r.parseMessages(raw)
		case "max_tokens", "max_completion_tokens":
			var n int64
			if err = decode(raw, &n); err == nil && n <= 0 {
				return nil, errorf(key, "%s must be at least 1.", key)
			}
			r.MaxTokens = &n
		case "temperature":
			err = decode(raw, &r.Temperature)
		case "top_p":
			err = decode(raw, &r.TopP)
		case "presence_penalty":
			r.PresencePenalty, err = nonZero(raw)
		case "frequency_penalty":
			r.FrequencyPenalty, err = nonZero(raw)
		case "stop":
			r.Stop, err = stringOrList(raw)
		case "stream":
			err = decode(raw, &r.Stream)
		case "stream_options":
			// include_obfuscation pads OpenAI's chunks against side channels;
			// translated streams aren't OpenAI's, so it has no effect.
			var o struct {
				IncludeUsage bool `json:"include_usage"`
			}
			err = json.Unmarshal(raw, &o)
			r.IncludeUsage = o.IncludeUsage
		case "n":
			if err = decode(raw, &r.N); err == nil && r.N < 1 {
				return nil, errorf(key, "n must be at least 1.")
			}
		case "seed":
			err = decode(raw, &r.Seed)
		case "tools":
			err = r.parseTools(raw)
		case "tool_choice":
			r.ToolChoice, err = parseToolChoice(raw)
		case "parallel_tool_calls":
			err = decode(raw, &r.ParallelToolCalls)
		case "response_format":
			r.Format, err = parseFormat(raw)
		case "reasoning_effort":
			err = decode(raw, &r.Effort)
		case "user", "safety_identifier":
			var user string
			if err = decode(raw, &user); err == nil && user != "" {
				r.User = user
			}
		case "store":
			err = decode(raw, &store)
		case "service_tier":
			var tier string
			if err = decode(raw, &tier); err == nil && tier != "auto" && tier != "default" {
				return nil, errorf(key, "The service tier %q exists only at OpenAI.", tier)
			}
		case "logprobs":
			var on bool
			if err = decode(raw, &on); err == nil && on {
				return nil, errorf(key, "Log probabilities can't be translated to other APIs.")
			}
		case "top_logprobs":
			return nil, errorf(key, "Log probabilities can't be translated to other APIs.")
		case "logit_bias":
			var bias map[string]json.RawMessage
			if err = decode(raw, &bias); err == nil && len(bias) > 0 {
				return nil, errorf(key, "logit_bias works only with OpenAI's tokenizer.")
			}
		case "modalities":
			var m []string
			if err = decode(raw, &m); err == nil && slices.ContainsFunc(m, func(s string) bool { return s != "text" }) {
				return nil, errorf(key, "Only text output can be translated.")
			}
		case "metadata", "prompt_cache_key", "prompt_cache_retention", "prompt_cache_options":
			// Metadata only labels stored completions, and the others steer
			// OpenAI's prompt cache: they don't change the answer.
		default:
			return nil, errorf(key, "The option %q can't be translated to this provider's API.", key)
		}
		var e *Error
		switch {
		case errors.As(err, &e):
			return nil, e
		case err != nil:
			return nil, errorf(key, "Invalid %s: %v.", key, err)
		}
	}
	switch {
	case store:
		return nil, errorf("store", "Only OpenAI stores completions.")
	case len(r.Messages) == 0:
		return nil, errorf("messages", "messages must list at least one message.")
	}
	return r, nil
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func decode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// nonZero reads a number that is unset when it's zero.
func nonZero(raw json.RawMessage) (*float64, error) {
	var v float64
	if err := decode(raw, &v); err != nil || v == 0 {
		return nil, err
	}
	return &v, nil
}

func stringOrList(raw json.RawMessage) ([]string, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}, nil
	}
	var list []string
	err := decode(raw, &list)
	return list, err
}

// openAIMessage is a message as OpenAI's request carries it.
type openAIMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       *string         `json:"name"`
	ToolCalls  []openAICall    `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
	Refusal    *string         `json:"refusal"`
	// Unsupported fields, which must be absent.
	FunctionCall json.RawMessage `json:"function_call"`
	Audio        json.RawMessage `json:"audio"`
}

type openAICall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
	ExtraContent *struct {
		Google struct {
			ThoughtSignature string `json:"thought_signature"`
		} `json:"google"`
	} `json:"extra_content"`
}

func (r *Request) parseMessages(raw json.RawMessage) error {
	var msgs []openAIMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return err
	}
	calls := map[string]bool{}
	for i, m := range msgs {
		field := fmt.Sprintf("messages[%d]", i)
		switch {
		case !slices.Contains([]string{"system", "developer", "user", "assistant", "tool"}, m.Role):
			return errorf(field+".role", "The role %q can't be translated.", m.Role)
		case m.Name != nil && m.Role != "tool":
			return errorf(field+".name", "Names of message authors can't be translated.")
		case len(m.FunctionCall) > 0 && !isNull(m.FunctionCall):
			return errorf(field+".function_call", "function_call is deprecated; use tool_calls.")
		case len(m.Audio) > 0 && !isNull(m.Audio):
			return errorf(field+".audio", "Audio can't be translated.")
		}
		parts, err := parseContent(field+".content", m.Content, m.Role == "user")
		if err != nil {
			return err
		}
		switch m.Role {
		case "system", "developer":
			for _, p := range parts {
				r.System = append(r.System, p.Text)
			}
			continue
		case "user":
		case "assistant":
			if len(parts) == 0 && m.Refusal != nil && *m.Refusal != "" {
				parts = []Part{{Text: *m.Refusal}}
			}
		case "tool":
			if !calls[m.ToolCallID] {
				return errorf(field+".tool_call_id", "tool_call_id %q doesn't match a tool call of an earlier "+
					"assistant message.", m.ToolCallID)
			}
		}
		msg := Message{Role: m.Role, Content: parts, ToolCallID: m.ToolCallID}
		for j, c := range m.ToolCalls {
			if c.Type != "function" {
				return errorf(fmt.Sprintf("%s.tool_calls[%d].type", field, j), "Only function calls can be translated.")
			}
			call := ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: c.Function.Arguments}
			if c.ExtraContent != nil {
				call.Signature = c.ExtraContent.Google.ThoughtSignature
			}
			calls[c.ID] = true
			msg.ToolCalls = append(msg.ToolCalls, call)
		}
		r.Messages = append(r.Messages, msg)
	}
	return nil
}

// parseContent reads a message's content: a string, or a list of parts.
// Images are allowed in user messages only, as OpenAI allows them.
func parseContent(field string, raw json.RawMessage, images bool) ([]Part, error) {
	if len(raw) == 0 || isNull(raw) {
		return nil, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []Part{{Text: s}}, nil
	}
	var list []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Refusal  string `json:"refusal"`
		ImageURL *struct {
			URL string `json:"url"`
			// Detail picks OpenAI's image resolution; the other APIs pick
			// their own, so it has no effect.
			Detail string `json:"detail"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, errorf(field, "%s must be a string or a list of parts.", field)
	}
	var parts []Part
	for i, p := range list {
		switch {
		case p.Type == "text":
			parts = append(parts, Part{Text: p.Text})
		case p.Type == "refusal":
			parts = append(parts, Part{Text: p.Refusal})
		case p.Type == "image_url" && images && p.ImageURL != nil:
			parts = append(parts, Part{ImageURL: p.ImageURL.URL})
		default:
			return nil, errorf(fmt.Sprintf("%s[%d]", field, i), "Content parts of the type %q can't be translated.", p.Type)
		}
	}
	return parts, nil
}

func (r *Request) parseTools(raw json.RawMessage) error {
	var tools []struct {
		Type     string `json:"type"`
		Function *struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
			Strict      *bool           `json:"strict"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		return err
	}
	for i, t := range tools {
		if t.Type != "function" || t.Function == nil || t.Function.Name == "" {
			return errorf(fmt.Sprintf("tools[%d]", i), "Only function tools with a name can be translated.")
		}
		tool := Tool{Name: t.Function.Name, Description: t.Function.Description, Strict: t.Function.Strict != nil &&
			*t.Function.Strict}
		if len(t.Function.Parameters) > 0 && !isNull(t.Function.Parameters) {
			tool.Parameters = t.Function.Parameters
		}
		r.Tools = append(r.Tools, tool)
	}
	return nil
}

func parseToolChoice(raw json.RawMessage) (*ToolChoice, error) {
	var mode string
	if json.Unmarshal(raw, &mode) == nil {
		if mode != "auto" && mode != "none" && mode != "required" {
			return nil, fmt.Errorf("unknown value %q", mode)
		}
		return &ToolChoice{Mode: mode}, nil
	}
	var c struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &c); err != nil || c.Type != "function" || c.Function.Name == "" {
		return nil, fmt.Errorf("only auto, none, required or a function can be translated")
	}
	return &ToolChoice{Mode: "function", Name: c.Function.Name}, nil
}

func parseFormat(raw json.RawMessage) (*Format, error) {
	var f struct {
		Type       string `json:"type"`
		JSONSchema *struct {
			Name   string          `json:"name"`
			Schema json.RawMessage `json:"schema"`
			Strict *bool           `json:"strict"`
		} `json:"json_schema"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	switch {
	case f.Type == "text":
		return nil, nil
	case f.Type == "json_object":
		return &Format{Type: f.Type}, nil
	case f.Type == "json_schema" && f.JSONSchema != nil && len(f.JSONSchema.Schema) > 0:
		return &Format{Type: f.Type, Name: f.JSONSchema.Name, Schema: f.JSONSchema.Schema,
			Strict: f.JSONSchema.Strict != nil && *f.JSONSchema.Strict}, nil
	}
	return nil, fmt.Errorf("the type %q can't be translated", f.Type)
}

// dataURL splits a data: URL of base64 data into its media type and data.
func dataURL(url string) (mediaType, data string, ok bool) {
	rest, ok := strings.CutPrefix(url, "data:")
	if !ok {
		return "", "", false
	}
	meta, data, ok := strings.Cut(rest, ",")
	mediaType, base64 := strings.CutSuffix(meta, ";base64")
	return mediaType, data, ok && base64 && mediaType != ""
}

// toolNames maps each tool call's ID to its function's name.
func (r *Request) toolNames() map[string]string {
	names := map[string]string{}
	for _, m := range r.Messages {
		for _, c := range m.ToolCalls {
			names[c.ID] = c.Name
		}
	}
	return names
}
