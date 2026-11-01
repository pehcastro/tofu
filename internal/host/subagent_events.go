package host

import (
	"cmp"
	"encoding/json"
	"slices"

	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

func (h *Host) SubAgentEvents(names []string, visit func(Event) bool) error {
	id := h.ID()
	if id == "" || len(names) == 0 {
		return nil
	}
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return err
	}
	header, err := store.Header(id)
	if err != nil {
		return err
	}
	lineage, err := store.Ancestors(id)
	if err != nil {
		return err
	}
	slices.Reverse(lineage)
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	stopped := false
	var failed error
	var logged *session.Event
	mask := sys.LoadKeyRedactor().Redact
	watch := &watcher{seen: &shownResults{ids: map[string]bool{}}, spawner: &turn.SpawnTool{}, emit: func(event Event) {
		event.Logged = logged
		stopped = stopped || !visit(redacted(event, mask))
	}}
	for _, from := range append(lineage, header) {
		err := store.EachEvent(from.ID, func(agent string) bool { return wanted[agent] }, func(event session.Event) bool {
			bare := event
			bare.Body, logged, watch.turnID = nil, &bare, event.Turn
			switch event.Kind {
			case session.EventMessage:
				var said session.MessageBody
				if json.Unmarshal(event.Body, &said) == nil && said.Role == session.RoleAssistant && said.Thinking != "" {
					watch.emit(Event{Kind: EventThinking, ID: session.EventIDFor(event.Agent, "thinking "+event.ID), Agent: event.Agent, Text: said.Thinking})
				}
			case session.EventToolCall:
				delete(watch.cards.open, event.Agent)
				watch.called(callOf(event), event.Agent)
			case session.EventToolResult:
				ran, unknown := resultOf(event)
				if unknown != nil {
					failed = unknown
					return false
				}
				watch.result(ran, event.Agent)
			}
			return !stopped
		})
		if err != nil || failed != nil || stopped {
			return cmp.Or(err, failed)
		}
	}
	return nil
}
