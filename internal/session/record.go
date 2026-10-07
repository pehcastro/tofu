package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const SchemaVersion = 5

const AuthorOrchestrator = "orchestrator"

const eventIDBytes = 16

const FirstAttempt = 1

type EventKind string

const (
	EventTurnStart  EventKind = "turn_start"
	EventPrompt     EventKind = "prompt"
	EventRequest    EventKind = "request"
	EventMessage    EventKind = "message"
	EventToolCall   EventKind = "tool_call"
	EventToolResult EventKind = "tool_result"
	EventSpawn      EventKind = "spawn"
	EventAgentEnd   EventKind = "agent_end"
	EventCompaction EventKind = "compaction"
	EventTurnEnd    EventKind = "turn_end"
	EventAttachment EventKind = "attachment"
	EventOutcome    EventKind = "outcome"
	EventStep       EventKind = "step"
	EventRead       EventKind = "read"
	EventNotice     EventKind = "notice"
	EventListChange EventKind = "list_change"
	EventReport     EventKind = "report"
	EventHook       EventKind = "hook"
)

type NoticeBody struct {
	Text string `json:"text"`
}

type ListChangeBody struct {
	Kind   string   `json:"kind"`
	Why    string   `json:"why,omitempty"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after"`
}

type PromptBody struct {
	System string   `json:"system"`
	Tools  []string `json:"tools,omitempty"`
}

type Attachment struct {
	File   string `json:"file"`
	Bytes  int    `json:"bytes"`
	Format string `json:"format"`
}

type TurnStart struct {
	Task           string `json:"task"`
	Wire           string `json:"wire,omitempty"`
	Spend          string `json:"spend,omitempty"`
	Account        int64  `json:"account,omitempty"`
	ContextCeiling int    `json:"context_ceiling,omitempty"`
	ContextTarget  int    `json:"context_target,omitempty"`
	AutoCompaction string `json:"auto_compaction,omitempty"`
}

type CallBody struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args,omitempty"`
}

type ResultBody struct {
	Content     string `json:"content"`
	ToolOutcome string `json:"tool_outcome,omitempty"`
	ResultBytes int    `json:"result_bytes"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	Error       string `json:"error,omitempty"`
}

type SpawnBody struct {
	Agent      string   `json:"agent"`
	Definition string   `json:"definition,omitempty"`
	Model      string   `json:"model,omitempty"`
	Mission    string   `json:"mission,omitempty"`
	Owns       []string `json:"owns,omitempty"`
	Depth      int      `json:"depth"`
}

type ReportBody struct {
	State string `json:"state"`
	Text  string `json:"text"`
}

type AgentEndBody struct {
	Status  string  `json:"status"`
	Usage   Usage   `json:"usage"`
	CostUSD float64 `json:"cost_usd"`
}

type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

func (u Usage) Plus(other Usage) Usage {
	return Usage{
		InputTokens:      u.InputTokens + other.InputTokens,
		OutputTokens:     u.OutputTokens + other.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens + other.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens + other.CacheWriteTokens,
	}
}

type AgentRun struct {
	Agent       string     `json:"agent"`
	Definition  string     `json:"definition,omitempty"`
	Model       string     `json:"model,omitempty"`
	ParentAgent string     `json:"parent_agent,omitempty"`
	SpawnCall   string     `json:"spawn_call,omitempty"`
	SpawnTurn   string     `json:"spawn_turn"`
	Depth       int        `json:"depth"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	Usage       Usage      `json:"usage"`
	CostUSD     float64    `json:"cost_usd"`
}

type Carried struct {
	Session string `json:"session"`
	Event   string `json:"event,omitempty"`
}

type EndReason string

const (
	EndedByNew   EndReason = "new"
	EndedByClose EndReason = "closed"
	EndedByFork  EndReason = "fork"
)

