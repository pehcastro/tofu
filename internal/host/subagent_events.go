package host

import (
	"encoding/json"
	"slices"

	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

func (h *Host) SubAgentEvents(names []string) ([]Event, error) {
	id := h.ID()
	if id == "" || len(names) == 0 {
		return nil, nil
	}
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return nil, err
	}
	header, err := store.Header(id)
	if err != nil {
		return nil, err
	}
	lineage, err := store.Ancestors(id)
	if err != nil {
		return nil, err
	}
	slices.Reverse(lineage)
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	var read []Event
	var logged *session.Event
	mask := sys.LoadKeyRedactor().Redact
	watch := &watcher{seen: map[string]bool{}, spawner: &turn.SpawnTool{}, emit: func(event Event) {
		event.Logged = logged
		read = append(read, redacted(event, mask))
	}}
	for _, from := range append(lineage, header) {
		events, err := store.Events(from.ID)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			if !wanted[event.Agent] {
				continue
			}
			bare := event
			bare.Body, logged = nil, &bare
			switch event.Kind {
			case session.EventMessage:
				var said session.MessageBody
				if json.Unmarshal(event.Body, &said) == nil && said.Role == session.RoleAssistant && said.Thinking != "" {
					watch.emit(Event{Kind: EventThinking, ID: session.EventIDFor(event.Agent, "thinking "+event.ID), Agent: event.Agent, Text: said.Thinking})
				}
			case session.EventToolCall:
				watch.called(callOf(event), event.Agent)
			case session.EventToolResult:
				watch.result(resultOf(event), event.Agent)
			}
		}
	}
	return read, nil
}
