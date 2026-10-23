package llm

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/transport"
)

type Role int

const (
	RoleUnknown Role = iota
	RoleSystem
	RoleUser
	RoleAssistant
	RoleTool
)

func (r Role) String() string {
	switch r {
	case RoleUnknown:
		return "unknown"
	case RoleSystem:
		return "system"
	case RoleUser:
		return "user"
	case RoleAssistant:
		return "assistant"
	case RoleTool:
		return "tool"
	}
	panic("llm: unknown role")
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type ToolOutcome int

const (
	ToolOutcomeUnset ToolOutcome = iota
	ToolOutcomeRan
	ToolOutcomeFailed
	ToolOutcomeAborted
)

func (o ToolOutcome) Failed() bool {
	switch o {
	case ToolOutcomeUnset, ToolOutcomeRan, ToolOutcomeAborted:
		return false
	case ToolOutcomeFailed:
		return true
	}
	panic("llm: unknown tool outcome")
}

const (
	ImageLongEdgePixels = 1568
	ImagePixels         = 1_150_000
	ImageEncodedBytes   = 5 << 20
)

type Image struct {
	MediaType string
	Data      []byte
}

func (i Image) Base64() string {
	return base64.StdEncoding.EncodeToString(i.Data)
}

func (i Image) DataURL() string {
	return "data:" + i.MediaType + ";base64," + i.Base64()
}

type Thinking struct {
	Text      string
	Signature string
}

func (t Thinking) Empty() bool {
	return t == Thinking{}
}

func BlankText(text string) bool {
	return strings.TrimSpace(text) == ""
}

type Message struct {
	Role            Role
	Content         string
	Images          []Image
	ToolCallID      string
	ToolCalls       []ToolCall
	ToolOutcome     ToolOutcome
	ToolResultBytes int
	ToolExitCode    *int `json:"-"`
	ToolHits        *int `json:"-"`
	Thinking        Thinking
	Origin          Origin `json:"-"`
}

type OriginKind string

const (
	OriginPerson  OriginKind = "person"
	OriginAgent   OriginKind = "agent"
	OriginTofu    OriginKind = "tofu"
	OriginCron    OriginKind = "cron"
	OriginHook    OriginKind = "hook"
	OriginMemory  OriginKind = "memory"
	OriginSkill   OriginKind = "skill"
	OriginLibrary OriginKind = "library"
	OriginPlugin  OriginKind = "plugin"
)

func (OriginKind) Enum() []string {
	return []string{string(OriginPerson), string(OriginAgent), string(OriginTofu), string(OriginCron), string(OriginHook),
		string(OriginMemory), string(OriginSkill), string(OriginLibrary), string(OriginPlugin)}
}

type Source string

const (
	SourceTask      Source = "task"
	SourceTyped     Source = "typed"
	SourceSteer     Source = "steer"
	SourceAnswer    Source = "answer"
	SourceForkTask  Source = "fork.task"
	SourceBrief     Source = "brief"
	SourceReport    Source = "report"
	SourceReportFor Source = "report.lead"
	SourceAsk       Source = "ask"
	SourceMessage   Source = "message"

	SourceShellExit   Source = "shell.exit"
	SourceAnswerFirst Source = "nudge.answer_first"
	SourceSlots       Source = "nudge.slots"
	SourceHandback    Source = "nudge.handback"
	SourceCheck       Source = "nudge.check"
	SourceStepCap     Source = "nudge.step_cap"
	SourceBlankAnswer Source = "nudge.blank_answer"
	SourceLastWord    Source = "nudge.last_word"
	SourceContinue    Source = "continue"
	SourceForkCarry   Source = "fork.carry"
	SourceForkState   Source = "fork.state"
	SourcePlan        Source = "plan"
	SourceReplyCut    Source = "reply_cut"
	SourceEnvironment Source = "environment"
	SourceNotice      Source = "notice"
	SourceFailure     Source = "failure"
	SourceGate        Source = "gate"
	SourceSpawnLine   Source = "spawn.line"

	SourceCronFire     Source = "fire"
	SourceSessionStart Source = "session_start"
	SourcePromptSubmit Source = "prompt_submit"
	SourcePreTool      Source = "pre_tool"
	SourcePostTool     Source = "post_tool"
	SourceStopHook     Source = "stop"
	SourceMemoryView   Source = "view"
	SourceEpisodes     Source = "episodes"
	SourceMemorySaved  Source = "saved"
	SourceSkillLoad    Source = "load"
	SourceReference    Source = "reference"

	SourceAfterTheCachedPrefix Source = "after the cached prefix"
)

type sourceRow struct {
	source Source
	kind   OriginKind
	legacy string
}

func sourceRows() []sourceRow {
	return []sourceRow{
		{SourceTask, OriginPerson, "task"}, {SourceTyped, OriginPerson, "typed by the person"}, {SourceSteer, OriginPerson, "steer"},
		{SourceAnswer, OriginPerson, "question answer"}, {SourceForkTask, OriginPerson, "fork task"},
		{SourceBrief, OriginAgent, "sub-agent brief"}, {SourceReport, OriginAgent, "sub-agent report"},
		{SourceReportFor, OriginAgent, "sub-agent report needing the lead"}, {SourceAsk, OriginAgent, "sub-agent gate ask"},
		{SourceMessage, OriginAgent, "message to this sub-agent"},
		{SourceShellExit, OriginTofu, "shell exit"}, {SourceAnswerFirst, OriginTofu, "mid-turn message note"},
		{SourceSlots, OriginTofu, "lead check-in"}, {SourceHandback, OriginTofu, "turn end check"}, {SourceCheck, OriginTofu, "sub-agent check"},
		{SourceStepCap, OriginTofu, "step cap notice"}, {SourceBlankAnswer, OriginTofu, "blank answer"}, {SourceLastWord, OriginTofu, "last word"},
		{SourceContinue, OriginTofu, "continue"}, {SourceForkCarry, OriginTofu, "fork carry"}, {SourceForkState, OriginTofu, "fork state"},
		{SourcePlan, OriginTofu, string(SourceAfterTheCachedPrefix)}, {SourceReplyCut, OriginTofu, "reply cut"},
		{SourceEnvironment, OriginTofu, "environment"}, {SourceNotice, OriginTofu, "notice"}, {SourceFailure, OriginTofu, "failure"},
		{SourceGate, OriginTofu, "gate"}, {SourceSpawnLine, OriginTofu, "spawn line"},
		{SourceCronFire, OriginCron, legacyCron},
		{SourceSessionStart, OriginHook, "session start hook"}, {SourcePromptSubmit, OriginHook, "prompt submit hook"},
		{SourcePreTool, OriginHook, "pre tool hook"}, {SourcePostTool, OriginHook, "post tool hook"}, {SourceStopHook, OriginHook, "stop hook"},
		{SourceMemoryView, OriginMemory, "memory view"}, {SourceEpisodes, OriginMemory, "episode view"}, {SourceMemorySaved, OriginMemory, "memory saved"},
		{SourceSkillLoad, OriginSkill, "skill"}, {SourceReference, OriginLibrary, "reference"},
	}
}

func (Source) Enum() []string {
	var named []string
	for _, row := range sourceRows() {
		named = append(named, string(row.source))
	}
	return named
}

func (s Source) row() (sourceRow, bool) {
	for _, row := range sourceRows() {
		if row.source == s {
			return row, true
		}
	}
	return sourceRow{}, false
}

func (s Source) Known() bool {
	_, known := s.row()
	return known
}

func (s Source) Kind() OriginKind {
	row, known := s.row()
	if !known && s != "" && s != SourceAfterTheCachedPrefix {
		panic("llm: unknown origin source " + string(s))
	}
	return row.kind
}

const (
	legacyCron    = "cron "
	legacyFiredIn = ", fired in process "
	legacyJoin    = " and "
)

func (o Origin) Legacy() string {
	row, _ := o.Source.row()
	if o.Source == SourceCronFire {
		return row.legacy + o.ID + " " + o.Schedule
	}
	return row.legacy
}

func LegacyOrigin(recorded string) Origin {
	if fired, isCron := strings.CutPrefix(recorded, legacyCron); isCron {
		fired, _, _ = strings.Cut(fired, legacyFiredIn)
		job, schedule, _ := strings.Cut(fired, " ")
		return Origin{Source: SourceCronFire, ID: job, Schedule: schedule}
	}
	var read []Source
	for part := range strings.SplitSeq(cmp.Or(recorded, string(SourceTask)), legacyJoin) {
		at := slices.IndexFunc(sourceRows(), func(row sourceRow) bool { return row.legacy == part })
		source := SourceNotice
		if at >= 0 {
			source = sourceRows()[at].source
		}
		read = append(read, source)
	}
	if person := slices.IndexFunc(read, func(source Source) bool { return source.Kind() == OriginPerson }); person >= 0 {
		return Origin{Source: read[person]}
	}
	return Origin{Source: read[0]}
}

type Origin struct {
	Source   Source
	Name     string
	ID       string
	Schedule string
	Exit     *int
	Ran      time.Duration
	PostedAt time.Time
	TakenAt  time.Time
}

func (o Origin) Kind() OriginKind {
	return o.Source.Kind()
}

func (o Origin) Marked() bool {
	kind := o.Kind()
	return kind != "" && kind != OriginPerson
}

func (s Source) AfterTheCachedPrefix() bool {
	return s == SourceAfterTheCachedPrefix || s == SourcePlan
}

func (o Origin) Mark(text string) string {
	attributes := [][2]string{{"origin", string(o.Kind()) + "/" + string(o.Source)}, {"name", o.Name}, {"id", o.ID}, {"schedule", o.Schedule}}
	if o.Exit != nil {
		attributes = append(attributes, [2]string{"exit", strconv.Itoa(*o.Exit)})
	}
	opened := "<from"
	for _, attribute := range attributes {
		if attribute[1] != "" {
			opened += " " + attribute[0] + `="` + strings.ReplaceAll(attribute[1], `"`, "'") + `"`
		}
	}
	return opened + ">\n" + text + "\n</from>"
}

func (m Message) Sent() string {
	if m.Role != RoleUser || !m.Origin.Marked() || BlankText(m.Content) {
		return m.Content
	}
	return m.Origin.Mark(m.Content)
}

type Tool struct {
	Name        string
	Description string
	Parameters  any
}

type ToolChoice int

const (
	ToolChoiceAuto ToolChoice = iota
	ToolChoiceNone
)

func (c ToolChoice) OpenAIValue() string {
	switch c {
	case ToolChoiceAuto:
		return ""
	case ToolChoiceNone:
		return "none"
	}
	panic("llm: unknown tool choice")
}

type Request struct {
	Turn       string
	Messages   []Message
	Tools      []Tool
	ToolChoice ToolChoice
	OnDelta    func(string)
	OnThinking func(string)
	OnRetry    func()

	OmitThinkingSummary bool
}

func (r Request) Encode(model string) ([]byte, error) {
	if model == "" {
		return nil, transport.Fail("llm.Encode", transport.KindBadRequest, nil, "the model is empty")
	}
	if len(r.Messages) == 0 {
		return nil, transport.Fail("llm.Encode", transport.KindBadRequest, nil, "the request carries no messages")
	}

	messages := make([]wireMessage, 0, len(r.Messages))
	var toolPictures []wirePart
	for index, message := range r.Messages {
		if message.Role != RoleTool && len(toolPictures) > 0 {
			messages, toolPictures = append(messages, wireMessage{Role: "user", Content: toolPictures}), nil
		}
		if message.Role == RoleAssistant && BlankText(message.Content) && len(message.ToolCalls) == 0 {
			continue
		}
		wire, err := encodeMessage(index, message)
		if err != nil {
			return nil, err
		}
		messages = append(messages, wire)
		if message.Role == RoleTool && len(message.Images) > 0 {
			toolPictures = append(toolPictures, wirePart{Type: "text", Text: "the picture tool call " + message.ToolCallID + " returned"})
			toolPictures = append(toolPictures, pictureParts(message.Images)...)
		}
	}
	if len(toolPictures) > 0 {
		messages = append(messages, wireMessage{Role: "user", Content: toolPictures})
	}

	tools, err := encodeTools(r.Tools)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(wireRequest{
		Model:      model,
		Messages:   messages,
		Tools:      tools,
		ToolChoice: r.ToolChoice.OpenAIValue(),
		Usage:      wireUsageOption{Include: true},
	})
	if err != nil {
		return nil, transport.Fail("llm.Encode", transport.KindBadRequest, err, "encoding the request")
	}
	return body, nil
}

func encodeMessage(index int, message Message) (wireMessage, error) {
	switch message.Role {
	case RoleSystem, RoleUser:
		if BlankText(message.Content) {
			return wireMessage{}, transport.Fail("llm.Encode", transport.KindBadRequest, nil,
				"message %d is a %s with no content", index, message.Role)
		}
	case RoleTool:
		if message.ToolCallID == "" {
			return wireMessage{}, transport.Fail("llm.Encode", transport.KindBadRequest, nil,
				"message %d is a tool result with no tool call id", index)
		}
	case RoleAssistant:
		if BlankText(message.Content) {
			message.Content = ""
		}
	default:
		return wireMessage{}, transport.Fail("llm.Encode", transport.KindBadRequest, nil,
			"message %d has an unknown role", index)
	}

	calls := make([]wireToolCall, len(message.ToolCalls))
	for callIndex, call := range message.ToolCalls {
		if call.ID == "" || call.Name == "" {
			return wireMessage{}, transport.Fail("llm.Encode", transport.KindBadRequest, nil,
				"message %d tool call %d has no id or no name", index, callIndex)
		}
		calls[callIndex] = wireToolCall{ID: call.ID, Type: "function"}
		calls[callIndex].Function.Name = call.Name
		calls[callIndex].Function.Arguments = string(call.Arguments)
	}

	wire := wireMessage{Role: message.Role.String(), ToolCallID: message.ToolCallID, ToolCalls: calls}
	if text := message.Sent(); text != "" {
		wire.Content = text
	}
	if message.Role == RoleUser && len(message.Images) > 0 {
		wire.Content = append([]wirePart{{Type: "text", Text: message.Sent()}}, pictureParts(message.Images)...)
	}
	return wire, nil
}

func pictureParts(images []Image) []wirePart {
	parts := make([]wirePart, len(images))
	for i, image := range images {
		parts[i] = wirePart{Type: "image_url", ImageURL: &wireImageURL{URL: image.Fitted().DataURL()}}
	}
	return parts
}

func encodeTools(tools []Tool) ([]wireTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(tools))
	encoded := make([]wireTool, len(tools))
	for index, tool := range tools {
		if tool.Name == "" {
			return nil, transport.Fail("llm.Encode", transport.KindBadRequest, nil, "tool %d has no name", index)
		}
		if seen[tool.Name] {
			return nil, transport.Fail("llm.Encode", transport.KindBadRequest, nil, "tool name %q appears twice", tool.Name)
		}
		seen[tool.Name] = true
		encoded[index] = wireTool{Type: "function"}
		encoded[index].Function.Name = tool.Name
		encoded[index].Function.Description = tool.Description
		encoded[index].Function.Parameters = tool.Parameters
	}
	return encoded, nil
}

type wireFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function wireFunctionCall `json:"function"`
}

type wireImageURL struct {
	URL string `json:"url"`
}

type wirePart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *wireImageURL `json:"image_url,omitempty"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
}

type wireFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireUsageOption struct {
	Include bool `json:"include"`
}

type wireRequest struct {
	Model      string          `json:"model"`
	Messages   []wireMessage   `json:"messages"`
	Tools      []wireTool      `json:"tools,omitempty"`
	ToolChoice string          `json:"tool_choice,omitempty"`
	Usage      wireUsageOption `json:"usage"`
}
