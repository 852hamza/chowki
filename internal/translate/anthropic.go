package translate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/852hamza/chowki/internal/usage"
)

// The Anthropic Messages API, as documented in
// https://platform.claude.com/docs/en/api/messages.

// AnthropicVersion is the version of the Messages API that translated
// requests ask for, in the anthropic-version header.
const AnthropicVersion = "2023-06-01"

type anthropicRequest struct {
	Model         string                 `json:"model"`
	MaxTokens     int64                  `json:"max_tokens"`
	System        []json.RawMessage      `json:"system,omitempty"`
	Messages      []anthropicMessage     `json:"messages"`
	Tools         []anthropicTool        `json:"tools,omitempty"`
	ToolChoice    map[string]any         `json:"tool_choice,omitempty"`
	Temperature   *float64               `json:"temperature,omitempty"`
	TopP          *float64               `json:"top_p,omitempty"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	Stream        bool                   `json:"stream,omitempty"`
	Metadata      *anthropicMetadata     `json:"metadata,omitempty"`
	Thinking      *anthropicThinking     `json:"thinking,omitempty"`
	OutputConfig  *anthropicOutputConfig `json:"output_config,omitempty"`
}

type anthropicMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
	Strict      bool            `json:"strict,omitempty"`
}

type anthropicMetadata struct {
	UserID string `json:"user_id"`
}

type anthropicThinking struct {
	Type string `json:"type"`
}

type anthropicOutputConfig struct {
	Effort string         `json:"effort,omitempty"`
	Format map[string]any `json:"format,omitempty"`
}

// anthropicEfforts are the reasoning efforts that Anthropic knows too.
var anthropicEfforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// anthropicImageTypes are the image types that Anthropic accepts.
var anthropicImageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}

func block(v any) json.RawMessage {
	b, _ := json.Marshal(v) // blocks are maps of plain values, which always marshal
	return b
}

func textBlock(text string) json.RawMessage {
	return block(map[string]any{"type": "text", "text": text})
}

// ToAnthropic translates r into a Messages API request for model, with
// maxTokens, which Anthropic requires, when the request sets none. Options
// that Anthropic can't honor are errors.
func ToAnthropic(r *Request, model string, maxTokens int64, mem *Memory) ([]byte, error) {
	out := anthropicRequest{Model: model, MaxTokens: maxTokens, Temperature: r.Temperature, TopP: r.TopP,
		StopSequences: r.Stop, Stream: r.Stream}
	switch {
	case r.N > 1:
		return nil, errorf("n", "Anthropic returns one choice: n must be 1.")
	case r.Seed != nil:
		return nil, errorf("seed", "Anthropic doesn't take a seed.")
	case r.PresencePenalty != nil:
		return nil, errorf("presence_penalty", "Anthropic doesn't take a presence penalty.")
	case r.FrequencyPenalty != nil:
		return nil, errorf("frequency_penalty", "Anthropic doesn't take a frequency penalty.")
	case r.Temperature != nil && (*r.Temperature < 0 || *r.Temperature > 1):
		return nil, errorf("temperature", "Anthropic takes a temperature from 0 to 1.")
	}
	if r.MaxTokens != nil {
		out.MaxTokens = *r.MaxTokens
	}
	if r.User != "" {
		out.Metadata = &anthropicMetadata{UserID: r.User}
	}
	switch {
	case r.Effort == "":
	case anthropicEfforts[r.Effort]:
		out.OutputConfig = &anthropicOutputConfig{Effort: r.Effort}
	case r.Effort == "none":
		out.Thinking = &anthropicThinking{Type: "disabled"}
	default:
		return nil, errorf("reasoning_effort", "Anthropic has no reasoning effort %q: use none, low, medium, high, "+
			"xhigh or max.", r.Effort)
	}
	if f := r.Format; f != nil {
		if f.Type != "json_schema" {
			return nil, errorf("response_format", "Anthropic returns JSON only for a schema: use json_schema.")
		}
		if out.OutputConfig == nil {
			out.OutputConfig = &anthropicOutputConfig{}
		}
		out.OutputConfig.Format = map[string]any{"type": "json_schema", "schema": f.Schema}
	}
	for _, s := range r.System {
		out.System = append(out.System, textBlock(s))
	}
	for _, t := range r.Tools {
		schema := t.Parameters
		if schema == nil {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		out.Tools = append(out.Tools, anthropicTool{Name: t.Name, Description: t.Description, InputSchema: schema,
			Strict: t.Strict})
	}
	out.ToolChoice = anthropicToolChoice(r)
	messages, err := anthropicMessages(r, mem)
	if err != nil {
		return nil, err
	}
	out.Messages = messages
	return json.Marshal(out)
}

func anthropicToolChoice(r *Request) map[string]any {
	var choice map[string]any
	if c := r.ToolChoice; c != nil {
		switch c.Mode {
		case "auto":
			choice = map[string]any{"type": "auto"}
		case "none":
			choice = map[string]any{"type": "none"}
		case "required":
			choice = map[string]any{"type": "any"}
		case "function":
			choice = map[string]any{"type": "tool", "name": c.Name}
		}
	}
	if p := r.ParallelToolCalls; p != nil && !*p && len(r.Tools) > 0 {
		if choice == nil {
			choice = map[string]any{"type": "auto"}
		}
		if choice["type"] != "none" {
			choice["disable_parallel_tool_use"] = true
		}
	}
	return choice
}

// anthropicMessages translates the conversation. Tool results become
// tool_result blocks of a user message, and messages in a row from the same
// role become one, as Anthropic wants the roles to alternate. Tool results
// come first in their message, as Anthropic requires.
func anthropicMessages(r *Request, mem *Memory) ([]anthropicMessage, error) {
	type turn struct {
		role            string
		results, others []json.RawMessage
	}
	var turns []turn
	add := func(role string, results, others []json.RawMessage) {
		if n := len(turns); n > 0 && turns[n-1].role == role {
			turns[n-1].results = append(turns[n-1].results, results...)
			turns[n-1].others = append(turns[n-1].others, others...)
			return
		}
		turns = append(turns, turn{role, results, others})
	}
	for i, m := range r.Messages {
		field := fmt.Sprintf("messages[%d]", i)
		switch m.Role {
		case "user":
			blocks, err := anthropicContent(field+".content", m.Content)
			if err != nil {
				return nil, err
			}
			add("user", nil, blocks)
		case "tool":
			result := map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID}
			switch len(m.Content) {
			case 0:
			case 1:
				result["content"] = m.Content[0].Text
			default:
				var content []json.RawMessage
				for _, p := range m.Content {
					content = append(content, textBlock(p.Text))
				}
				result["content"] = content
			}
			add("user", []json.RawMessage{block(result)}, nil)
		case "assistant":
			blocks, err := anthropicAssistant(field, m, mem)
			if err != nil {
				return nil, err
			}
			add("assistant", nil, blocks)
		}
	}
	out := make([]anthropicMessage, 0, len(turns))
	for _, t := range turns {
		out = append(out, anthropicMessage{Role: t.role, Content: append(t.results, t.others...)})
	}
	return out, nil
}

func anthropicContent(field string, parts []Part) ([]json.RawMessage, error) {
	var blocks []json.RawMessage
	for i, p := range parts {
		if p.ImageURL == "" {
			blocks = append(blocks, textBlock(p.Text))
			continue
		}
		mediaType, data, ok := dataURL(p.ImageURL)
		switch {
		case ok && anthropicImageTypes[mediaType]:
			blocks = append(blocks, block(map[string]any{"type": "image", "source": map[string]any{
				"type": "base64", "media_type": mediaType, "data": data}}))
		case ok:
			return nil, errorf(fmt.Sprintf("%s[%d]", field, i), "Anthropic takes JPEG, PNG, GIF and WebP images, "+
				"not %s.", mediaType)
		case strings.HasPrefix(p.ImageURL, "https://") || strings.HasPrefix(p.ImageURL, "http://"):
			blocks = append(blocks, block(map[string]any{"type": "image", "source": map[string]any{
				"type": "url", "url": p.ImageURL}}))
		default:
			return nil, errorf(fmt.Sprintf("%s[%d]", field, i), "An image must be a base64 data: URL or an http(s) URL.")
		}
	}
	return blocks, nil
}

// anthropicAssistant translates an assistant message. When Memory has the
// thinking of the answer that made its tool calls, the blocks go back in
// their original order, as Anthropic requires within a tool-use loop.
func anthropicAssistant(field string, m Message, mem *Memory) ([]json.RawMessage, error) {
	var text []json.RawMessage
	for _, p := range m.Content {
		if p.Text != "" {
			text = append(text, textBlock(p.Text))
		}
	}
	calls := map[string]json.RawMessage{}
	var order []string
	for j, c := range m.ToolCalls {
		input := json.RawMessage(c.Arguments)
		if strings.TrimSpace(c.Arguments) == "" {
			input = json.RawMessage(`{}`)
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(input, &obj) != nil {
			return nil, errorf(fmt.Sprintf("%s.tool_calls[%d].function.arguments", field, j),
				"The arguments of a tool call must be a JSON object.")
		}
		calls[c.ID] = block(map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": input})
		order = append(order, c.ID)
	}
	var saved *memo
	if len(order) > 0 {
		saved = mem.get(order[0])
	}
	if saved == nil {
		out := text
		for _, id := range order {
			out = append(out, calls[id])
		}
		return out, nil
	}
	var out []json.RawMessage
	for _, it := range saved.items {
		switch {
		case it.thinking != nil:
			out = append(out, it.thinking)
		case it.text:
			out = append(out, text...)
			text = nil
		case calls[it.toolUse] != nil:
			out = append(out, calls[it.toolUse])
			delete(calls, it.toolUse)
		}
	}
	out = append(out, text...)
	for _, id := range order { // calls that the answer didn't have
		if calls[id] != nil {
			out = append(out, calls[id])
		}
	}
	return out, nil
}

// anthropicResponse is a Messages API response, as translation reads it.
type anthropicResponse struct {
	ID         string            `json:"id"`
	Model      string            `json:"model"`
	Content    []json.RawMessage `json:"content"`
	StopReason string            `json:"stop_reason"`
}

type anthropicBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// anthropicFinish maps a stop reason to OpenAI's finish reasons.
func anthropicFinish(reason string) string {
	switch reason {
	case "max_tokens", "model_context_window_exceeded":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "refusal":
		return "content_filter"
	}
	return "stop"
}

// FromAnthropic translates a Messages API response into a chat completion
// created at the given Unix time, and keeps its thinking in Memory for the
// next request of a tool-use loop.
func FromAnthropic(body []byte, created int64, mem *Memory) ([]byte, error) {
	var r anthropicResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("translate Anthropic response: %w", err)
	}
	report, _ := usage.ParseResponse(usage.Anthropic, body) // the body parsed already
	var text strings.Builder
	msg := &message{Role: "assistant"}
	var items []item
	var ids []string
	for _, raw := range r.Content {
		var b anthropicBlock
		if json.Unmarshal(raw, &b) != nil {
			continue
		}
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
			items = append(items, item{text: true})
		case "tool_use":
			input := string(b.Input)
			if input == "" {
				input = "{}"
			}
			msg.ToolCalls = append(msg.ToolCalls, outCall{ID: b.ID, Type: "function",
				Function: outFunction{Name: b.Name, Arguments: input}})
			items = append(items, item{toolUse: b.ID})
			ids = append(ids, b.ID)
		case "thinking", "redacted_thinking":
			items = append(items, item{thinking: raw})
		}
	}
	if text.Len() > 0 || len(msg.ToolCalls) == 0 {
		msg.Content = ptr(text.String())
	}
	rememberThinking(mem, ids, items)
	return marshal(completion{ID: "chatcmpl-" + r.ID, Object: "chat.completion", Created: created,
		Model: r.Model, Choices: []choice{{Message: msg, FinishReason: ptr(anthropicFinish(r.StopReason))}},
		Usage: openAIUsage(report.Usage)})
}

// rememberThinking keeps the thinking of an answer with tool calls.
func rememberThinking(mem *Memory, ids []string, items []item) {
	for _, it := range items {
		if it.thinking != nil {
			mem.put(&memo{ids: ids, items: items})
			return
		}
	}
}

// AnthropicStream translates a Messages API stream into chat completion
// chunks.
type AnthropicStream struct {
	w            chunkWriter
	includeUsage bool
	mem          *Memory
	report       *usage.Stream
	started      bool
	// tools maps the index of a tool_use block to the index of its call.
	tools map[int]int
	// blocks are the answer's content blocks, for Memory.
	blocks map[int]*streamBlock
	order  []int
}

type streamBlock struct {
	kind      string // the block type
	id        string // of a tool_use
	thinking  strings.Builder
	signature strings.Builder
	data      string // of redacted_thinking
}

// NewAnthropicStream returns a translator of one stream, whose chunks are
// created at the given Unix time. includeUsage adds a final chunk with the
// usage, as stream_options.include_usage asks.
func NewAnthropicStream(created int64, includeUsage bool, mem *Memory) *AnthropicStream {
	return &AnthropicStream{w: chunkWriter{created: created}, includeUsage: includeUsage, mem: mem,
		report: usage.NewStream(usage.Anthropic), tools: map[int]int{}, blocks: map[int]*streamBlock{}}
}

// Event translates one event of the stream into the chunks to send, each
// the data of a server-sent event.
func (s *AnthropicStream) Event(name string, data []byte) [][]byte {
	s.report.Event(name, data)
	var e struct {
		Message *struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		} `json:"message"`
		Index        int             `json:"index"`
		ContentBlock json.RawMessage `json:"content_block"`
		Delta        struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
			Thinking    string `json:"thinking"`
			Signature   string `json:"signature"`
			StopReason  string `json:"stop_reason"`
		} `json:"delta"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) != nil {
		return nil
	}
	switch name {
	case "message_start":
		if e.Message == nil {
			return nil
		}
		s.w.id, s.w.model, s.started = "chatcmpl-"+e.Message.ID, e.Message.Model, true
		return [][]byte{s.w.delta(0, delta{Role: "assistant", Content: ptr("")}, nil)}
	case "content_block_start":
		return s.start(e.Index, e.ContentBlock)
	case "content_block_delta":
		b := s.blocks[e.Index]
		switch e.Delta.Type {
		case "text_delta":
			return [][]byte{s.w.delta(0, delta{Content: ptr(e.Delta.Text)}, nil)}
		case "input_json_delta":
			if e.Delta.PartialJSON == "" {
				return nil
			}
			i := s.tools[e.Index]
			return [][]byte{s.w.delta(0, delta{ToolCalls: []outCall{{Index: &i,
				Function: outFunction{Arguments: e.Delta.PartialJSON}}}}, nil)}
		case "thinking_delta":
			if b != nil {
				b.thinking.WriteString(e.Delta.Thinking)
			}
		case "signature_delta":
			if b != nil {
				b.signature.WriteString(e.Delta.Signature)
			}
		}
	case "message_delta":
		if e.Delta.StopReason != "" {
			return [][]byte{s.w.delta(0, delta{}, ptr(anthropicFinish(e.Delta.StopReason)))}
		}
	case "message_stop":
		s.remember()
	case "error":
		return [][]byte{errorChunk(e.Error.Message, openAIErrorType(e.Error.Type))}
	}
	return nil
}

