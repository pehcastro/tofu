package session

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const convertingPrefix = ".converting-"

type Converted struct {
	Header Header
	Events []Event
	From   []string
}

type Conversion struct {
	Sessions []Converted
	Skipped  []Skip
	Head     string
}

const legacySubAgentKey = "child_id"

type oldEntry struct {
	id        string
	header    Header
	events    []Event
	parent    string
	subAgents []string
	session   string
}

func (s *Store) PlanConversion() (Conversion, error) {
	ids, err := entryIDs(s.dir)
	if err != nil {
		return Conversion{}, err
	}
	var plan Conversion
	entries := map[string]*oldEntry{}
	var order []string
	for _, id := range ids {
		if !s.legacy(id) {
			continue
		}
		header, err := s.legacyHeader(id)
		if err == nil {
			var events []Event
			events, err = s.legacyEvents(id)
			entries[id] = &oldEntry{id: id, header: header, events: events, parent: header.Parent}
		}
		if err != nil {
			plan.Skipped = append(plan.Skipped, Skip{ID: id, Reason: err})
			continue
		}
		order = append(order, id)
	}
	spawned := func(entry *oldEntry) bool {
		parent, found := entries[entry.parent]
		return found && entry.header.ForkKind == "" && parent != entry
	}
	rootOf := func(id string) string {
		seen := map[string]bool{}
		for start := id; spawned(entries[id]); id = entries[id].parent {
			if seen[id] {
				return start
			}
			seen[id] = true
		}
		return id
	}
	for _, id := range order {
		entries[id].session = EventIDFor("session", rootOf(id))
		if rootOf(id) != id {
			entries[entries[id].parent].subAgents = append(entries[entries[id].parent].subAgents, id)
		}
	}
	for _, id := range order {
		entry := entries[id]
		if rootOf(id) != id {
			continue
		}
		fold := &folding{entries: entries, placed: map[string]bool{}}
		converted := Converted{Header: entry.header, Events: fold.entry(id, "", "", "", 0), From: fold.from}
		header := &converted.Header
		header.ID, header.CarriedFrom, header.Root = entry.session, nil, entry.session
		if parent, known := entries[entry.parent]; known && parent != entry {
			header.CarriedFrom = &Carried{Session: parent.session}
		}
		if into, known := entries[entry.header.ForkedInto]; known {
			header.ForkedInto = into.session
		}
		stamp(&converted)
		plan.Sessions = append(plan.Sessions, converted)
	}
	for i := range plan.Sessions {
		header := &plan.Sessions[i].Header
		if header.CarriedFrom == nil {
			continue
		}
		for _, parent := range plan.Sessions {
			if parent.Header.ID == header.CarriedFrom.Session {
				header.CarriedFrom.Event, header.Root = parent.Header.Head, cmp.Or(parent.Header.Root, parent.Header.ID)
			}
		}
	}
	if raw, err := s.readFile(filepath.Join(s.dir, headName)); err == nil {
		if entry, known := entries[strings.TrimSpace(string(raw))]; known {
			plan.Head = entry.session
		}
	}
	return plan, nil
}

type folding struct {
	entries map[string]*oldEntry
	placed  map[string]bool
	from    []string
}

func (f *folding) entry(id, agent, turn, spawnedBy string, depth int) []Event {
	entry := f.entries[id]
	f.placed[id] = true
	f.from = append(f.from, id)
	subAgents := slices.Clone(entry.subAgents)
	slices.SortStableFunc(subAgents, func(a, b string) int { return f.entries[a].header.At.Compare(f.entries[b].header.At) })
	named := map[string]bool{}
	for _, event := range entry.events {
		if subAgent := stringField(event.Body, legacySubAgentKey); event.Kind == EventToolResult && subAgent != "" {
			named[subAgent] = true
		}
	}
	var out []Event
	for _, event := range entry.events {
		if agent != "" {
			event.Agent, event.Turn, event.SpawnedBy = agent, turn, spawnedBy
		}
		if event.Kind == EventAttachment {
			var attached Attachment
			_ = json.Unmarshal(event.Body, &attached)
			attached.File = AttachmentPath(entry.session, filepath.Base(attached.File))
			event.Body = marshalled(attached)
		}
		subAgent := stringField(event.Body, legacySubAgentKey)
		if event.Kind == EventToolResult && named[subAgent] && !f.placed[subAgent] {
			for _, spawned := range group(subAgents, subAgent, named) {
				out = append(out, f.spawn(spawned, agent, event.Turn, event.Call, depth+1)...)
			}
		}
		out = append(out, event)
	}
	for _, subAgent := range subAgents {
		if !f.placed[subAgent] {
			out = append(out, f.spawn(subAgent, agent, cmp.Or(turn, lastTurn(out)), "", depth+1)...)
		}
	}
	return out
}

func group(subAgents []string, named string, called map[string]bool) []string {
	at := slices.Index(subAgents, named)
	grouped := []string{named}
	for _, next := range subAgents[at+1:] {
		if called[next] {
			break
		}
		grouped = append(grouped, next)
	}
	return grouped
}

func lastTurn(events []Event) string {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Turn != "" {
			return events[i].Turn
		}
	}
	return ""
}

