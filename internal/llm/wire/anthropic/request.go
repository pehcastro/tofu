package anthropic

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/transport"
)

type Request struct {
	Model      string
	System     []string
	Messages   []llm.Message
	Tools      []llm.Tool
	ToolChoice llm.ToolChoice
	MaxTokens  int
	Effort     llm.Effort
	SessionID  string
	AccountID  string
	InstallID  string
	UserID     string
	CacheTTL   string

	ClaudeCodeVersion   string
	HistoryCacheOff     bool
	OmitThinkingSummary bool
	OnDelta             func(string)
	OnThinking          func(string)
	OnRetry             func()
}

const (
	historyCacheMinMessages       = 3
	historyCacheMinPrefixChars    = 4096
	historyCacheCommittedMinChars = 4096
	cacheBreakpointsPerRequest    = 4

	summarizedThinkingMajor = 4
	summarizedThinkingMinor = 7

	ImageBytesCap = 5 << 20
)

type cacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Source    *imageSource    `json:"source,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`

	CacheControl *cacheControl `json:"cache_control,omitempty"`
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

type wireThinking struct {
	Type    string `json:"type"`
	Display string `json:"display"`
}

type wireOutput struct {
	Effort string `json:"effort"`
}

type wireToolChoice struct {
	Type string `json:"type"`
}

type wireBody struct {
	Model      string          `json:"model"`
	Messages   []wireMessage   `json:"messages"`
	System     []systemBlock   `json:"system,omitempty"`
	Tools      []wireTool      `json:"tools,omitempty"`
	ToolChoice *wireToolChoice `json:"tool_choice,omitempty"`
	MaxTokens  int             `json:"max_tokens"`
	Metadata   *wireMetadata   `json:"metadata,omitempty"`
	Thinking   *wireThinking   `json:"thinking,omitempty"`
	Output     *wireOutput     `json:"output_config,omitempty"`
	Stream     bool            `json:"stream"`
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

	system := systemBlocks(r.System, oauth, BillingSystemBlock(firstUserText(r.Messages), cmp.Or(r.ClaudeCodeVersion, PinnedClaudeCodeVersion)), r.CacheTTL)
	head := applyHeadCaching(system, tools, r.CacheTTL)
	if !r.HistoryCacheOff {
		applyHistoryCaching(messages, map[bool]string{true: konst.HistoryCacheTTL}[r.CacheTTL != ""], cacheBreakpointsPerRequest-head)
	}

	userID, err := metadataUserID(r, oauth)
	if err != nil {
		return nil, err
	}
	var metadata *wireMetadata
	if userID != "" {
		metadata = &wireMetadata{UserID: userID}
	}
	output, err := r.outputConfig()
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(wireBody{
		Model:      r.Model,
		Messages:   messages,
		System:     system,
		Tools:      tools,
		ToolChoice: r.toolChoice(),
		MaxTokens:  maxTokens,
		Metadata:   metadata,
		Thinking:   r.summarizedThinking(),
		Output:     output,
		Stream:     true,
	}); err != nil {
		return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, err, "encoding the request")
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

func (r Request) toolChoice() *wireToolChoice {
	switch r.ToolChoice {
	case llm.ToolChoiceAuto:
		return nil
	case llm.ToolChoiceNone:
		return &wireToolChoice{Type: "none"}
	}
	panic("anthropic: unknown tool choice")
}

func (r Request) outputConfig() (*wireOutput, error) {
	if !r.Effort.Thinks() {
		return nil, nil
	}
	if !slices.Contains(ReasoningEfforts(), r.Effort) {
		return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil,
			"%q is not an anthropic thinking effort; this wire takes %s",
			r.Effort, llm.EffortList(ReasoningEfforts()))
	}
	return &wireOutput{Effort: string(r.Effort)}, nil
}

func (r Request) summarizedThinking() *wireThinking {
	version := regexp.MustCompile(`^claude-[a-z]+-(\d+)(?:-(\d{1,2}))?(?:-|$)`).FindStringSubmatch(r.Model)
	if r.OmitThinkingSummary || !r.Effort.Thinks() || version == nil {
		return nil
	}
	major, _ := strconv.Atoi(version[1])
	minor, _ := strconv.Atoi(version[2])
	if major < summarizedThinkingMajor || major == summarizedThinkingMajor && minor < summarizedThinkingMinor {
		return nil
	}
	return &wireThinking{Type: "adaptive", Display: "summarized"}
}

