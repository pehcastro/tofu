package codex

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"slices"

	"tofu/internal/llm"
	"tofu/internal/transport"
)

type Sampling struct {
	Temperature       *float64
	TopP              *float64
	TopK              *int
	MinP              *float64
	PresencePenalty   *float64
	FrequencyPenalty  *float64
	RepetitionPenalty *float64
	Stop              []string
}

type Request struct {
	Model           string
	Instructions    string
	Messages        []llm.Message
	Tools           []llm.Tool
	ToolChoice      string
	Effort          llm.Effort
	SummaryOff      bool
	ServiceTier     string
	PromptCacheKey  string
	Identity        Identity
	TurnState       string
	MaxOutputTokens int
	Sampling        Sampling
	OnThinking      func(string)
	OnRetry         func()
}

const imageDetail = "high"

type inputPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type inputItem struct {
	Type             string      `json:"type,omitempty"`
	ID               string      `json:"id,omitempty"`
	Role             string      `json:"role,omitempty"`
	Content          []inputPart `json:"content,omitempty"`
	CallID           string      `json:"call_id,omitempty"`
	Name             string      `json:"name,omitempty"`
	Arguments        string      `json:"arguments,omitempty"`
	Output           string      `json:"output,omitempty"`
	EncryptedContent string      `json:"encrypted_content,omitempty"`
	Summary          []inputPart `json:"summary,omitzero"`
}

type reasoningItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	EncryptedContent string `json:"encrypted_content"`
}

func EncodeReasoning(id, encryptedContent string) string {
	if id == "" || encryptedContent == "" {
		return ""
	}
	raw, err := json.Marshal(reasoningItem{ID: id, Type: "reasoning", EncryptedContent: encryptedContent})
	if err != nil {
		return ""
	}
	return string(raw)
}

func DecodeReasoning(signature string) (id, encryptedContent string, ok bool) {
	if signature == "" {
		return "", "", false
	}
	var item reasoningItem
	if err := json.Unmarshal([]byte(signature), &item); err != nil || item.ID == "" || item.EncryptedContent == "" {
		return "", "", false
	}
	return item.ID, item.EncryptedContent, true
}

type wireTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type wireReasoning struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary,omitempty"`
}

type wireBody struct {
	Model          string            `json:"model"`
	Input          []inputItem       `json:"input"`
	Stream         bool              `json:"stream"`
	Store          bool              `json:"store"`
	PromptCacheKey string            `json:"prompt_cache_key,omitempty"`
	Instructions   string            `json:"instructions,omitempty"`
	Tools          []wireTool        `json:"tools,omitempty"`
	ToolChoice     string            `json:"tool_choice,omitempty"`
	ServiceTier    string            `json:"service_tier,omitempty"`
	Reasoning      *wireReasoning    `json:"reasoning,omitempty"`
	Include        []string          `json:"include"`
	ClientMetadata map[string]string `json:"client_metadata,omitempty"`
}

