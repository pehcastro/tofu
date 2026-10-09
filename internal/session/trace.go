package session

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

type AttemptView struct {
	Detail json.RawMessage `json:"detail"`
	Body   json.RawMessage `json:"body,omitempty"`
}

type RequestView struct {
	Request    string          `json:"request"`
	Agent      string          `json:"agent,omitempty"`
	Turn       string          `json:"turn,omitempty"`
	At         time.Time       `json:"at"`
	Why        string          `json:"why,omitempty"`
	Wire       string          `json:"wire,omitempty"`
	Model      string          `json:"model,omitempty"`
	ToolChoice string          `json:"tool_choice,omitempty"`
	DurationMS int64           `json:"duration_ms"`
	Messages   []MessageBody   `json:"messages"`
	Tools      json.RawMessage `json:"tools,omitempty"`
	Attempts   []AttemptView   `json:"attempts,omitempty"`
	Response   json.RawMessage `json:"response,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type TracedInsert struct {
	Agent    string     `json:"agent,omitempty"`
	Turn     string     `json:"turn,omitempty"`
	Origin   string     `json:"origin"`
	Role     string     `json:"role"`
	PostedAt *time.Time `json:"posted_at,omitempty"`
	TakenAt  *time.Time `json:"taken_at,omitempty"`
	Request  string     `json:"first_sent_in,omitempty"`
	Text     string     `json:"text"`
}

type TracedChange struct {
	Agent  string `json:"agent,omitempty"`
	Turn   string `json:"turn,omitempty"`
	Kind   string `json:"kind"`
	Why    string `json:"why,omitempty"`
	Before int    `json:"before"`
	After  int    `json:"after"`
}

type TracedNotice struct {
	Agent string    `json:"agent,omitempty"`
	Turn  string    `json:"turn,omitempty"`
	At    time.Time `json:"at"`
	Text  string    `json:"text"`
}

type TracedMemory struct {
	Agent   string    `json:"agent,omitempty"`
	Turn    string    `json:"turn,omitempty"`
	Request string    `json:"request"`
	At      time.Time `json:"at"`
	Why     string    `json:"why,omitempty"`
	Model   string    `json:"model,omitempty"`
	Usage   Usage     `json:"usage"`
	CostUSD float64   `json:"cost_usd"`
}

type Traced struct {
	Exchanges []Exchange
	Memory    []TracedMemory
	Inserted  []TracedInsert
	Changes   []TracedChange
	Notices   []TracedNotice
}

func (s *Store) Traced(id string, events []Event) (Traced, error) {
	exchanges, err := s.Exchanges(id)
	if err != nil {
		return Traced{}, err
	}
	traced := Traced{Exchanges: exchanges}
	for _, event := range events {
		switch event.Kind {
		case EventMessage:
			var message MessageBody
			if json.Unmarshal(event.Body, &message) != nil || message.Origin == "" {
				continue
			}
			insert := TracedInsert{Agent: event.Agent, Turn: event.Turn, Origin: message.Origin, Role: message.Role, PostedAt: message.PostedAt,
				TakenAt: message.TakenAt, Text: message.Content}
			hash := BlobHash(event.Body)
			for _, exchange := range exchanges {
				if insert.Request == "" && exchange.Agent == event.Agent && slices.Contains(exchange.Messages, hash) {
					insert.Request = exchange.Request
				}
			}
			traced.Inserted = append(traced.Inserted, insert)
		case EventListChange:
			var change ListChangeBody
			if json.Unmarshal(event.Body, &change) == nil {
				traced.Changes = append(traced.Changes, TracedChange{Agent: event.Agent, Turn: event.Turn, Kind: change.Kind, Why: change.Why,
					Before: len(change.Before), After: len(change.After)})
			}
		case EventNotice:
			var notice NoticeBody
			if json.Unmarshal(event.Body, &notice) == nil {
				traced.Notices = append(traced.Notices, TracedNotice{Agent: event.Agent, Turn: event.Turn, At: event.At, Text: notice.Text})
			}
		case EventMemoryRequest:
			var step StepBody
			_ = json.Unmarshal(event.Body, &step)
			call := TracedMemory{Agent: event.Agent, Turn: event.Turn, Request: cmp.Or(event.Request, event.ID), At: event.At, Model: step.Model,
				Usage: step.usage(), CostUSD: step.CostUSD}
			if at := slices.IndexFunc(exchanges, func(exchange Exchange) bool { return exchange.Request == call.Request }); at >= 0 {
				call.Why = exchanges[at].Why
			}
			traced.Memory = append(traced.Memory, call)
		}
	}
	return traced, nil
}

func (s *Store) Request(id, handle string) (RequestView, error) {
	exchanges, err := s.Exchanges(id)
	if err != nil {
		return RequestView{}, err
	}
	var found []Exchange
	for _, exchange := range exchanges {
		if DrawnAs(exchange.Request, handle) {
			found = append(found, exchange)
		}
	}
	switch len(found) {
	case 0:
		return RequestView{}, fmt.Errorf("%w: %q", ErrEventHashNotFound, handle)
	case 1:
	default:
		return RequestView{}, fmt.Errorf("%w: %q matches %d requests", ErrEventHashAmbiguous, handle, len(found))
	}
	blobs, err := s.Blobs(id)
	if err != nil {
		return RequestView{}, err
	}
	exchange := found[0]
	view := RequestView{Request: exchange.Request, Agent: exchange.Agent, Turn: exchange.Turn, At: exchange.At, Why: exchange.Why, Wire: exchange.Wire,
		Model: exchange.Model, ToolChoice: exchange.ToolChoice, DurationMS: exchange.DurationMS, Tools: blobs[exchange.Tools],
		Response: blobs[exchange.Response], Error: exchange.Error, Messages: make([]MessageBody, len(exchange.Messages))}
	for i, hash := range exchange.Messages {
		if err := json.Unmarshal(blobs[hash], &view.Messages[i]); err != nil {
			return RequestView{}, fmt.Errorf("message %d of request %s, blob %s: %w", i, exchange.Request, hash, err)
		}
	}
	for _, attempt := range exchange.Attempts {
		shown := AttemptView{Detail: attempt.Detail}
		if len(attempt.Body) > 0 {
			shown.Body = Assemble(attempt.Body, blobs)
		}
		view.Attempts = append(view.Attempts, shown)
	}
	return view, nil
}
