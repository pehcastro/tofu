package host

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"sync"
)

type picture struct {
	Settings heldSettings
	Session  *Event        `json:",omitempty"`
	Turn     *Event        `json:",omitempty"`
	Partial  string        `json:",omitempty"`
	Agents   []SubAgentRow `json:",omitempty"`
	Plan     []PlanItem    `json:",omitempty"`
	Context  Context       `json:",omitzero"`
	Asks     []Event       `json:",omitempty"`
	Steers   []Event       `json:",omitempty"`
}

type bus struct {
	mu  sync.Mutex
	seq uint64
	now picture
}

func (p *picture) apply(event Event) {
	switch {
	case event.Kind == EventTurnStarted:
		p.Turn, p.Partial = &event, ""
	case event.Kind == EventTurnEnded:
		p.Turn, p.Partial = nil, ""
	case event.Kind == EventTextDelta && event.Agent == "":
		p.Partial += event.Text
	case endsTheLeadReply(event):
		p.Partial = ""
	}
	switch event.Kind {
	case EventSession:
		if event.ID != "" {
			p.Session = &event
		}
	case EventSubAgent, EventDone:
		if event.SubAgents != nil {
			p.Agents = event.SubAgents
		}
	case EventPlan:
		if event.Agent == "" {
			p.Plan = event.Plan
		}
	case EventContext:
		if event.Agent == "" {
			p.Context = event.Context
		}
	case EventAwaitPerson:
		p.Asks = append(p.Asks, event)
	case EventResumed:
		p.Asks = slices.DeleteFunc(p.Asks, withID(event.ID))
	case EventSteerQueued:
		p.Steers = append(p.Steers, event)
	case EventSteered:
		p.Steers = slices.DeleteFunc(p.Steers, withID(event.ID))
	}
}

func withID(id string) func(Event) bool { return func(event Event) bool { return event.ID == id } }

func (seen picture) toward(now picture) []Event {
	var moves []Event
	for _, asked := range seen.Asks {
		if !slices.ContainsFunc(now.Asks, withID(asked.ID)) {
			moves = append(moves, Event{Kind: EventResumed, ID: asked.ID})
		}
	}
	if now.Session != nil && (seen.Session == nil || seen.Session.ID != now.Session.ID) {
		moves = append(moves, *now.Session)
	}
	same := seen.Turn != nil && now.Turn != nil && seen.Turn.ID == now.Turn.ID
	shown := seen.Partial
	if !same {
		shown = ""
		if seen.Turn != nil {
			moves = append(moves, Event{Kind: EventTurnEnded})
		}
		if now.Turn != nil {
			moves = append(moves, *now.Turn)
		}
	}
	rest, grew := strings.CutPrefix(now.Partial, shown)
	if !grew {
		moves, rest = append(moves, Event{Kind: EventStreamReset}), now.Partial
	}
	if rest != "" {
		moves = append(moves, Event{Kind: EventTextDelta, Text: rest})
	}
	if now.Agents != nil && !reflect.DeepEqual(seen.Agents, now.Agents) {
		moves = append(moves, Event{Kind: EventSubAgent, SubAgents: now.Agents})
	}
	if now.Plan != nil && !slices.Equal(seen.Plan, now.Plan) {
		moves = append(moves, Event{Kind: EventPlan, Plan: now.Plan})
	}
	if now.Context != seen.Context {
		moves = append(moves, Event{Kind: EventContext, Context: now.Context})
	}
	for _, asked := range now.Asks {
		if !slices.ContainsFunc(seen.Asks, withID(asked.ID)) {
			moves = append(moves, asked)
		}
	}
	for _, steer := range now.Steers {
		if !slices.ContainsFunc(seen.Steers, withID(steer.ID)) {
			moves = append(moves, steer)
		}
	}
	return moves
}

func (h *Host) Events() <-chan Event { return h.events }

func (h *Host) deliver(event Event) {
	h.bus.mu.Lock()
	h.bus.seq++
	h.bus.now.apply(event)
	h.fanOut(followLine{Seq: h.bus.seq, Event: &event})
	h.bus.mu.Unlock()
	h.hand(event, h.done)
}

func (h *Host) fanOut(said followLine) {
	if a := h.audience.Load(); a != nil {
		a.broadcast(said, h.snapshot)
	}
}

func (h *Host) snapshot() []byte {
	h.steering.mu.Lock()
	queued := h.steering.queued()
	h.steering.mu.Unlock()
	h.bus.now.Steers = slices.DeleteFunc(h.bus.now.Steers, func(steer Event) bool { return !slices.Contains(queued, steer.ID) })
	line, _ := json.Marshal(followLine{Seq: h.bus.seq, Snapshot: &h.bus.now})
	return line
}

func (h *Host) hand(event Event, quit <-chan struct{}) {
	select {
	case h.events <- event:
		return
	default:
	}
	if event.snapshot() {
		return
	}
	select {
	case h.events <- event:
	case <-quit:
	}
}

func (h *Host) seen() picture {
	h.bus.mu.Lock()
	defer h.bus.mu.Unlock()
	return h.bus.now
}

func (h *Host) forget() {
	h.bus.mu.Lock()
	defer h.bus.mu.Unlock()
	h.bus.now = picture{Settings: h.bus.now.Settings}
}
