package translate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/852hamza/chowki/internal/usage"
)

// The Gemini API's generateContent, as the discovery document at
// https://generativelanguage.googleapis.com/$discovery/rest?version=v1beta
// describes it.

type geminiRequest struct {
	Contents          []geminiContent   `json:"contents"`
	SystemInstruction *geminiContent    `json:"systemInstruction,omitempty"`
	Tools             []geminiTool      `json:"tools,omitempty"`
	ToolConfig        *geminiToolConfig `json:"toolConfig,omitempty"`
	GenerationConfig  *geminiConfig     `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string            `json:"role,omitempty"`
	Parts []json.RawMessage `json:"parts"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunction `json:"functionDeclarations"`
}

type geminiFunction struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	ParametersJSONSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

type geminiToolConfig struct {
	FunctionCallingConfig geminiCalling `json:"functionCallingConfig"`
}

type geminiCalling struct {
	Mode                 string   `json:"mode"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type geminiConfig struct {
	MaxOutputTokens    *int64          `json:"maxOutputTokens,omitempty"`
	Temperature        *float64        `json:"temperature,omitempty"`
	TopP               *float64        `json:"topP,omitempty"`
	StopSequences      []string        `json:"stopSequences,omitempty"`
	CandidateCount     int64           `json:"candidateCount,omitempty"`
	Seed               *int64          `json:"seed,omitempty"`
	PresencePenalty    *float64        `json:"presencePenalty,omitempty"`
	FrequencyPenalty   *float64        `json:"frequencyPenalty,omitempty"`
	ResponseMimeType   string          `json:"responseMimeType,omitempty"`
	ResponseJSONSchema json.RawMessage `json:"responseJsonSchema,omitempty"`
	ThinkingConfig     map[string]any  `json:"thinkingConfig,omitempty"`
}

// geminiStopSequences is how many stop sequences Gemini takes.
const geminiStopSequences = 5

// SkipSignature is the placeholder thought signature that Google documents
// for a function call whose signature is lost: it skips the validation that
// Gemini 3 applies, at some cost in quality. See the FAQ of
// https://ai.google.dev/gemini-api/docs/generate-content/thought-signatures.
const SkipSignature = "skip_thought_signature_validator"

// geminiGeneration returns the major version in a Gemini model's name, such
// as 3 for gemini-3.1-flash-lite, or 0 when the name has none.
func geminiGeneration(model string) int {
	rest, ok := strings.CutPrefix(model, "gemini-")
	n := 0
	for _, c := range rest {
		if !ok || c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// geminiThinking maps a reasoning effort to Gemini's thinking settings, as
// https://ai.google.dev/gemini-api/docs/openai documents it: a budget of
// tokens for Gemini 2.5, and a level for later models, which can't turn
// thinking off.
func geminiThinking(model, effort string) (map[string]any, error) {
	if geminiGeneration(model) == 2 {
		budget, ok := map[string]int{"none": 0, "minimal": 1024, "low": 1024, "medium": 8192, "high": 24576}[effort]
		if !ok {
			return nil, errorf("reasoning_effort", "Gemini 2.5 takes a reasoning effort of none, minimal, low, "+
				"medium or high.")
		}
		return map[string]any{"thinkingBudget": budget}, nil
	}
	level, ok := map[string]string{"minimal": "MINIMAL", "low": "LOW", "medium": "MEDIUM", "high": "HIGH"}[effort]
	if !ok {
		return nil, errorf("reasoning_effort", "Gemini takes a reasoning effort of minimal, low, medium or high.")
	}
	if level == "MINIMAL" && strings.Contains(model, "-pro") {
		level = "LOW" // Pro models have no minimal level
	}
	return map[string]any{"thinkingLevel": level}, nil
}

// ToGemini translates r into a generateContent request for model; the
// model goes in the path. Options that Gemini can't honor are errors.
func ToGemini(r *Request, model string, mem *Memory) ([]byte, error) {
	cfg := &geminiConfig{MaxOutputTokens: r.MaxTokens, Temperature: r.Temperature, TopP: r.TopP,
		StopSequences: r.Stop, Seed: r.Seed, PresencePenalty: r.PresencePenalty, FrequencyPenalty: r.FrequencyPenalty}
	if r.N > 1 {
		cfg.CandidateCount = r.N
	}
	switch {
	case len(r.Stop) > geminiStopSequences:
		return nil, errorf("stop", "Gemini takes up to %d stop sequences.", geminiStopSequences)
	case r.ParallelToolCalls != nil && !*r.ParallelToolCalls && len(r.Tools) > 0:
		return nil, errorf("parallel_tool_calls", "Gemini can't be kept from calling tools in parallel.")
	}
	if f := r.Format; f != nil {
		cfg.ResponseMimeType = "application/json"
		cfg.ResponseJSONSchema = f.Schema
	}
	if r.Effort != "" {
		thinking, err := geminiThinking(model, r.Effort)
		if err != nil {
			return nil, err
		}
		cfg.ThinkingConfig = thinking
	}
	out := geminiRequest{}
	if !reflect.ValueOf(*cfg).IsZero() {
		out.GenerationConfig = cfg
	}
	if len(r.System) > 0 {
		out.SystemInstruction = &geminiContent{}
		for _, s := range r.System {
			out.SystemInstruction.Parts = append(out.SystemInstruction.Parts, block(map[string]any{"text": s}))
		}
	}
	strict := false
	if len(r.Tools) > 0 {
		tool := geminiTool{}
		for _, t := range r.Tools {
			tool.FunctionDeclarations = append(tool.FunctionDeclarations, geminiFunction{Name: t.Name,
				Description: t.Description, ParametersJSONSchema: t.Parameters})
			strict = strict || t.Strict
		}
		out.Tools = []geminiTool{tool}
	}
	calling, err := geminiToolChoice(r, strict)
	if err != nil {
		return nil, err
	}
	if calling != nil {
		out.ToolConfig = &geminiToolConfig{FunctionCallingConfig: *calling}
	}
	if out.Contents, err = geminiContents(r, model, mem); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// geminiToolChoice maps the tool choice. Strict tools become Gemini's
// VALIDATED mode, which checks calls against their schemas.
func geminiToolChoice(r *Request, strict bool) (*geminiCalling, error) {
	mode := ""
	if r.ToolChoice != nil {
		mode = r.ToolChoice.Mode
	}
	switch mode {
	case "none":
		return &geminiCalling{Mode: "NONE"}, nil
	case "required", "function":
		if strict {
			return nil, errorf("tool_choice", "Gemini can't force a tool call and check it against a strict schema: "+
				"drop strict or tool_choice.")
		}
		c := &geminiCalling{Mode: "ANY"}
		if mode == "function" {
			c.AllowedFunctionNames = []string{r.ToolChoice.Name}
		}
		return c, nil
	}
	switch {
	case strict:
		return &geminiCalling{Mode: "VALIDATED"}, nil
	case mode == "auto":
		return &geminiCalling{Mode: "AUTO"}, nil
	}
	return nil, nil
}

// geminiContents translates the conversation. Tool results become
// functionResponse parts of a user turn, after all the calls, and turns in
// a row from the same role become one.
func geminiContents(r *Request, model string, mem *Memory) ([]geminiContent, error) {
	names := r.toolNames()
	var out []geminiContent
	add := func(role string, parts []json.RawMessage) {
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Parts = append(out[n-1].Parts, parts...)
			return
		}
		out = append(out, geminiContent{Role: role, Parts: parts})
	}
	needsSignature := geminiGeneration(model) >= 3 || geminiGeneration(model) == 0
	for i, m := range r.Messages {
		field := fmt.Sprintf("messages[%d]", i)
		switch m.Role {
		case "user":
			var parts []json.RawMessage
			for j, p := range m.Content {
				if p.ImageURL == "" {
					parts = append(parts, block(map[string]any{"text": p.Text}))
					continue
				}
				mediaType, data, ok := dataURL(p.ImageURL)
				if !ok {
					return nil, errorf(fmt.Sprintf("%s.content[%d]", field, j),
						"Gemini takes images as base64 data: URLs.")
				}
				parts = append(parts, block(map[string]any{"inlineData": map[string]any{"mimeType": mediaType,
					"data": data}}))
			}
			add("user", parts)
		case "tool":
			var text strings.Builder
			for _, p := range m.Content {
				text.WriteString(p.Text)
			}
			// Gemini wants an object; a result that isn't one goes in output.
			response := json.RawMessage(text.String())
			var obj map[string]json.RawMessage
			if json.Unmarshal(response, &obj) != nil || obj == nil {
				response = block(map[string]any{"output": text.String()})
			}
			add("user", []json.RawMessage{block(map[string]any{"functionResponse": map[string]any{
				"name": names[m.ToolCallID], "response": response}})})
		case "assistant":
			var parts []json.RawMessage
			for _, p := range m.Content {
				if p.Text != "" {
					parts = append(parts, block(map[string]any{"text": p.Text}))
				}
			}
			for j, c := range m.ToolCalls {
				args := json.RawMessage(c.Arguments)
				if strings.TrimSpace(c.Arguments) == "" {
					args = json.RawMessage(`{}`)
				}
				var obj map[string]json.RawMessage
				if json.Unmarshal(args, &obj) != nil {
					return nil, errorf(fmt.Sprintf("%s.tool_calls[%d].function.arguments", field, j),
						"The arguments of a tool call must be a JSON object.")
				}
				part := map[string]any{"functionCall": map[string]any{"name": c.Name, "args": args}}
				sig := c.Signature
				if saved := mem.get(c.ID); sig == "" && saved != nil {
					sig = saved.signature
				}
				// Gemini 3 wants the signature of the first call of each step.
				if sig == "" && j == 0 && needsSignature {
					sig = SkipSignature
				}
				if sig != "" {
					part["thoughtSignature"] = sig
				}
				parts = append(parts, block(part))
			}
			if len(parts) > 0 {
				add("model", parts)
			}
		}
	}
	return out, nil
}

// geminiResponse is a generateContent response, or a chunk of a stream.
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
		Index        int    `json:"index"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	ModelVersion string `json:"modelVersion"`
	ResponseID   string `json:"responseId"`
}

