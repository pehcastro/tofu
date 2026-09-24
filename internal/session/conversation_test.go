package session

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/sys"
)

func messageSaying(t *testing.T, role, content string, calls ...string) Event {
	t.Helper()
	body := MessageBody{Role: role, Content: content}
	for _, name := range calls {
		body.ToolCalls = append(body.ToolCalls, MessageToolCall{ID: name + "-1", Name: name, Arguments: json.RawMessage(`{"command":"ls"}`)})
	}
	return Event{ID: NewEventID(), Kind: EventMessage, Body: marshal(t, body)}
}

func stepSaying(t *testing.T, index int, text string, tools ...string) Event {
	t.Helper()
	body := StepBody{Index: index, AssistantText: text}
	for _, tool := range tools {
		body.ToolCalls = append(body.ToolCalls, StepToolCall{Tool: tool, Args: json.RawMessage(`{"command":"ls"}`)})
	}
	return Event{ID: NewEventID(), Kind: EventStep, Body: marshal(t, body)}
}

func marshal(t *testing.T, body any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func written(t *testing.T, events ...Event) *Store {
	t.Helper()
	store := NewStore(t.TempDir())
	if err := store.Write(Header{ID: "turn-1", Root: "turn-1", At: time.Now()}, events); err != nil {
		t.Fatalf("write: %v", err)
	}
	return store
}

func TestASessionCarryingBothAMessageAndAStepReadsTheToolCallOnce(t *testing.T) {
	store := written(t,
		messageSaying(t, RoleAssistant, "running it", "bash"),
		stepSaying(t, 1, "running it", "bash"),
	)
	talk, err := store.Conversation("turn-1")
	if err != nil {
		t.Fatalf("conversation: %v", err)
	}
	if talk.FromSteps {
		t.Error("a session with a message event read its steps as well")
	}
	calls := 0
	for _, said := range talk.Said {
		calls += len(said.Calls)
	}
	if len(talk.Said) != 1 || calls != 1 {
		t.Fatalf("read %d utterances and %d calls, want 1 and 1: %+v", len(talk.Said), calls, talk.Said)
	}
	if talk.Said[0].Calls[0].ID != "bash-1" {
		t.Errorf("the call read as %+v, and without its id a tool result cannot be paired with it", talk.Said[0].Calls[0])
	}
}

func TestASessionCarryingOnlyStepsReadsThemAsTheConversation(t *testing.T) {
	store := written(t, stepSaying(t, 1, "first", "bash"), stepSaying(t, 2, "second", "write", "read"))
	talk, err := store.Conversation("turn-1")
	if err != nil {
		t.Fatalf("conversation: %v", err)
	}
	if !talk.FromSteps || len(talk.Said) != 2 {
		t.Fatalf("read %d utterances, from steps %v: %+v", len(talk.Said), talk.FromSteps, talk.Said)
	}
	if talk.Said[1].Text != "second" || len(talk.Said[1].Calls) != 2 || talk.Said[1].Calls[1].Name != "read" {
		t.Fatalf("the second step read as %+v", talk.Said[1])
	}
}

func TestASessionThatRecordedOnlyAnOutcomeReadsAnEmptyConversation(t *testing.T) {
	store := written(t, Event{ID: NewEventID(), Kind: EventOutcome, Body: marshal(t, map[string]string{"outcome": "stopped"})})
	talk, err := store.Conversation("turn-1")
	if err != nil {
		t.Fatalf("conversation: %v", err)
	}
	if talk.FromSteps || talk.Said != nil {
		t.Fatalf("a session with no message and no step read as %+v", talk)
	}
}

type handDecodedStep struct {
	AssistantText string `json:"assistant_text"`
	ToolCalls     []struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	} `json:"tool_calls"`
}

func TestEveryRecordedSessionWithNoMessageEventReadsWhatAHandDecodeOfItsStepsReads(t *testing.T) {
	store := OpenAt(sys.StateDir("../.."))
	listing, err := store.Listing()
	if err != nil {
		t.Skipf("this checkout has no session store to read: %v", err)
	}
	if len(listing.Sessions) == 0 {
		t.Skip("this checkout carries no recorded session")
	}
	stepsOnly := 0
	for _, header := range listing.Sessions {
		events, err := store.Body(header.ID)
		if err != nil {
			continue
		}
		var want []Utterance
		messages := false
		for _, event := range events {
			if event.Kind == EventMessage {
				messages = true
			}
		}
		if messages {
			continue
		}
		for _, event := range events {
			if event.Kind != EventStep {
				continue
			}
			var step handDecodedStep
			if err := json.Unmarshal(event.Body, &step); err != nil {
				t.Fatalf("%s: a step does not hand decode: %v", header.ID, err)
			}
			said := Utterance{Event: event.ID, Role: RoleAssistant, Text: step.AssistantText}
			for _, call := range step.ToolCalls {
				said.Calls = append(said.Calls, Call{Name: call.Tool, Args: call.Args})
			}
			want = append(want, said)
		}
		if len(want) == 0 {
			continue
		}
		stepsOnly++
		talk, err := store.Conversation(header.ID)
		if err != nil {
			t.Fatalf("%s: conversation: %v", header.ID, err)
		}
		if !talk.FromSteps {
			t.Errorf("%s carries no message event and did not read its steps", header.ID)
		}
		if len(talk.Said) != len(want) {
			t.Fatalf("%s read %d utterances, a hand decode read %d", header.ID, len(talk.Said), len(want))
		}
		for at := range want {
			if talk.Said[at].Text != want[at].Text || len(talk.Said[at].Calls) != len(want[at].Calls) {
				t.Fatalf("%s utterance %d read as %+v, a hand decode read %+v", header.ID, at, talk.Said[at], want[at])
			}
			for call := range want[at].Calls {
				if talk.Said[at].Calls[call].Name != want[at].Calls[call].Name || string(talk.Said[at].Calls[call].Args) != string(want[at].Calls[call].Args) {
					t.Fatalf("%s call %d of utterance %d read as %+v", header.ID, call, at, talk.Said[at].Calls[call])
				}
			}
		}
	}
	if stepsOnly == 0 {
		t.Skipf("none of the %d recorded sessions carries steps and no message", len(listing.Sessions))
	}
	t.Logf("%d of %d recorded sessions carry steps and no message event", stepsOnly, len(listing.Sessions))
}

func roleWord(role llm.Role) (word string, named bool) {
	defer func() {
		if recover() != nil {
			word, named = "", false
		}
	}()
	return role.String(), true
}

func TestTheRoleWordsOnDiskAreTheWordsTheModelPackageWrites(t *testing.T) {
	var sent []string
	for role := llm.RoleUnknown + 1; ; role++ {
		word, named := roleWord(role)
		if !named {
			break
		}
		sent = append(sent, word)
	}
	read := []string{RoleSystem, RoleUser, RoleAssistant, RoleTool}
	if !slices.Equal(sent, read) {
		t.Fatalf("llm writes the roles %v, session reads back %v", sent, read)
	}
}
