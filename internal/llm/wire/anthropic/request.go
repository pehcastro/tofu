package anthropic

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	"boji/internal/llm"
	"boji/internal/transport"
)

type Request struct {
	Model     string
	System    []string
	Messages  []llm.Message
	Tools     []llm.Tool
	MaxTokens int
	Thinking  bool
	SessionID string
	AccountID string
	InstallID string
	UserID    string
	CacheTTL  string
}

type cacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type wireMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type wireTool struct {
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	InputSchema  any           `json:"input_schema"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type wireMetadata struct {
	UserID string `json:"user_id"`
}

type wireBody struct {
	Model     string        `json:"model"`
	Messages  []wireMessage `json:"messages"`
	System    []systemBlock `json:"system,omitempty"`
	Tools     []wireTool    `json:"tools,omitempty"`
	MaxTokens int           `json:"max_tokens"`
	Metadata  *wireMetadata `json:"metadata,omitempty"`
	Stream    bool          `json:"stream"`
}

func (r Request) Encode(oauth bool) ([]byte, error) {
	if r.Model == "" {
		return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil, "the request has no model")
	}
	messages, err := encodeMessages(r.Messages, oauth)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil, "the request carries no messages")
	}
	tools, err := encodeTools(r.Tools, oauth)
	if err != nil {
		return nil, err
	}

	maxTokens := r.MaxTokens
	if maxTokens <= 0 || (oauth && maxTokens > ClaudeCodeMaxTokens) {
		maxTokens = ClaudeCodeMaxTokens
	}

	system := systemBlocks(r.System, oauth, firstUserText(r.Messages), r.CacheTTL)
	applyHeadCaching(system, tools, r.CacheTTL)

	userID, err := metadataUserID(r, oauth)
	if err != nil {
		return nil, err
	}
	var metadata *wireMetadata
	if userID != "" {
		metadata = &wireMetadata{UserID: userID}
	}

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(wireBody{
		Model:     r.Model,
		Messages:  messages,
		System:    system,
		Tools:     tools,
		MaxTokens: maxTokens,
		Metadata:  metadata,
		Stream:    true,
	}); err != nil {
		return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, err, "encoding the request")
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

func systemBlocks(prompts []string, oauth bool, firstUserMessage, ttl string) []systemBlock {
	kept := make([]string, 0, len(prompts))
	for _, prompt := range prompts {
		if trimmed := strings.TrimSpace(prompt); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	alreadyBilled := len(kept) > 0 && strings.HasPrefix(kept[0], BillingHeaderPrefix)
	blocks := make([]systemBlock, 0, len(kept)+2)
	if oauth && !alreadyBilled {
		blocks = append(blocks,
			systemBlock{Type: "text", Text: BillingSystemBlock(firstUserMessage)},
			systemBlock{Type: "text", Text: ClaudeCodeSystemIdentity, CacheControl: ephemeral(ttl)},
		)
	}
	for _, prompt := range kept {
		blocks = append(blocks, systemBlock{Type: "text", Text: prompt})
	}
	return blocks
}

func ephemeral(ttl string) *cacheControl {
	return &cacheControl{Type: "ephemeral", TTL: ttl}
}

func applyHeadCaching(system []systemBlock, tools []wireTool, ttl string) {
	if len(tools) > 0 {
		tools[len(tools)-1].CacheControl = ephemeral(ttl)
	}
	for _, block := range system {
		if block.CacheControl != nil {
			return
		}
	}
	if len(system) > 0 {
		system[len(system)-1].CacheControl = ephemeral(ttl)
	}
}

func firstUserText(messages []llm.Message) string {
	for _, message := range messages {
		if message.Role == llm.RoleUser {
			return message.Content
		}
	}
	return ""
}

func encodeMessages(messages []llm.Message, oauth bool) ([]wireMessage, error) {
	encoded := make([]wireMessage, 0, len(messages))
	for index, message := range messages {
		switch message.Role {
		case llm.RoleSystem:
			return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil,
				"message %d is a system message; anthropic carries those in the system field", index)

		case llm.RoleUser:
			if message.Content == "" {
				return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil,
					"message %d is a user message with no content", index)
			}
			encoded = append(encoded, wireMessage{Role: "user",
				Content: []contentBlock{{Type: "text", Text: message.Content}}})

		case llm.RoleTool:
			if message.ToolCallID == "" {
				return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil,
					"message %d is a tool result with no tool call id", index)
			}
			result := contentBlock{Type: "tool_result", ToolUseID: message.ToolCallID, Content: message.Content}
			last := len(encoded) - 1
			if last >= 0 && encoded[last].Role == "user" && encoded[last].Content[0].Type == "tool_result" {
				encoded[last].Content = append(encoded[last].Content, result)
				continue
			}
			encoded = append(encoded, wireMessage{Role: "user", Content: []contentBlock{result}})

		case llm.RoleAssistant:
			if message.Content == "" && len(message.ToolCalls) == 0 {
				return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil,
					"message %d is an assistant message with no content and no tool calls", index)
			}
			blocks := make([]contentBlock, 0, len(message.ToolCalls)+1)
			if message.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: message.Content})
			}
			for callIndex, call := range message.ToolCalls {
				if call.ID == "" || call.Name == "" {
					return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil,
						"message %d tool call %d has no id or no name", index, callIndex)
				}
				input := call.Arguments
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, contentBlock{Type: "tool_use", ID: call.ID,
					Name: EncodeToolName(call.Name, oauth), Input: input})
			}
			encoded = append(encoded, wireMessage{Role: "assistant", Content: blocks})

		case llm.RoleUnknown:
			return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil,
				"message %d has an unknown role", index)
		}
	}
	return encoded, nil
}

func encodeTools(tools []llm.Tool, oauth bool) ([]wireTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	encoded := make([]wireTool, len(tools))
	for index, tool := range tools {
		if tool.Name == "" {
			return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil, "tool %d has no name", index)
		}
		schema := tool.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		encoded[index] = wireTool{
			Name:        EncodeToolName(tool.Name, oauth),
			Description: tool.Description,
			InputSchema: schema,
		}
	}
	return encoded, nil
}

func EncodeToolName(name string, oauth bool) string {
	if !oauth {
		return name
	}
	for _, builtin := range AnthropicBuiltinToolNames() {
		if strings.EqualFold(name, builtin) {
			return name
		}
	}
	return ClaudeCodeToolPrefix + name
}

func DecodeToolName(name string, oauth bool) string {
	if !oauth || !strings.HasPrefix(strings.ToLower(name), ClaudeCodeToolPrefix) {
		return name
	}
	return name[len(ClaudeCodeToolPrefix):]
}

type claudeUserID struct {
	DeviceID    string `json:"device_id,omitempty"`
	SessionID   string `json:"session_id"`
	AccountUUID string `json:"account_uuid,omitempty"`
}

func isCloakingUserID(userID string) bool {
	return regexp.MustCompile(
		`^user_[0-9a-fA-F]{64}_account_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}` +
			`_session_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(userID)
}