func (s *AnthropicStream) start(index int, raw json.RawMessage) [][]byte {
	var b struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
		Text string `json:"text"`
		Data string `json:"data"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return nil
	}
	sb := &streamBlock{kind: b.Type, id: b.ID, data: b.Data}
	s.blocks[index] = sb
	s.order = append(s.order, index)
	switch b.Type {
	case "text":
		if b.Text != "" {
			return [][]byte{s.w.delta(0, delta{Content: ptr(b.Text)}, nil)}
		}
	case "tool_use":
		i := len(s.tools)
		s.tools[index] = i
		return [][]byte{s.w.delta(0, delta{ToolCalls: []outCall{{Index: &i, ID: b.ID, Type: "function",
			Function: outFunction{Name: b.Name}}}}, nil)}
	}
	return nil
}

// remember keeps the thinking of an answer with tool calls.
func (s *AnthropicStream) remember() {
	var items []item
	var ids []string
	for _, index := range s.order {
		b := s.blocks[index]
		switch b.kind {
		case "text":
			items = append(items, item{text: true})
		case "tool_use":
			items = append(items, item{toolUse: b.id})
			ids = append(ids, b.id)
		case "thinking":
			items = append(items, item{thinking: block(map[string]any{"type": "thinking",
				"thinking": b.thinking.String(), "signature": b.signature.String()})})
		case "redacted_thinking":
			items = append(items, item{thinking: block(map[string]any{"type": "redacted_thinking", "data": b.data})})
		}
	}
	rememberThinking(s.mem, ids, items)
}

// End returns the last chunks of the stream: the usage, when the client
// asked for it.
func (s *AnthropicStream) End() [][]byte {
	if !s.includeUsage || !s.started {
		return nil
	}
	return [][]byte{s.w.chunk(nil, openAIUsage(s.report.Report().Usage))}
}

// openAIErrorType maps an Anthropic error type to OpenAI's.
func openAIErrorType(anthropicType string) string {
	switch anthropicType {
	case "authentication_error", "permission_error", "rate_limit_error":
		return anthropicType
	case "api_error", "overloaded_error", "timeout_error":
		return "server_error"
	}
	return "invalid_request_error"
}
