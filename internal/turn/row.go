package turn

import (
	"cmp"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

const SchemaVersion = 1

type Outcome = session.Outcome

const (
	OutcomeUnset               = session.OutcomeUnset
	OutcomeStopped             = session.OutcomeStopped
	OutcomeStepCap             = session.OutcomeStepCap
	OutcomeRetiredCostCap      = session.OutcomeRetiredCostCap
	OutcomeRetiredWallClockCap = session.OutcomeRetiredWallClockCap
	OutcomeDecisionCap         = session.OutcomeDecisionCap
	OutcomeForked              = session.OutcomeForked
	OutcomeError               = session.OutcomeError
	OutcomeTruncated           = session.OutcomeTruncated
	OutcomeLoopGuard           = session.OutcomeLoopGuard
)

func AllOutcomes() []Outcome {
	return session.AllOutcomes()
}

type ToolCallRow struct {
	ID     string `json:"id,omitempty"`
	Parent string `json:"parent,omitempty"`
	Author string `json:"author,omitempty"`

	Tool              string          `json:"tool"`
	Args              json.RawMessage `json:"args,omitempty"`
	Command           string          `json:"command,omitempty"`
	Proxy             *ProxyRow       `json:"proxy,omitempty"`
	ChildID           string          `json:"child_id"`
	ExitCode          *int            `json:"exit_code,omitempty"`
	ResultBytes       int             `json:"result_bytes"`
	RenderedBytes     int             `json:"rendered_bytes"`
	ResultHash        string          `json:"result_hash,omitempty"`
	ResultHandle      string          `json:"result_handle,omitempty"`
	ResultHandleError string          `json:"result_handle_error,omitempty"`
	SiftSavedBytes    int             `json:"sift_saved_bytes,omitempty"`

	ParallelBatch  int    `json:"parallel_batch,omitempty"`
	GateDecisionID string `json:"gate_decision_id,omitempty"`
	GateVerdict    string `json:"gate_verdict,omitempty"`
	GateError      string `json:"gate_error,omitempty"`
	DurationMS     int64  `json:"duration_ms"`
	Error          string `json:"error,omitempty"`
}

func (r ToolCallRow) Outcome() llm.ToolOutcome {
	if r.Error != "" || (r.ExitCode != nil && *r.ExitCode != 0) {
		return llm.ToolOutcomeFailed
	}
	return llm.ToolOutcomeRan
}

type StepRow struct {
	id      string
	attempt int

	Index            int               `json:"index"`
	ToolCalls        []ToolCallRow     `json:"tool_calls,omitempty"`
	AssistantText    string            `json:"assistant_text,omitempty"`
	StopReason       string            `json:"stop_reason,omitempty"`
	PromptTokens     int               `json:"prompt_tokens"`
	CompletionTokens int               `json:"completion_tokens"`
	CacheReadTokens  int               `json:"cache_read_tokens"`
	CacheWriteTokens int               `json:"cache_write_tokens"`
	CostUSD          float64           `json:"cost_usd"`
	Warnings         []string          `json:"warnings,omitempty"`
	Occupancy        *recall.Occupancy `json:"occupancy,omitempty"`
	Bands            *recall.Bands     `json:"bands,omitempty"`
	Compaction       *Compaction       `json:"compaction,omitempty"`
	Fork             *Fork             `json:"fork,omitempty"`

	Grants []subagent.Question `json:"grants,omitempty"`
}

type Spend string

const (
	SpendSubscription Spend = "subscription"
	SpendAPIKey       Spend = "api_key"
)

type Row struct {
	ID           string       `json:"id"`
	Schema       int          `json:"schema"`
	At           time.Time    `json:"at"`
	Task         string       `json:"task"`
	Wire         string       `json:"wire,omitempty"`
	Model        string       `json:"model"`
	Spend        Spend        `json:"spend"`
	Steps        []StepRow    `json:"steps,omitempty"`
	Root         string       `json:"root,omitempty"`
	Account      int64        `json:"account,omitempty"`
	SpawnedFrom  string       `json:"spawned_from,omitempty"`
	ForkedFrom   string       `json:"forked_from,omitempty"`
	ForkedInto   string       `json:"forked_into,omitempty"`
	ForkKind     ForkKind     `json:"fork_kind,omitempty"`
	Warnings     []string     `json:"warnings,omitempty"`
	ChildIDs     []string     `json:"child_ids"`
	Outcome      Outcome      `json:"outcome"`
	TotalCostUSD float64      `json:"total_cost_usd"`
	WallClockMS  int64        `json:"wall_clock_ms"`
	DecisionIDs  []string     `json:"decision_ids,omitempty"`
	SearchLinks  []SearchLink `json:"search_links,omitempty"`

	Budget recall.Budget  `json:"budget"`
	Guard  *LoopGuardStop `json:"loop_guard,omitempty"`

	Conversation []llm.Message `json:"-"`
	System       string        `json:"-"`
	Tools        []string      `json:"-"`
}

type LoopGuardStop struct {
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args,omitempty"`
	Repeats int             `json:"repeats"`
}

type MessageToolCall = session.MessageToolCall

type MessageRow = session.MessageBody

func toolOutcomeName(outcome llm.ToolOutcome) string {
	switch outcome {
	case llm.ToolOutcomeUnset:
		return ""
	case llm.ToolOutcomeRan:
		return session.ToolOutcomeRan
	case llm.ToolOutcomeFailed:
		return session.ToolOutcomeFailed
	case llm.ToolOutcomeAborted:
		return session.ToolOutcomeAborted
	}
	panic("turn: unknown tool outcome")
}

func messageRowOf(message llm.Message) MessageRow {
	row := MessageRow{
		Role:            message.Role.String(),
		Content:         message.Content,
		ToolCallID:      message.ToolCallID,
		ToolOutcome:     toolOutcomeName(message.ToolOutcome),
		ToolResultBytes: message.ToolResultBytes,
		Thinking:        message.Thinking.Text,
	}
	if id, encrypted, ok := codex.DecodeReasoning(message.Thinking.Signature); ok {
		row.Reasoning = &session.ReasoningItem{ID: id, EncryptedContent: encrypted}
	} else {
		row.ThinkingSignature = message.Thinking.Signature
	}
	for _, call := range message.ToolCalls {
		row.ToolCalls = append(row.ToolCalls, MessageToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return row
}

func messageOf(m MessageRow) (llm.Message, error) {
	signature := m.ThinkingSignature
	if m.Reasoning != nil {
		signature = codex.EncodeReasoning(m.Reasoning.ID, m.Reasoning.EncryptedContent)
	}
	message := llm.Message{Content: m.Content, ToolCallID: m.ToolCallID, ToolResultBytes: m.ToolResultBytes,
		Thinking: llm.Thinking{Text: m.Thinking, Signature: signature}}
	switch m.Role {
	case session.RoleSystem:
		message.Role = llm.RoleSystem
	case session.RoleUser:
		message.Role = llm.RoleUser
	case session.RoleAssistant:
		message.Role = llm.RoleAssistant
	case session.RoleTool:
		message.Role = llm.RoleTool
	default:
		return llm.Message{}, errors.New("turn: a recorded message names the role " + strconv.Quote(m.Role) + ", which is none this build sends")
	}
	switch m.ToolOutcome {
	case "":
		message.ToolOutcome = llm.ToolOutcomeUnset
	case session.ToolOutcomeRan:
		message.ToolOutcome = llm.ToolOutcomeRan
	case session.ToolOutcomeFailed:
		message.ToolOutcome = llm.ToolOutcomeFailed
	case session.ToolOutcomeAborted:
		message.ToolOutcome = llm.ToolOutcomeAborted
	default:
		return llm.Message{}, errors.New("turn: a recorded message names the tool outcome " + strconv.Quote(m.ToolOutcome) + ", which is none this build records")
	}
	for _, call := range m.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return message, nil
}

func ConversationFrom(events []session.Event) ([]llm.Message, error) {
	reading, err := session.ReadEvents(events)
	if err != nil {
		return nil, err
	}
	messages := make([]llm.Message, 0, len(reading.Messages))
	for _, recorded := range reading.Messages {
		message, err := messageOf(recorded.MessageBody)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return Sendable(messages), nil
}

func Sendable(messages []llm.Message) []llm.Message {
	answered := map[string]bool{}
	for _, message := range messages {
		if message.ToolCallID != "" {
			answered[message.ToolCallID] = true
		}
	}
	sendable := make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		var kept []llm.ToolCall
		for _, call := range message.ToolCalls {
			if answered[call.ID] {
				kept = append(kept, call)
			}
		}
		message.ToolCalls = kept
		if message.Role == llm.RoleAssistant && kept == nil && message.Content == "" {
			continue
		}
		sendable = append(sendable, message)
	}
	return sendable
}

func (r Row) PromptAccounting() llm.PromptAccounting {
	return llm.PromptAccountingFor(r.Wire)
}

func (r Row) Header() session.Header {
	header := session.Header{
		ID:         r.ID,
		At:         r.At,
		Task:       r.Task,
		Wire:       r.Wire,
		Model:      r.Model,
		Parent:     cmp.Or(r.ForkedFrom, r.SpawnedFrom),
		Root:       cmp.Or(r.Root, r.ID),
		Account:    r.Account,
		ForkedInto: r.ForkedInto,
		ForkKind:   string(r.ForkKind),
		Outcome:    r.Outcome.String(),
		CostUSD:    r.TotalCostUSD,
	}
	if r.Budget != (recall.Budget{}) {
		header.ContextCeiling = r.Budget.CeilingTokens
		header.ContextTarget = r.Budget.Bands.Target()
		header.AutoCompaction = r.Budget.Record()
	}
	for _, step := range r.Steps {
		if step.Fork != nil {
			header.ForkTokensBefore, header.ForkTokensAfter = step.Fork.TokensBefore, step.Fork.TokensAfter
		}
	}
	return header
}

func (r Row) Summary() Row {
	r.Steps = nil
	return r
}

func (r Row) author() string {
	if r.SpawnedFrom == "" {
		return session.AuthorOrchestrator
	}
	return r.ID
}

func (r Row) Record() (session.Header, []session.Event, error) {
	r.SearchLinks = SearchLinksOf(r.Conversation)
	author, last := r.author(), ""
	events := make([]session.Event, 0, len(r.Conversation)+len(r.Steps)+2)
	var failed error
	record := func(kind session.EventKind, attempt int, body any) {
		if failed != nil {
			return
		}
		raw, err := json.Marshal(body)
		if err != nil {
			failed = err
			return
		}
		id := session.NewEventID()
		events = append(events, session.Event{ID: id, Parent: last, Author: author, Attempt: max(attempt, session.FirstAttempt), Kind: kind, Body: raw})
		last = id
	}
	if r.System != "" || len(r.Tools) > 0 {
		record(session.EventPrompt, session.FirstAttempt, session.PromptBody{System: r.System, Tools: r.Tools})
	}
	for _, message := range r.Conversation {
		record(session.EventMessage, session.FirstAttempt, messageRowOf(message))
	}
	for _, step := range r.Steps {
		record(session.EventStep, step.attempt, step)
	}
	record(session.EventOutcome, session.FirstAttempt, r.Summary())
	if failed != nil {
		return session.Header{}, nil, failed
	}
	return r.Header(), events, nil
}