func metadataUserID(r Request, oauth bool) (string, error) {
	if r.UserID != "" && (!oauth || isCloakingUserID(r.UserID) || carriesSessionID(r.UserID)) {
		return r.UserID, nil
	}
	if !oauth {
		return "", nil
	}

	session := r.SessionID
	if session == "" {
		generated, err := randomUUID()
		if err != nil {
			return "", err
		}
		session = generated
	}
	encoded, err := json.Marshal(claudeUserID{
		DeviceID:    deviceID(r.InstallID, r.AccountID),
		SessionID:   session,
		AccountUUID: r.AccountID,
	})
	if err != nil {
		return "", transport.Fail("anthropic.Encode", transport.KindBadRequest, err, "encoding the metadata user id")
	}
	return string(encoded), nil
}

func carriesSessionID(userID string) bool {
	var parsed struct {
		SessionID string `json:"session_id"`
	}
	return json.Unmarshal([]byte(userID), &parsed) == nil && parsed.SessionID != ""
}

func deviceID(installID, accountID string) string {
	if installID == "" {
		return ""
	}
	if accountID != "" {
		sum := sha256.Sum256([]byte(DeviceIDAccountHashDomain + "\x00" + installID + "\x00" + accountID))
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256([]byte(DeviceIDInstallHashDomain + installID))
	return hex.EncodeToString(sum[:])
}

func randomUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", transport.Fail("anthropic.Encode", transport.KindBadRequest, err, "drawing a session id")
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	text := hex.EncodeToString(raw[:])
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:], nil
}
