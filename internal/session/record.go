package session

import (
	"encoding/json"
	"time"
)

const SchemaVersion = 1

type EventKind string

const (
	EventStep    EventKind = "step"
	EventMessage EventKind = "message"
	EventRead    EventKind = "read"
	EventOutcome EventKind = "outcome"
)

type EndReason string

const (
	EndedByNew   EndReason = "new"
	EndedByClose EndReason = "closed"
	EndedByFork  EndReason = "fork"
)

type Event struct {
	Kind EventKind       `json:"kind"`
	Body json.RawMessage `json:"body"`
}

type Header struct {
	ID               string     `json:"id"`
	Schema           int        `json:"schema"`
	Name             *string    `json:"name,omitempty"`
	At               time.Time  `json:"at"`
	Task             string     `json:"task,omitempty"`
	Wire             string     `json:"wire,omitempty"`
	Model            string     `json:"model,omitempty"`
	Parent           string     `json:"parent,omitempty"`
	Root             string     `json:"root"`
	ForkedInto       string     `json:"forked_into,omitempty"`
	ForkKind         string     `json:"fork_kind,omitempty"`
	ForkTokensBefore int        `json:"fork_tokens_before,omitempty"`
	ForkTokensAfter  int        `json:"fork_tokens_after,omitempty"`
	Outcome          string     `json:"outcome,omitempty"`
	CostUSD          float64    `json:"cost_usd,omitempty"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	EndReason        EndReason  `json:"end_reason,omitempty"`
}

func (h Header) Ended() bool { return h.EndedAt != nil }

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
