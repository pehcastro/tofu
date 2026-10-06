package host

import (
	"maps"
	"os"
	"slices"
	"strings"
	"sync"

	"tofu/internal/sys"
)

type emitter struct {
	h       *Host
	mask    func(string) string
	secrets []string
	mu      sync.Mutex
	lead    stream
}

type stream struct {
	raw  string
	sent string
}

func (h *Host) emitter() *emitter {
	stored, _ := sys.StoredKeys()
	secrets := slices.Collect(maps.Values(stored))
	for _, name := range sys.KeyNames() {
		secrets = append(secrets, strings.TrimSpace(os.Getenv(name)))
	}
	return &emitter{h: h, mask: sys.LoadKeyRedactor().Redact, secrets: slices.DeleteFunc(secrets, func(secret string) bool { return secret == "" })}
}

func (e *emitter) emit(event Event) {
	if event.Kind == EventSteered {
		e.h.markRead(event.Text)
	}
	if event.Kind == EventTextDelta {
		e.mu.Lock()
		event.Text = e.lead.take(e.mask, e.secrets, event.Text)
		e.mu.Unlock()
		if event.Text != "" {
			e.send(event)
		}
		return
	}
	if endsTheLeadReply(event) {
		e.mu.Lock()
		tail := ""
		if event.Kind != EventStreamReset {
			tail = e.lead.upTo(e.mask, len(e.lead.raw))
		}
		e.lead = stream{}
		e.mu.Unlock()
		if tail != "" {
			e.send(Event{Kind: EventTextDelta, Text: tail})
		}
	}
	e.send(redacted(event, e.mask))
}

func (e *emitter) send(event Event) {
	e.h.heard(event)
	e.h.deliver(event)
}

func endsTheLeadReply(event Event) bool {
	switch event.Kind {
	case EventTextDelta, EventThinking, EventRequesting:
		return false
	}
	return event.Agent == "" && !event.snapshot()
}

func (s *stream) take(mask func(string) string, secrets []string, delta string) string {
	s.raw += delta
	held := 0
	for _, secret := range secrets {
		for size := min(len(secret)-1, len(s.raw)); size > held; size-- {
			if strings.HasSuffix(s.raw, secret[:size]) {
				held = size
				break
			}
		}
	}
	return s.upTo(mask, len(s.raw)-held)
}

func (s *stream) upTo(mask func(string) string, end int) string {
	masked := mask(s.raw[:end])
	fresh, extends := strings.CutPrefix(masked, s.sent)
	s.sent = masked
	if !extends {
		return ""
	}
	return fresh
}

func (h *Host) heard(event Event) {
	if event.Agent != "" || event.snapshot() {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch event.Kind {
	case EventTextDelta:
		if !h.streaming {
			h.previous, h.answer, h.streaming = h.answer, "", true
		}
		h.answer += event.Text
	case EventStreamReset:
		if h.streaming {
			h.answer, h.streaming = h.previous, false
		}
	case EventText:
		h.answer, h.streaming = event.Text, false
	case EventThinking, EventRequesting, EventStats:
	default:
		h.streaming = false
	}
}

func redacted(event Event, mask func(string) string) Event {
	event.Text, event.Detail, event.Diff, event.Created = mask(event.Text), mask(event.Detail), mask(event.Diff), mask(event.Created)
	if event.Decision != nil {
		decided := *event.Decision
		decided.Failure, decided.OverridesRule = mask(decided.Failure), mask(decided.OverridesRule)
		event.Decision = &decided
	}
	event.Plan = slices.Clone(event.Plan)
	for i := range event.Plan {
		event.Plan[i].Phase, event.Plan[i].Text = mask(event.Plan[i].Phase), mask(event.Plan[i].Text)
	}
	event.SubAgents = slices.Clone(event.SubAgents)
	for i := range event.SubAgents {
		row := &event.SubAgents[i]
		row.Doing, row.Report = mask(row.Doing), mask(row.Report)
		row.Owns, row.Calls = slices.Clone(row.Owns), slices.Clone(row.Calls)
		for j := range row.Owns {
			row.Owns[j] = mask(row.Owns[j])
		}
		for j := range row.Calls {
			row.Calls[j].Text, row.Calls[j].Result = mask(row.Calls[j].Text), mask(row.Calls[j].Result)
		}
	}
	return event
}
