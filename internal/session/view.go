package session

import (
	"cmp"
	"encoding/json"
)

func (s Settings) view(events []Event) ([]Event, error) {
	calls := map[string]Event{}
	asked := map[string][]string{}
	madeBy := map[string][]string{}
	latestAssistant := map[string]string{}
	results := map[string]Event{}
	said := map[string]string{}
	compacted := map[string][]Event{}
	for _, event := range events {
		switch event.Kind {
		case EventToolCall:
			calls[event.Call] = event
			asked[event.Request] = append(asked[event.Request], event.Call)
			if maker, found := latestAssistant[event.Request]; found {
				madeBy[maker] = append(madeBy[maker], event.Call)
			}
		case EventToolResult:
			results[event.Call] = event
		case EventCompaction:
			compacted[event.Request] = append(compacted[event.Request], event)
		case EventMessage:
			var message MessageBody
			if json.Unmarshal(event.Body, &message) == nil && message.Role == RoleAssistant && event.Request != "" {
				said[event.Request], latestAssistant[event.Request] = message.Content, event.ID
			}
		}
	}
	var shown []Event
	for _, event := range events {
		author := cmp.Or(event.Author, event.Agent, AuthorOrchestrator)
		switch event.Kind {
		case EventToolCall, EventCompaction, EventTurnStart, EventSpawn, EventAgentEnd:
		case EventMessage:
			if made := madeBy[event.ID]; event.Request != "" && len(made) > 0 {
				message := objectOf(event.Body)
				var folded []MessageToolCall
				for _, call := range made {
					var body CallBody
					_ = json.Unmarshal(calls[call].Body, &body)
					folded = append(folded, MessageToolCall{ID: call, Name: body.Tool, Arguments: body.Args})
				}
				message["tool_calls"] = marshalled(folded)
				event.Body = marshalled(message)
			}
			shown = append(shown, Event{ID: event.ID, Author: author, Attempt: FirstAttempt, Kind: EventMessage, Body: event.Body})
		case EventToolResult:
			var result ResultBody
			_ = json.Unmarshal(event.Body, &result)
			message := MessageBody{Role: RoleTool, ToolCallID: event.Call, Content: result.Content, ToolOutcome: result.ToolOutcome,
				ToolResultBytes: cmp.Or(result.ResultBytes, len(result.Content))}
			shown = append(shown, Event{ID: event.ID, Author: author, Attempt: FirstAttempt, Kind: EventMessage, Body: marshalled(message)})
		case EventRequest:
			request := cmp.Or(event.Request, event.ID)
			step := objectOf(event.Body)
			var rows []json.RawMessage
			for _, call := range asked[request] {
				row := objectOf(results[call].Body)
				delete(row, "content")
				delete(row, "tool_outcome")
				var body CallBody
				_ = json.Unmarshal(calls[call].Body, &body)
				row["tool"], row["id"], row["call"] = marshalled(body.Tool), marshalled(calls[call].ID), marshalled(call)
				if _, rewritten := row["args"]; !rewritten && len(body.Args) > 0 {
					row["args"] = body.Args
				}
				rows = append(rows, marshalled(row))
			}
			if len(rows) > 0 {
				step["tool_calls"] = marshalled(rows)
			}
			if _, has := step["assistant_text"]; !has && said[request] != "" {
				step["assistant_text"] = marshalled(said[request])
			}
			for _, compaction := range compacted[request] {
				for key, value := range objectOf(compaction.Body) {
					step[key] = value
				}
			}
			shown = append(shown, Event{ID: event.ID, Author: author, Attempt: max(event.Attempt, FirstAttempt), Kind: EventStep, Body: marshalled(step)})
			reads, err := s.readsOf(event.ID, author, marshalled(step))
			if err != nil {
				return nil, err
			}
			shown = append(shown, reads...)
		case EventTurnEnd:
			shown = append(shown, Event{ID: event.ID, Author: author, Attempt: FirstAttempt, Kind: EventOutcome, Body: event.Body})
		default:
			event.Author = author
			shown = append(shown, event)
		}
	}
	return shown, nil
}
