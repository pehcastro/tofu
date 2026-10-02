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
	Call   string `json:"call,omitempty"`

	Tool              string          `json:"tool,omitempty"`
	Args              json.RawMessage `json:"args,omitempty"`
	Command           string          `json:"command,omitempty"`
	Proxy             *ProxyRow       `json:"proxy,omitempty"`
	SubAgentID        string          `json:"sub_agent_id,omitempty"`
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
	Model            string            `json:"model,omitempty"`
	StopReason       string            `json:"stop_reason,omitempty"`
	PromptTokens     int               `json:"prompt_tokens"`
	CompletionTokens int               `json:"completion_tokens"`
	ReasoningTokens  int               `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int               `json:"cache_read_tokens"`
	CacheWriteTokens int               `json:"cache_write_tokens"`
	CostUSD          float64           `json:"cost_usd"`
	DurationMS       int64             `json:"duration_ms"`
	FirstTokenMS     int64             `json:"first_token_ms"`
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
	Session      string       `json:"session,omitempty"`
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
	SpawnedBy    string       `json:"spawned_by,omitempty"`
	ForkedFrom   string       `json:"forked_from,omitempty"`
	ForkedInto   string       `json:"forked_into,omitempty"`
	ForkKind     ForkKind     `json:"fork_kind,omitempty"`
	Warnings     []string     `json:"warnings,omitempty"`
	SubAgentIDs  []string     `json:"sub_agent_ids"`
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
		Root:       cmp.Or(r.Root, r.ID),
		Account:    r.Account,
		ForkedInto: r.ForkedInto,
		ForkKind:   string(r.ForkKind),
		Outcome:    r.Outcome.String(),
		CostUSD:    r.TotalCostUSD,
	}
	if parent := cmp.Or(r.ForkedFrom, r.SpawnedFrom); parent != "" {
		header.CarriedFrom = &session.Carried{Session: parent}
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

type record struct {
	store     *session.Store
	inbox     *Inbox
	log       *session.Log
	own       bool
	scope     string
	turn      string
	agent     string
	spawnedBy string
	said      map[string]string
	failed    []string
}

type resultRow struct {
	ToolCallRow
	Content     string `json:"content"`
	ToolOutcome string `json:"tool_outcome,omitempty"`
}

type compactionRow struct {
	Compaction *Compaction `json:"compaction,omitempty"`
	Fork       *Fork       `json:"fork,omitempty"`
}

func openRecord(config Config, row Row) (*record, error) {
	opened := &record{store: config.Sessions, inbox: config.Inbox, log: config.Log, scope: row.ID, turn: row.ID, said: map[string]string{}}
	switch {
	case config.Log != nil:
		opened.turn, opened.agent, opened.spawnedBy = config.Turn, row.ID, config.SpawnedBy
	case config.Sessions != nil:
		log, err := config.Inbox.open(config.Sessions, session.Header{ID: config.Session, At: row.At})
		if err != nil {
			return nil, err
		}
		opened.log, opened.own = log, true
	default:
		return nil, nil
	}
	return opened, nil
}

func (r *record) session() string {
	if r == nil {
		return ""
	}
	return r.log.ID()
}

func (r *record) note(err error) {
	if err != nil {
		r.failed = append(r.failed, err.Error())
	}
}

func (r *record) add(event session.Event, body any) {
	if r == nil {
		return
	}
	event.Turn, event.Agent, event.SpawnedBy = r.turn, r.agent, r.spawnedBy
	_, err := r.log.Append(event, body)
	r.note(err)
}

func (r *record) begin(row Row) {
	if r == nil {
		return
	}
	start := session.TurnStart{Task: row.Task, Wire: row.Wire, Spend: string(row.Spend), Account: row.Account}
	if row.Budget != (recall.Budget{}) {
		start.ContextCeiling, start.ContextTarget, start.AutoCompaction = row.Budget.CeilingTokens, row.Budget.Bands.Target(), row.Budget.Record()
	}
	r.add(session.Event{Kind: session.EventTurnStart}, start)
	if prompt := (session.PromptBody{System: row.System, Tools: row.Tools}); !r.log.Prompted(r.agent, prompt) {
		r.add(session.Event{Kind: session.EventPrompt}, prompt)
	}
	if !r.own {
		return
	}
	r.note(r.log.Edit(func(header *session.Header) {
		header.Turns++
		header.Task = cmp.Or(header.Task, row.Task)
		header.Wire, header.Account = row.Wire, row.Account
		header.ContextCeiling, header.ContextTarget, header.AutoCompaction = start.ContextCeiling, start.ContextTarget, start.AutoCompaction
	}))
}

func (r *record) message(message llm.Message, request string, results map[string]ToolCallRow) {
	if r == nil {
		return
	}
	switch message.Role {
	case llm.RoleSystem:
	case llm.RoleTool:
		row := results[message.ToolCallID]
		row.ID, row.Parent, row.Author, row.Call, row.Tool = "", "", "", "", ""
		if row.Proxy == nil {
			row.Args = nil
		}
		r.add(session.Event{Kind: session.EventToolResult, Call: message.ToolCallID, Request: request},
			resultRow{ToolCallRow: row, Content: message.Content, ToolOutcome: toolOutcomeName(message.ToolOutcome)})
	case llm.RoleAssistant:
		body := messageRowOf(message)
		body.ToolCalls = nil
		r.said[request] = message.Content
		r.add(session.Event{Kind: session.EventMessage, Request: request}, body)
		for _, call := range message.ToolCalls {
			r.add(session.Event{ID: session.EventIDFor(r.scope, call.ID), Kind: session.EventToolCall, Call: call.ID, Request: request},
				session.CallBody{Tool: call.Name, Args: call.Arguments})
		}
	default:
		r.add(session.Event{Kind: session.EventMessage}, messageRowOf(message))
	}
}

func (r *record) step(step StepRow) {
	if r == nil {
		return
	}
	asked := step
	asked.ToolCalls, asked.Compaction, asked.Fork = nil, nil, nil
	if asked.AssistantText == r.said[step.id] {
		asked.AssistantText = ""
	}
	r.add(session.Event{ID: step.id, Kind: session.EventRequest, Request: step.id, Attempt: max(step.attempt, session.FirstAttempt)}, asked)
	if step.Compaction != nil || step.Fork != nil {
		r.add(session.Event{Kind: session.EventCompaction, Request: step.id}, compactionRow{Compaction: step.Compaction, Fork: step.Fork})
	}
	r.log.Spent(r.agent, step.Model, session.Usage{InputTokens: step.PromptTokens, OutputTokens: step.CompletionTokens,
		CacheReadTokens: step.CacheReadTokens, CacheWriteTokens: step.CacheWriteTokens}, step.CostUSD)
}

func (r *record) end(row Row) {
	if r == nil {
		return
	}
	r.add(session.Event{Kind: session.EventTurnEnd}, row.Summary())
	if !r.own {
		return
	}
	r.note(r.log.Edit(func(header *session.Header) {
		header.Outcome, header.Model = row.Outcome.String(), cmp.Or(row.Model, header.Model)
	}))
	r.note(r.inbox.release(r.log))
}

func (r *record) fork(ended, next Row, fork *Fork, at time.Time) {
	if r == nil || !r.own {
		return
	}
	into, from := session.NewEventID(), r.log.Header()
	r.add(session.Event{Kind: session.EventTurnEnd}, ended.Summary())
	r.note(r.log.Edit(func(header *session.Header) {
		header.Outcome, header.Model, header.ForkedInto = ended.Outcome.String(), cmp.Or(ended.Model, header.Model), into
		header.EndedAt, header.EndReason = &at, session.EndedByFork
		header.ForkTokensBefore, header.ForkTokensAfter = fork.TokensBefore, fork.TokensAfter
	}))
	r.note(r.inbox.release(r.log))
	log, err := r.inbox.open(r.store, session.Header{ID: into, At: at, ForkKind: string(fork.Kind), Root: cmp.Or(from.Root, from.ID),
		CarriedFrom: &session.Carried{Session: from.ID, Event: r.log.Header().Head}})
	if err != nil {
		r.note(err)
		return
	}
	r.log, r.turn = log, next.ID
	r.begin(next)
}
