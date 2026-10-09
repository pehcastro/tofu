package quote

import (
	"encoding/json"
	"testing"

	"tofu/interface/tui/trace"
	isession "tofu/internal/session"
)

func talked() isession.Conversation {
	return isession.Conversation{
		Session: "turn-18d7474e7fae090c",
		Said: []isession.Utterance{
			{Event: "0f2c9b1a-1111-4aaa-8bbb-aaaaaade2233", Role: isession.RoleUser, Text: "  \n  read the policy before the wire\nthen say so"},
			{Event: "0f2c9b1a-2222-4aaa-8bbb-bbbbbbc41099", Role: isession.RoleAssistant, Calls: []isession.Call{{Name: "read"}, {Name: "glob"}}},
			{Event: "0f2c9b1a-3333-4aaa-8bbb-ccccccc41099", Role: isession.RoleAssistant, Text: "the gate reads library/policy/shell.yaml"},
			{Event: "0f2c9b1a-4444-4aaa-8bbb-dddddd9f0e11", Role: isession.RoleSystem, Text: "you are an agent"},
		},
	}
}

func TestCollectNamesTheSpeakerAndPutsTheNewestTurnFirst(t *testing.T) {
	turns := Collect(talked())
	if len(turns) != 3 {
		t.Fatalf("Collect kept %d turns, want the three that are not the system prompt: %#v", len(turns), turns)
	}
	if turns[0].Text != "the gate reads library/policy/shell.yaml" || turns[0].From != agentLabel {
		t.Errorf("the newest turn is %#v", turns[0])
	}
	if turns[2].Text != "read the policy before the wire" || turns[2].From != youLabel {
		t.Errorf("the oldest turn is %#v, want the first non-blank line of what you typed", turns[2])
	}
}

func TestATurnWithNoTextIsToldApartByTheToolsItRan(t *testing.T) {
	if got := Collect(talked())[1].Text; got != "ran read, glob" {
		t.Fatalf("a turn that only called tools reads as %q, want the tool names", got)
	}
}

func TestTheReferenceCarriesTheShortenedIDTheScreenAlreadyUses(t *testing.T) {
	turns := Collect(talked())
	ref := Ref(turns[0].Event)
	if want := "[quote" + trace.Short(turns[0].Event) + "]"; ref != want {
		t.Fatalf("Ref wrote %q, want %q", ref, want)
	}
	if ref != "[quote#c41099]" {
		t.Fatalf("Ref wrote %q, want [quote#c41099]", ref)
	}
}

func TestATurnRecordedWithNoIDIsGivenOneDerivedFromTheSessionAndItsPlace(t *testing.T) {
	talk := isession.Conversation{
		Session:   "turn-18d6ea32da3230c0",
		FromSteps: true,
		Said: []isession.Utterance{
			{Role: isession.RoleAssistant, Text: "first"},
			{Role: isession.RoleAssistant, Text: "second"},
		},
	}
	turns := Collect(talk)
	if turns[0].Event == turns[1].Event {
		t.Fatalf("two turns of a session that recorded no event id share %s", turns[0].Event)
	}
	if want := isession.EventIDFor(talk.Session, "step:1"); turns[0].Event != want {
		t.Fatalf("the newest turn was given %s, want the derived %s", turns[0].Event, want)
	}
}

func TestAnUtteranceWithNeitherTextNorACallIsNotOfferedToQuote(t *testing.T) {
	turns := Collect(isession.Conversation{Session: "turn-1", Said: []isession.Utterance{{Event: "a", Role: isession.RoleAssistant}}})
	if len(turns) != 0 {
		t.Fatalf("a turn with nothing in it was offered: %#v", turns)
	}
}

func TestTheStepShapeRecordedOnDiskReadsAsQuotableTurns(t *testing.T) {
	var step isession.StepBody
	raw := `{"index":1,"tool_calls":[{"tool":"glob","args":{"pattern":"*"},"result_bytes":6326832}]}`
	if err := json.Unmarshal([]byte(raw), &step); err != nil {
		t.Fatalf("the recorded step shape no longer parses: %v", err)
	}
	said := isession.Utterance{Role: isession.RoleAssistant, Text: step.AssistantText}
	for _, call := range step.ToolCalls {
		said.Calls = append(said.Calls, isession.Call{Name: call.Tool})
	}
	if got := gistOf(said); got != "ran glob" {
		t.Fatalf("a recorded step reads as %q", got)
	}
}