func (r Request) Encode(clientMetadata map[string]string) ([]byte, error) {
	if r.Model == "" {
		return nil, transport.Fail("codex.Encode", transport.KindBadRequest, nil, "the request has no model")
	}
	input, err := encodeInput(r.Messages)
	if err != nil {
		return nil, err
	}
	if len(input) == 0 {
		return nil, transport.Fail("codex.Encode", transport.KindBadRequest, nil, "the request carries no messages")
	}
	tools, err := encodeTools(r.Tools)
	if err != nil {
		return nil, err
	}
	reasoning, err := r.reasoning()
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(wireBody{
		Model:          r.Model,
		Input:          input,
		Stream:         true,
		Store:          false,
		PromptCacheKey: cmp.Or(r.PromptCacheKey, r.Identity.SessionID),
		Instructions:   r.Instructions,
		Tools:          tools,
		ToolChoice:     r.ToolChoice,
		ServiceTier:    r.ServiceTier,
		Reasoning:      reasoning,
		Include:        []string{EncryptedReasoningInclude},
		ClientMetadata: clientMetadata,
	}); err != nil {
		return nil, transport.Fail("codex.Encode", transport.KindBadRequest, err, "encoding the request")
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

func (r Request) reasoning() (*wireReasoning, error) {
	if r.Effort == "" {
		return nil, nil
	}
	if !slices.Contains(ReasoningEfforts(), r.Effort) {
		return nil, transport.Fail("codex.Encode", transport.KindBadRequest, nil,
			"%q is not a codex reasoning effort; this wire takes %s",
			r.Effort, llm.EffortList(ReasoningEfforts()))
	}
	reasoning := &wireReasoning{Effort: string(r.Effort)}
	if !r.SummaryOff {
		reasoning.Summary = DefaultReasoningSummary
	}
	return reasoning, nil
}

func (r Request) RefusedControls() []string {
	present := [9]bool{
		r.Sampling.Temperature != nil,
		r.Sampling.TopP != nil,
		r.Sampling.TopK != nil,
		r.Sampling.MinP != nil,
		r.Sampling.PresencePenalty != nil,
		r.Sampling.FrequencyPenalty != nil,
		r.Sampling.RepetitionPenalty != nil,
		len(r.Sampling.Stop) > 0,
		r.MaxOutputTokens > 0,
	}
	names := append(ForbiddenSamplingControls(), "max_output_tokens")
	var refused []string
	for index, set := range present {
		if set {
			refused = append(refused, names[index])
		}
	}
	return refused
}

func encodeInput(messages []llm.Message) ([]inputItem, error) {
	items := make([]inputItem, 0, len(messages))
	for index, message := range messages {
		if len(message.Images) > 0 && message.Role != llm.RoleUser {
			return nil, transport.Fail("codex.Encode", transport.KindBadRequest, nil,
				"message %d carries an image outside a user message; codex sends images only from the user", index)
		}
		switch message.Role {
		case llm.RoleSystem:
			return nil, transport.Fail("codex.Encode", transport.KindBadRequest, nil,
				"message %d is a system message; codex carries those in instructions", index)

		case llm.RoleUser:
			if llm.BlankText(message.Content) && len(message.Images) == 0 {
				return nil, transport.Fail("codex.Encode", transport.KindBadRequest, nil,
					"message %d is a user message with no content", index)
			}
			parts := make([]inputPart, 0, len(message.Images)+1)
			if !llm.BlankText(message.Content) {
				parts = append(parts, inputPart{Type: "input_text", Text: message.Content})
			}
			for _, image := range message.Images {
				parts = append(parts, inputPart{Type: "input_image", Detail: imageDetail,
					ImageURL: "data:" + image.MediaType + ";base64," + base64.StdEncoding.EncodeToString(image.Data)})
			}
			items = append(items, inputItem{Role: "user", Content: parts})

		case llm.RoleTool:
			if message.ToolCallID == "" {
				return nil, transport.Fail("codex.Encode", transport.KindBadRequest, nil,
					"message %d is a tool result with no tool call id", index)
			}
			items = append(items, inputItem{Type: "function_call_output",
				CallID: message.ToolCallID, Output: message.Content})

		case llm.RoleAssistant:
			if id, encrypted, ok := DecodeReasoning(message.Thinking.Signature); ok && len(message.ToolCalls) > 0 {
				items = append(items, inputItem{Type: "reasoning", ID: id, EncryptedContent: encrypted, Summary: []inputPart{}})
			}
			if !llm.BlankText(message.Content) {
				items = append(items, inputItem{Type: "message", Role: "assistant",
					Content: []inputPart{{Type: "output_text", Text: message.Content}}})
			}
			for callIndex, call := range message.ToolCalls {
				if call.ID == "" || call.Name == "" {
					return nil, transport.Fail("codex.Encode", transport.KindBadRequest, nil,
						"message %d tool call %d has no id or no name", index, callIndex)
				}
				arguments := string(call.Arguments)
				if arguments == "" {
					arguments = "{}"
				}
				items = append(items, inputItem{Type: "function_call",
					CallID: call.ID, Name: call.Name, Arguments: arguments})
			}

		case llm.RoleUnknown:
			return nil, transport.Fail("codex.Encode", transport.KindBadRequest, nil,
				"message %d has an unknown role", index)
		}
	}
	return items, nil
}

func encodeTools(tools []llm.Tool) ([]wireTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	encoded := make([]wireTool, len(tools))
	for index, tool := range tools {
		if tool.Name == "" {
			return nil, transport.Fail("codex.Encode", transport.KindBadRequest, nil, "tool %d has no name", index)
		}
		schema := tool.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		encoded[index] = wireTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  schema,
		}
	}
	return encoded, nil
}