func systemBlocks(prompts []string, oauth bool, billing, ttl string) []systemBlock {
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
			systemBlock{Type: "text", Text: billing},
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

func applyHeadCaching(system []systemBlock, tools []wireTool, ttl string) int {
	placed := 0
	if len(tools) > 0 {
		tools[len(tools)-1].CacheControl = ephemeral(ttl)
		placed++
	}
	for _, block := range system {
		if block.CacheControl != nil {
			placed++
		}
	}
	if anchor := lastStableSystemBlock(system); anchor >= 0 && system[anchor].CacheControl == nil {
		system[anchor].CacheControl = ephemeral(ttl)
		placed++
	}
	return placed
}

func lastStableSystemBlock(system []systemBlock) int {
	for index := len(system) - 1; index >= 0; index-- {
		if !strings.HasPrefix(system[index].Text, BillingHeaderPrefix) {
			return index
		}
	}
	return -1
}

func applyHistoryCaching(messages []wireMessage, ttl string, budget int) {
	if budget < 1 || len(messages) < historyCacheMinMessages || historyChars(messages) < historyCacheMinPrefixChars {
		return
	}
	markPrefixEnd(messages[len(messages)-1].Content, ttl)
	committed := committedPrefixEnd(messages)
	if budget < 2 || committed < 1 || historyChars(messages[:committed+1]) < historyCacheCommittedMinChars {
		return
	}
	markPrefixEnd(messages[committed].Content, ttl)
}

func committedPrefixEnd(messages []wireMessage) int {
	for index := len(messages) - 1; index > 0; index-- {
		if messages[index].Role == "assistant" {
			return index - 1
		}
	}
	return -1
}

func markPrefixEnd(blocks []contentBlock, ttl string) {
	blocks[len(blocks)-1].CacheControl = ephemeral(ttl)
}

func historyChars(messages []wireMessage) int {
	total := 0
	for _, message := range messages {
		for _, block := range message.Content {
			total += len(block.Text) + len(block.Content) + len(block.Input)
		}
	}
	return total
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
			if llm.BlankText(message.Content) && len(message.Images) == 0 {
				return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil,
					"message %d is a user message with no content", index)
			}
			blocks := make([]contentBlock, 0, len(message.Images)+1)
			if !llm.BlankText(message.Content) {
				blocks = append(blocks, contentBlock{Type: "text", Text: message.Content})
			}
			for imageIndex, image := range message.Images {
				if len(image.Data) > ImageBytesCap {
					return nil, transport.Fail("anthropic.Encode", transport.KindBadRequest, nil,
						"message %d image %d is %d bytes, past the %d byte cap", index, imageIndex, len(image.Data), ImageBytesCap)
				}
				blocks = append(blocks, contentBlock{Type: "image", Source: &imageSource{
					Type: "base64", MediaType: image.MediaType, Data: base64.StdEncoding.EncodeToString(image.Data),
				}})
			}
			encoded = append(encoded, wireMessage{Role: "user", Content: blocks})

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
			if llm.BlankText(message.Content) && len(message.ToolCalls) == 0 {
				continue
			}
			blocks := make([]contentBlock, 0, len(message.ToolCalls)+2)
			if len(message.ToolCalls) > 0 && message.Thinking.Text != "" {
				blocks = append(blocks, contentBlock{Type: "thinking",
					Thinking: message.Thinking.Text, Signature: message.Thinking.Signature})
			}
			if !llm.BlankText(message.Content) {
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
	encoded, err := json.Marshal(claudeUserID{
		DeviceID:    deviceID(r.InstallID, r.AccountID),
		SessionID:   r.SessionID,
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
		return "", transport.Fail("anthropic.Ask", transport.KindBadRequest, err, "drawing a session id")
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	text := hex.EncodeToString(raw[:])
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:], nil
}
