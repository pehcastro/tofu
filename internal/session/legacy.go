package session

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	legacyHeaderName = "header.json"
	legacyBodyName   = "body.jsonl"
	legacySuffix     = ".json"
)

type legacyHeader struct {
	ID               string     `json:"id"`
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
	Account          int64      `json:"account,omitempty"`
	ForkedInto       string     `json:"forked_into,omitempty"`
	ForkKind         string     `json:"fork_kind,omitempty"`
	ForkTokensBefore int        `json:"fork_tokens_before,omitempty"`
	ForkTokensAfter  int        `json:"fork_tokens_after,omitempty"`
	Outcome          string     `json:"outcome,omitempty"`
	CostUSD          float64    `json:"cost_usd,omitempty"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	EndReason        EndReason  `json:"end_reason,omitempty"`
}

func (h legacyHeader) header() Header {
	header := Header{
		ID: h.ID, Name: h.Name, At: h.At, EndedAt: h.EndedAt, EndReason: h.EndReason,
		ForkedInto: h.ForkedInto, ForkKind: h.ForkKind, ForkTokensBefore: h.ForkTokensBefore, ForkTokensAfter: h.ForkTokensAfter,
		Root: cmp.Or(h.Root, h.ID), Task: h.Task, Wire: h.Wire, Model: h.Model, Outcome: h.Outcome, Account: h.Account,
		ContextCeiling: h.ContextCeiling, ContextTarget: h.ContextTarget, AutoCompaction: h.AutoCompaction, CostUSD: h.CostUSD,
	}
	if h.Parent != "" {
		header.CarriedFrom, header.Parent = &Carried{Session: h.Parent}, h.Parent
	}
	return header
}

type singleFileRow struct {
	ID           string            `json:"id"`
	At           time.Time         `json:"at"`
	Task         string            `json:"task"`
	Wire         string            `json:"wire"`
	Model        string            `json:"model"`
	Account      int64             `json:"account"`
	SpawnedFrom  string            `json:"spawned_from"`
	ForkedFrom   string            `json:"forked_from"`
	ForkedInto   string            `json:"forked_into"`
	ForkKind     string            `json:"fork_kind"`
	Outcome      json.RawMessage   `json:"outcome"`
	TotalCostUSD float64           `json:"total_cost_usd"`
	Steps        []json.RawMessage `json:"steps"`
}

func outcomeName(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return name, nil
	}
	var ordinal int
	if err := json.Unmarshal(raw, &ordinal); err != nil {
		return "", fmt.Errorf("the outcome %s is neither a name nor the number an earlier schema wrote", raw)
	}
	if ordinal < int(OutcomeUnset) || ordinal >= int(outcomeCount) {
		return "", fmt.Errorf("the outcome %d is not one this build has a name for", ordinal)
	}
	return Outcome(ordinal).String(), nil
}

func (s *Store) legacy(id string) bool {
	if namesOneSession(id) != nil {
		return false
	}
	for _, path := range []string{filepath.Join(s.Dir(id), legacyHeaderName), filepath.Join(s.Dir(id), legacyBodyName), filepath.Join(s.dir, id+legacySuffix)} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func (s *Store) legacyHeader(id string) (Header, error) {
	raw, err := s.readFile(filepath.Join(s.Dir(id), legacyHeaderName))
	if err == nil {
		var old legacyHeader
		if err := json.Unmarshal(raw, &old); err != nil {
			return Header{}, fmt.Errorf("session: the header of %s does not parse: %w", id, err)
		}
		return old.header(), nil
	}
	header, _, singleErr := s.singleFile(id)
	if singleErr == nil {
		return header, nil
	}
	if errors.Is(singleErr, fs.ErrNotExist) {
		return Header{}, fmt.Errorf("session: %s reads as no session: %w", id, errors.Join(err, singleErr))
	}
	return Header{}, singleErr
}

func (s *Store) legacyEvents(id string) ([]Event, error) {
	header, headerErr := s.legacyHeader(id)
	if headerErr != nil {
		header = Header{ID: id}
	}
	raw, err := s.readFile(filepath.Join(s.Dir(id), legacyBodyName))
	if err != nil {
		_, events, singleErr := s.singleFile(id)
		if singleErr != nil {
			return nil, fmt.Errorf("session: %s has no body in either old shape: %w", id, errors.Join(err, singleErr))
		}
		return convertTurns(header, events), nil
	}
	events, err := parseEvents(raw, id)
	if err != nil {
		return nil, err
	}
	return convertTurns(header, events), nil
}

func (s *Store) singleFile(id string) (Header, []Event, error) {
	raw, err := s.readFile(filepath.Join(s.dir, id+legacySuffix))
	if err != nil {
		return Header{}, nil, err
	}
	var row singleFileRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return Header{}, nil, fmt.Errorf("session: %s is not a turn row: %w", id, err)
	}
	outcome, err := outcomeName(row.Outcome)
	if err != nil {
		return Header{}, nil, fmt.Errorf("session: %s: %w", id, err)
	}
	old := legacyHeader{ID: cmp.Or(row.ID, id), At: row.At, Task: row.Task, Wire: row.Wire, Model: row.Model, Account: row.Account,
		Parent: cmp.Or(row.ForkedFrom, row.SpawnedFrom), ForkedInto: row.ForkedInto, ForkKind: row.ForkKind, Outcome: outcome, CostUSD: row.TotalCostUSD}
	events := make([]Event, 0, len(row.Steps)+1)
	for _, step := range row.Steps {
		events = append(events, Event{Kind: EventStep, Body: step})
	}
	return old.header(), append(events, Event{Kind: EventOutcome, Body: raw}), nil
}

type legacyRow struct {
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Task    string    `json:"task"`
	Wire    string    `json:"wire"`
	Spend   string    `json:"spend"`
	Account int64     `json:"account"`
}

type converter struct {
	scope    string
	at       time.Time
	task     string
	said     []MessageBody
	out      []Event
	place    int
	spoken   int
	turnAt   time.Time
	turnID   string
	steps    []*pairedStep
	rows     map[string]json.RawMessage
	requests map[string]string
}

func convertTurns(header Header, events []Event) []Event {
	c := &converter{scope: header.ID, at: header.At, task: header.Task}
	var segment []Event
	for _, event := range events {
		segment = append(segment, event)
		if event.Kind == EventOutcome {
			c.turn(segment)
			segment = nil
		}
	}
	if len(segment) > 0 {
		c.turn(segment)
	}
	return c.out
}

func (c *converter) id(kept, kind string) string {
	c.place++
	return cmp.Or(kept, EventIDFor(c.scope, kind+":"+strconv.Itoa(c.place)))
}

func (c *converter) spokenID(kept string) string {
	c.spoken++
	return cmp.Or(kept, EventIDFor(c.scope, "step:"+strconv.Itoa(c.spoken-1)))
}

func (c *converter) emit(event Event) {
	event.Parent, event.Author, event.Seq = "", "", 0
	if event.At.IsZero() {
		event.At = c.turnAt
	}
	event.Turn = cmp.Or(event.Turn, c.turnID)
	c.out = append(c.out, event)
}

type pairedStep struct {
	place    int
	event    Event
	body     StepBody
	calls    []json.RawMessage
	consumed bool
}

func (c *converter) turn(segment []Event) {
	var row legacyRow
	isTurn := false
	var steps []*pairedStep
	for place, event := range segment {
		switch event.Kind {
		case EventOutcome:
			isTurn = true
			_ = json.Unmarshal(event.Body, &row)
		case EventStep:
			isTurn = true
			step := &pairedStep{place: place, event: event}
			_ = json.Unmarshal(event.Body, &step.body)
			var calls struct {
				ToolCalls []json.RawMessage `json:"tool_calls"`
			}
			_ = json.Unmarshal(event.Body, &calls)
			step.calls = calls.ToolCalls
			steps = append(steps, step)
		}
	}
	c.turnAt, c.turnID, c.steps, c.rows, c.requests = row.At, cmp.Or(row.ID, c.scope), steps, map[string]json.RawMessage{}, map[string]string{}
	if c.turnAt.IsZero() {
		c.turnAt = c.at
	}
	if isTurn {
		c.emit(Event{ID: c.id("", "turn_start"), Kind: EventTurnStart, Body: marshalled(TurnStart{Task: cmp.Or(row.Task, c.task), Wire: row.Wire, Spend: row.Spend, Account: row.Account})})
	}
	fresh := c.unseen(segment)
	for place, event := range segment {
		switch event.Kind {
		case EventMessage:
			if !fresh[place] {
				continue
			}
			var body MessageBody
			_ = json.Unmarshal(event.Body, &body)
			c.message(place, event, body)
		case EventStep:
			for _, step := range steps {
				if step.place == place && !step.consumed {
					c.request(step, "", false)
				}
			}
		case EventOutcome:
			c.emit(Event{ID: c.id(event.ID, "turn_end"), Kind: EventTurnEnd, Body: without(event.Body, "steps")})
		case EventRead:
		default:
			event.ID = c.id(event.ID, string(event.Kind))
			c.emit(event)
		}
	}
}

func (c *converter) unseen(segment []Event) map[int]bool {
	fresh := map[int]bool{}
	matched, from := true, 0
	for index, event := range segment {
		if event.Kind != EventMessage {
			continue
		}
		var body MessageBody
		_ = json.Unmarshal(event.Body, &body)
		if matched {
			found := -1
			for at := from; at < len(c.said) && found < 0; at++ {
				if sameMessage(c.said[at], body) {
					found = at
				}
			}
			if found >= 0 {
				from = found + 1
				continue
			}
			matched = false
		}
		fresh[index] = true
		c.said = append(c.said, body)
	}
	return fresh
}

func sameMessage(said, again MessageBody) bool {
	if said.Role != again.Role || said.Content != again.Content || said.ToolCallID != again.ToolCallID {
		return false
	}
	for _, call := range again.ToolCalls {
		found := false
		for _, earlier := range said.ToolCalls {
			found = found || earlier.ID == call.ID
		}
		if !found {
			return false
		}
	}
	return true
}

func (c *converter) message(place int, event Event, body MessageBody) {
	switch body.Role {
	case RoleTool:
		result := objectOf(c.rows[body.ToolCallID])
		result["content"] = marshalled(body.Content)
		if body.ToolOutcome != "" {
			result["tool_outcome"] = marshalled(body.ToolOutcome)
		}
		if _, has := result["result_bytes"]; !has {
			result["result_bytes"] = marshalled(body.ToolResultBytes)
		}
		c.emit(Event{ID: c.spokenID(event.ID), Kind: EventToolResult, Call: body.ToolCallID, Request: c.requests[body.ToolCallID], Body: marshalled(result)})
		return
	case RoleAssistant:
	default:
		event.ID = c.spokenID(event.ID)
		c.emit(event)
		return
	}
	var step *pairedStep
	for _, candidate := range c.steps {
		if step == nil && candidate.place > place && !candidate.consumed && (len(candidate.calls) > 0) == (len(body.ToolCalls) > 0) {
			step = candidate
		}
	}
	request := c.id("", "request")
	if step != nil {
		request = cmp.Or(step.event.ID, request)
	}
	calls := body.ToolCalls
	body.ToolCalls = nil
	c.emit(Event{ID: c.spokenID(event.ID), Kind: EventMessage, Request: request, Body: marshalled(body)})
	for index, call := range calls {
		row := step.rowFor(index, call.ID)
		c.requests[call.ID], c.rows[call.ID] = request, callMetadata(row)
		c.emit(Event{ID: cmp.Or(stringField(row, "id"), EventIDFor(c.turnID, call.ID)), Kind: EventToolCall, Call: call.ID, Request: request, Body: marshalled(CallBody{Tool: call.Name, Args: call.Arguments})})
	}
	if step != nil {
		step.event.ID = request
		c.request(step, body.Content, true)
	}
}

func (c *converter) request(step *pairedStep, said string, callsWritten bool) {
	step.consumed = true
	request := c.id(step.event.ID, "request")
	body := objectOf(step.event.Body)
	delete(body, "tool_calls")
	if step.body.AssistantText != "" && step.body.AssistantText == said {
		delete(body, "assistant_text")
	}
	compacted := map[string]json.RawMessage{}
	for _, key := range []string{"compaction", "fork"} {
		if value, has := body[key]; has && string(value) != "null" {
			compacted[key] = value
		}
		delete(body, key)
	}
	c.emit(Event{ID: request, Kind: EventRequest, Request: request, Attempt: step.event.Attempt, Body: marshalled(body)})
	if len(compacted) > 0 {
		c.emit(Event{ID: c.id("", "compaction"), Kind: EventCompaction, Request: request, Body: marshalled(compacted)})
	}
	if callsWritten {
		return
	}
	for index, row := range step.calls {
		call := cmp.Or(stringField(row, "call"), request+"."+strconv.Itoa(index+1))
		var asked CallBody
		_ = json.Unmarshal(row, &asked)
		c.emit(Event{ID: cmp.Or(stringField(row, "id"), EventIDFor(c.turnID, call)), Kind: EventToolCall, Call: call, Request: request, Body: marshalled(asked)})
		c.emit(Event{ID: c.id("", "tool_result"), Kind: EventToolResult, Call: call, Request: request, Body: callMetadata(row)})
	}
}

func (step *pairedStep) rowFor(index int, call string) json.RawMessage {
	if step == nil {
		return nil
	}
	for _, row := range step.calls {
		if stringField(row, "call") == call {
			return row
		}
	}
	if index < len(step.calls) {
		return step.calls[index]
	}
	return nil
}

func callMetadata(row json.RawMessage) json.RawMessage {
	if _, rewritten := objectOf(row)["proxy"]; rewritten {
		return without(row, "tool", "id", "parent", "author", "call")
	}
	return without(row, "tool", "args", "id", "parent", "author", "call")
}

func objectOf(raw json.RawMessage) map[string]json.RawMessage {
	object := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &object)
	return object
}

func without(raw json.RawMessage, keys ...string) json.RawMessage {
	object := objectOf(raw)
	for _, key := range keys {
		delete(object, key)
	}
	return marshalled(object)
}

func stringField(raw json.RawMessage, key string) string {
	var text string
	_ = json.Unmarshal(objectOf(raw)[key], &text)
	return text
}

func marshalled(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic("session: a value built here does not marshal: " + err.Error())
	}
	return raw
}

func entryIDs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case entry.IsDir() && !strings.HasPrefix(name, "."):
			ids = append(ids, name)
		case !entry.IsDir() && filepath.Ext(name) == legacySuffix:
			ids = append(ids, strings.TrimSuffix(name, legacySuffix))
		}
	}
	return ids, nil
}
