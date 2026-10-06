package turn

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/recall"
	"tofu/internal/session"
)

func threeReads(prefix string) llm.Decision {
	var calls []llm.ToolCall
	for _, path := range []string{"a.txt", "b.txt", "c.txt"} {
		calls = append(calls, llm.ToolCall{ID: prefix + "-" + path, Name: "read", Arguments: json.RawMessage(`{"path":"` + path + `"}`)})
	}
	return toolCallDecision(calls...)
}

func TestAContinuationForkCarryingParallelCallsReadsBackAsAListAnthropicAccepts(t *testing.T) {
	bulky := threeReads("first")
	bulky.Content = strings.Repeat("a long account of the plan so far. ", 250)
	model := newCrew(map[string][]llm.Decision{
		leadKey: {bulky, threeReads("second"), threeReads("third"), claimDecision("read them all")},
	})
	store, first := session.NewStore(t.TempDir()), session.NewEventID()
	lead := crewLead(t, model)
	lead.Sessions, lead.Session, lead.Budget = store, first, recall.Budget{Bands: recall.Bands{Recent: 6000}}
	turns := startLead(context.Background(), lead, nil).wait(t)
	events, err := store.Events(first)
	if err != nil {
		t.Fatal(err)
	}
	tail := 0
	for _, event := range events {
		var forked struct {
			Fork *Fork `json:"fork"`
		}
		if event.Kind == session.EventCompaction && json.Unmarshal(event.Body, &forked) == nil && forked.Fork != nil {
			tail = forked.Fork.TailMessages
		}
	}
	if len(turns) != 1 || turns[0].Session == first || tail == 0 {
		t.Fatalf("the turn ended in %s with a fork tail of %d messages, want a continuation fork that carried calls", turns[0].Session, tail)
	}
	for i, request := range model.requests(leadKey) {
		sent := slices.DeleteFunc(slices.Clone(request.Messages), func(message llm.Message) bool { return message.Role == llm.RoleSystem })
		if _, err := (anthropic.Request{Model: "claude-opus-5", Messages: sent}).Encode(true); err != nil {
			t.Errorf("live request %d: %v", i+1, err)
		}
	}
	forkedEvents, err := store.Events(turns[0].Session)
	if err != nil {
		t.Fatal(err)
	}
	spoke := map[string]int{}
	for _, event := range forkedEvents {
		var message session.MessageBody
		if event.Kind == session.EventMessage && json.Unmarshal(event.Body, &message) == nil && message.Role == session.RoleAssistant {
			if spoke[event.Request]++; spoke[event.Request] > 1 {
				t.Errorf("the forked session files %d assistant messages under request %s, and a request makes one", spoke[event.Request], event.Request)
			}
		}
	}
	body, err := store.Body(turns[0].Session)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := ConversationFrom(body)
	if err != nil {
		t.Fatal(err)
	}
	resumed = append(resumed, llm.Message{Role: llm.RoleUser, Content: "go on"})
	if _, err := (anthropic.Request{Model: "claude-opus-5", Messages: resumed}).Encode(true); err != nil {
		t.Errorf("the forked session read back as --continue reads it: %v", err)
	}
}