func (f *folding) spawn(subAgent, orchestratorAgent, turn, call string, depth int) []Event {
	header := f.entries[subAgent].header
	spawned := Event{ID: EventIDFor(subAgent, "spawn"), At: header.At, Turn: turn, Agent: orchestratorAgent, Call: call, Kind: EventSpawn,
		Body: marshalled(SpawnBody{Agent: subAgent, Model: header.Model, Mission: firstLine(header.Task), Depth: depth})}
	events := append([]Event{spawned}, f.entry(subAgent, subAgent, turn, call, depth)...)
	usage, cost := spent(events, subAgent)
	ended := Event{ID: EventIDFor(subAgent, "agent_end"), At: header.LastAt(), Turn: turn, Agent: subAgent, Kind: EventAgentEnd,
		Body: marshalled(AgentEndBody{Status: header.Outcome, Usage: usage, CostUSD: cost})}
	return append(events, ended)
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

func spent(events []Event, agent string) (Usage, float64) {
	var usage Usage
	var cost float64
	for _, event := range events {
		if event.Kind != EventRequest || event.Agent != agent {
			continue
		}
		var step StepBody
		_ = json.Unmarshal(event.Body, &step)
		usage, cost = usage.Plus(step.usage()), cost+step.CostUSD
	}
	return usage, cost
}

func stamp(converted *Converted) {
	header := &converted.Header
	header.Schema, header.Turns, header.Agents, header.Models = SchemaVersion, 0, nil, nil
	header.Usage, header.CostUSD = Usage{}, 0
	last := map[string]string{}
	for i := range converted.Events {
		event := &converted.Events[i]
		event.Seq, event.Parent = i+1, last[event.Agent]
		last[event.Agent], header.Head = event.ID, event.ID
		switch event.Kind {
		case EventTurnStart:
			if event.Agent == "" {
				header.Turns++
			}
		case EventRequest:
			var step StepBody
			_ = json.Unmarshal(event.Body, &step)
			header.Usage, header.CostUSD = header.Usage.Plus(step.usage()), header.CostUSD+step.CostUSD
			if step.Model != "" && !slices.Contains(header.Models, step.Model) {
				header.Models = append(header.Models, step.Model)
			}
		case EventTurnEnd:
			if model := stringField(event.Body, "model"); model != "" && !slices.Contains(header.Models, model) {
				header.Models = append(header.Models, model)
			}
		case EventSpawn:
			var body SpawnBody
			_ = json.Unmarshal(event.Body, &body)
			header.Agents = append(header.Agents, AgentRun{Agent: body.Agent, Model: body.Model, ParentAgent: event.Agent, SpawnCall: event.Call,
				SpawnTurn: event.Turn, Depth: body.Depth, StartedAt: event.At})
		case EventAgentEnd:
			var body AgentEndBody
			_ = json.Unmarshal(event.Body, &body)
			for j := range header.Agents {
				if header.Agents[j].Agent == event.Agent {
					ended := event.At
					header.Agents[j].Status, header.Agents[j].EndedAt, header.Agents[j].Usage, header.Agents[j].CostUSD = body.Status, &ended, body.Usage, body.CostUSD
				}
			}
		}
	}
}

func (s *Store) Convert(plan Conversion) (int, error) {
	var failed []error
	var done []Converted
	for _, converted := range plan.Sessions {
		if err := s.writeConverted(converted); err != nil {
			failed = append(failed, fmt.Errorf("%s from %s: %w", converted.Header.ID, strings.Join(converted.From, ", "), err))
			continue
		}
		done = append(done, converted)
	}
	for _, converted := range done {
		for _, id := range converted.From {
			if err := s.removeLegacy(id); err != nil {
				failed = append(failed, fmt.Errorf("%s was converted into %s and stays where it was: %w", id, converted.Header.ID, err))
			}
		}
		if converted.Header.ID == plan.Head {
			failed = append(failed, s.SetHead(plan.Head))
		}
	}
	return len(done), errors.Join(failed...)
}

func (s *Store) writeConverted(converted Converted) error {
	target := s.Dir(converted.Header.ID)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("%s already exists and is not overwritten", target)
	}
	staging := filepath.Join(s.dir, convertingPrefix+converted.Header.ID)
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	var lines []byte
	for _, event := range converted.Events {
		lines = append(append(lines, marshalled(event)...), '\n')
	}
	body, err := json.MarshalIndent(converted.Header, "", "  ")
	if err == nil {
		err = errors.Join(appendLines(filepath.Join(staging, eventsName), lines), os.WriteFile(filepath.Join(staging, headerName), body, 0o644))
	}
	for _, id := range converted.From {
		if err == nil {
			err = s.copyAttachments(id, converted.Header.ID)
		}
	}
	if err == nil {
		err = s.rename(staging, target)
	}
	if err != nil {
		return errors.Join(err, os.RemoveAll(staging))
	}
	return nil
}

func (s *Store) copyAttachments(id, into string) error {
	entries, err := os.ReadDir(s.Dir(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == legacyHeaderName || name == legacyBodyName {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.Dir(id), name))
		if err == nil {
			err = os.MkdirAll(s.AttachmentDir(into), 0o755)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(s.AttachmentDir(into), name), raw, 0o644)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) removeLegacy(id string) error {
	if _, err := os.Stat(filepath.Join(s.dir, id+legacySuffix)); err == nil {
		return os.Remove(filepath.Join(s.dir, id+legacySuffix))
	}
	return os.RemoveAll(s.Dir(id))
}
