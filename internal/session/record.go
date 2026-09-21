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

const SchemaVersion = 3

const AuthorOrchestrator = "orchestrator"

const eventIDBytes = 16

const FirstAttempt = 1

type EventKind string

const (
	EventStep       EventKind = "step"
	EventMessage    EventKind = "message"
	EventRead       EventKind = "read"
	EventOutcome    EventKind = "outcome"
	EventAttachment EventKind = "attachment"
)

type Attachment struct {
	File   string `json:"file"`
	Bytes  int    `json:"bytes"`
	Format string `json:"format"`
}

type EndReason string

const (
	EndedByNew   EndReason = "new"
	EndedByClose EndReason = "closed"
	EndedByFork  EndReason = "fork"
)

type Event struct {
	ID      string          `json:"id,omitempty"`
	Parent  string          `json:"parent,omitempty"`
	Author  string          `json:"author,omitempty"`
	Attempt int             `json:"attempt"`
	Kind    EventKind       `json:"kind"`
	Body    json.RawMessage `json:"body"`
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
	At               time.Time  `json:"at"`
	Task             string     `json:"task,omitempty"`
	Wire             string     `json:"wire,omitempty"`
	Model            string     `json:"model,omitempty"`
	Parent           string     `json:"parent,omitempty"`
	ContextCeiling   int        `json:"context_ceiling,omitempty"`
	ContextTarget    int        `json:"context_target,omitempty"`
	AutoCompaction   string     `json:"auto_compaction,omitempty"`
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