type Event struct {
	Seq       int             `json:"seq,omitempty"`
	ID        string          `json:"id,omitempty"`
	Parent    string          `json:"parent,omitempty"`
	At        time.Time       `json:"at"`
	Turn      string          `json:"turn,omitempty"`
	Agent     string          `json:"agent,omitempty"`
	SpawnedBy string          `json:"spawned_by,omitempty"`
	Request   string          `json:"request,omitempty"`
	Call      string          `json:"call,omitempty"`
	Author    string          `json:"author,omitempty"`
	Attempt   int             `json:"attempt,omitempty"`
	Kind      EventKind       `json:"kind"`
	Body      json.RawMessage `json:"body,omitempty"`
}

func NewEventID() string {
	var raw [eventIDBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("session: the system random source failed: " + err.Error())
	}
	return uuidOf(raw)
}

func EventIDFor(scope, key string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + key))
	var raw [eventIDBytes]byte
	copy(raw[:], sum[:])
	return uuidOf(raw)
}

func uuidOf(raw [eventIDBytes]byte) string {
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

var (
	ErrEventHashNotFound  = errors.New("session: nothing in this session ends with that id")
	ErrEventHashAmbiguous = errors.New("session: more than one event in this session ends with that id")
)

func DrawnAs(id, hash string) bool {
	if id == "" || hash == "" {
		return false
	}
	return strings.HasSuffix(strings.ToLower(id), strings.ToLower(strings.TrimPrefix(hash, "#")))
}

func FindByHash(events []Event, hash string) (Event, error) {
	var found []Event
	for _, event := range events {
		if DrawnAs(event.ID, hash) {
			found = append(found, event)
		}
	}
	switch len(found) {
	case 0:
		return Event{}, fmt.Errorf("%w: %q", ErrEventHashNotFound, hash)
	case 1:
		return found[0], nil
	default:
		return Event{}, fmt.Errorf("%w: %q matches %d events", ErrEventHashAmbiguous, hash, len(found))
	}
}

type Header struct {
	ID               string     `json:"id"`
	Schema           int        `json:"schema"`
	Name             *string    `json:"name,omitempty"`
	Project          string     `json:"project,omitempty"`
	At               time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	EndReason        EndReason  `json:"end_reason,omitempty"`
	Head             string     `json:"head,omitempty"`
	CarriedFrom      *Carried   `json:"carried_from,omitempty"`
	BranchedFrom     *Carried   `json:"branched_from,omitempty"`
	Parent           string     `json:"-"`
	ForkedInto       string     `json:"forked_into,omitempty"`
	ForkKind         string     `json:"fork_kind,omitempty"`
	ForkTokensBefore int        `json:"fork_tokens_before,omitempty"`
	ForkTokensAfter  int        `json:"fork_tokens_after,omitempty"`
	Root             string     `json:"root,omitempty"`
	Generation       int        `json:"generation,omitempty"`
	Task             string     `json:"task,omitempty"`
	Wire             string     `json:"wire,omitempty"`
	Model            string     `json:"model,omitempty"`
	Models           []string   `json:"models,omitempty"`
	Outcome          string     `json:"outcome,omitempty"`
	Error            string     `json:"error,omitempty"`
	Account          int64      `json:"account,omitempty"`
	ContextCeiling   int        `json:"context_ceiling,omitempty"`
	ContextTarget    int        `json:"context_target,omitempty"`
	AutoCompaction   string     `json:"auto_compaction,omitempty"`
	Turns            int        `json:"turns"`
	Usage            Usage      `json:"usage"`
	CostUSD          float64    `json:"cost_usd"`
	Agents           []AgentRun `json:"agents,omitempty"`
}

func (h Header) Ended() bool { return h.EndedAt != nil }

func (h Header) Named() string {
	if h.Name == nil {
		return ""
	}
	return *h.Name
}

func (h Header) LastAt() time.Time {
	if h.EndedAt == nil {
		return h.At
	}
	return *h.EndedAt
}

type Head struct {
	ID      string
	Derived bool
}

type Skip struct {
	ID     string
	Reason error
}

type Listing struct {
	Sessions []Header
	Skipped  []Skip
}