type geminiPart struct {
	Text             *string `json:"text"`
	Thought          bool    `json:"thought"`
	ThoughtSignature string  `json:"thoughtSignature"`
	FunctionCall     *struct {
		ID   string          `json:"id"`
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"functionCall"`
}

// geminiFinish maps a finish reason to OpenAI's, which says tool_calls when
// the answer calls tools.
func geminiFinish(reason string, calls bool) *string {
	switch reason {
	case "":
		return nil
	case "MAX_TOKENS":
		return ptr("length")
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY",
		"IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION":
		return ptr("content_filter")
	}
	if calls {
		return ptr("tool_calls")
	}
	return ptr("stop")
}

// geminiCallID returns the ID of the nth call of a candidate in a response
// whose calls have none: derived from the response, so that a stream and a
// retry name a call alike, and short, as OpenAI takes IDs of up to 40
// characters.
func geminiCallID(responseID string, candidate, n int) string {
	if responseID == "" {
		return callID()
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s/%d/%d", responseID, candidate, n))
	return "call_" + hex.EncodeToString(sum[:12])
}

// geminiCall translates a function call, and keeps its thought signature
// in Memory for the next request of a tool-use loop.
func geminiCall(p geminiPart, id string, mem *Memory) outCall {
	if p.FunctionCall.ID != "" {
		id = p.FunctionCall.ID
	}
	args := string(p.FunctionCall.Args)
	if args == "" || args == "null" {
		args = "{}"
	}
	call := outCall{ID: id, Type: "function", Function: outFunction{Name: p.FunctionCall.Name, Arguments: args}}
	if p.ThoughtSignature != "" {
		call.ExtraContent = &extraContent{}
		call.ExtraContent.Google.ThoughtSignature = p.ThoughtSignature
		mem.put(&memo{ids: []string{id}, signature: p.ThoughtSignature})
	}
	return call
}

// FromGemini translates a generateContent response for model into a chat
// completion created at the given Unix time.
func FromGemini(body []byte, model string, created int64, mem *Memory) ([]byte, error) {
	var r geminiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("translate Gemini response: %w", err)
	}
	report, _ := usage.ParseResponse(usage.Gemini, body) // the body parsed already
	out := completion{ID: "chatcmpl-" + r.ResponseID, Object: "chat.completion", Created: created,
		Model: cmpOr(r.ModelVersion, model), Choices: []choice{}, Usage: openAIUsage(report.Usage)}
	if r.ResponseID == "" {
		out.ID = "chatcmpl-" + strings.TrimPrefix(callID(), "call_")
	}
	for _, c := range r.Candidates {
		var text strings.Builder
		msg := &message{Role: "assistant"}
		for _, p := range c.Content.Parts {
			switch {
			case p.FunctionCall != nil:
				msg.ToolCalls = append(msg.ToolCalls, geminiCall(p, geminiCallID(r.ResponseID, c.Index,
					len(msg.ToolCalls)), mem))
			case p.Text != nil && !p.Thought:
				text.WriteString(*p.Text)
			}
		}
		if text.Len() > 0 || len(msg.ToolCalls) == 0 {
			msg.Content = ptr(text.String())
		}
		finish := geminiFinish(c.FinishReason, len(msg.ToolCalls) > 0)
		if finish == nil {
			finish = ptr("stop")
		}
		out.Choices = append(out.Choices, choice{Index: c.Index, Message: msg, FinishReason: finish})
	}
	if len(out.Choices) == 0 && r.PromptFeedback != nil && r.PromptFeedback.BlockReason != "" {
		// Gemini blocked the prompt: an answer without content.
		out.Choices = append(out.Choices, choice{Message: &message{Role: "assistant"},
			FinishReason: ptr("content_filter")})
	}
	return json.Marshal(out)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// GeminiStream translates a streamGenerateContent stream into chat
// completion chunks.
type GeminiStream struct {
	w            chunkWriter
	includeUsage bool
	mem          *Memory
	report       *usage.Stream
	// calls counts the tool calls of each candidate so far; a candidate
	// has started when it's here.
	calls      map[int]int
	responseID string
}

// NewGeminiStream returns a translator of one stream for model, whose
// chunks are created at the given Unix time. includeUsage adds a final
// chunk with the usage, as stream_options.include_usage asks.
func NewGeminiStream(model string, created int64, includeUsage bool, mem *Memory) *GeminiStream {
	return &GeminiStream{w: chunkWriter{model: model, created: created}, includeUsage: includeUsage, mem: mem,
		report: usage.NewStream(usage.Gemini), calls: map[int]int{}}
}

// Event translates one chunk of the stream into the chunks to send, each
// the data of a server-sent event.
func (s *GeminiStream) Event(_ string, data []byte) [][]byte {
	s.report.Event("", data)
	var r geminiResponse
	if json.Unmarshal(data, &r) != nil {
		return nil
	}
	if r.ModelVersion != "" {
		s.w.model = r.ModelVersion
	}
	if s.w.id == "" {
		s.responseID = r.ResponseID
		s.w.id = "chatcmpl-" + r.ResponseID
		if r.ResponseID == "" {
			s.w.id = "chatcmpl-" + strings.TrimPrefix(callID(), "call_")
		}
	}
	var out [][]byte
	for _, c := range r.Candidates {
		if _, started := s.calls[c.Index]; !started {
			s.calls[c.Index] = 0
			out = append(out, s.w.delta(c.Index, delta{Role: "assistant", Content: ptr("")}, nil))
		}
		for _, p := range c.Content.Parts {
			switch {
			case p.FunctionCall != nil:
				i := s.calls[c.Index]
				call := geminiCall(p, geminiCallID(s.responseID, c.Index, i), s.mem)
				s.calls[c.Index]++
				call.Index = &i
				out = append(out, s.w.delta(c.Index, delta{ToolCalls: []outCall{call}}, nil))
			case p.Text != nil && !p.Thought && *p.Text != "":
				out = append(out, s.w.delta(c.Index, delta{Content: p.Text}, nil))
			}
		}
		if finish := geminiFinish(c.FinishReason, s.calls[c.Index] > 0); finish != nil {
			out = append(out, s.w.delta(c.Index, delta{}, finish))
		}
	}
	if len(r.Candidates) == 0 && r.PromptFeedback != nil && r.PromptFeedback.BlockReason != "" {
		out = append(out, s.w.delta(0, delta{Role: "assistant"}, ptr("content_filter")))
	}
	return out
}

// End returns the last chunks of the stream: the usage, when the client
// asked for it.
func (s *GeminiStream) End() [][]byte {
	if !s.includeUsage {
		return nil
	}
	return [][]byte{s.w.chunk(nil, openAIUsage(s.report.Report().Usage))}
}

// OpenAIError translates the error body of an Anthropic or Gemini provider
// into an OpenAI error with the provider's message.
func OpenAIError(f usage.Family, status int, body []byte) []byte {
	msg, code := "", ""
	switch f {
	case usage.Anthropic:
		var e struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil {
			msg, code = e.Error.Message, e.Error.Type
		}
	case usage.Gemini:
		var e struct {
			Error struct {
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil {
			msg, code = e.Error.Message, e.Error.Status
		}
	}
	if msg == "" {
		msg = fmt.Sprintf("The provider answered with status %d.", status)
	}
	typ := "invalid_request_error"
	switch {
	case status == 401:
		typ = "authentication_error"
	case status == 403:
		typ = "permission_error"
	case status == 429:
		typ = "rate_limit_error"
	case status >= 500:
		typ = "server_error"
	}
	var c any
	if code != "" {
		c = code
	}
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": typ, "param": nil,
		"code": c}}) // plain values always marshal
	return b
}
