package turn

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/recall"
	"tofu/internal/session"
)

func TestAStepFromBeforeTheOccupancyReadsAsAnAbsenceAndAMeasuredZeroDoesNot(t *testing.T) {
	before := session.NewStore(filepath.Join("..", "session", "testdata"))
	events, err := before.Body("turn-18d6d295dfac466c-f2")
	if err != nil {
		t.Fatalf("read a session written before this ticket: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("the old session gave back no steps, so nothing was read")
	}
	if events[len(events)-1].Kind != session.EventOutcome {
		t.Fatalf("the last event of the old session is %q, want the outcome the single file recorded", events[len(events)-1].Kind)
	}
	steps := events[:len(events)-1]
	for i, event := range steps {
		if event.Kind != session.EventStep {
			t.Fatalf("event %d of the old session is %q, want a step", i, event.Kind)
		}
		var step StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatalf("step %d of the old session does not read as a step row: %v", i, err)
		}
		if step.Occupancy != nil {
			t.Fatalf("step %d of a session written before this ticket reads back occupancy %+v, and it was never measured",
				i, *step.Occupancy)
		}
	}

	unmeasured, err := json.Marshal(StepRow{Index: 2})
	if err != nil {
		t.Fatalf("marshal a step that measured nothing: %v", err)
	}
	if strings.Contains(string(unmeasured), "occupancy") {
		t.Fatalf("a step that measured nothing writes %s, and a later reader cannot tell that from a measurement", unmeasured)
	}

	written, err := json.Marshal(StepRow{Index: 1, Occupancy: &recall.Occupancy{Target: 50000}})
	if err != nil {
		t.Fatalf("marshal a step that measured every band at zero: %v", err)
	}
	var read StepRow
	if err := json.Unmarshal(written, &read); err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if read.Occupancy == nil {
		t.Fatal("a step that measured zero in every band reads back as a step that measured nothing")
	}
	if read.Occupancy.Total() != 0 || read.Occupancy.Target != 50000 {
		t.Fatalf("the measured zero reads back as %+v", *read.Occupancy)
	}
	t.Logf("%d old steps carry no occupancy; a measured zero carries %s", len(steps), written)
}

func TestACodexReasoningItemWritesAsItsOwnNamedFieldRatherThanTheSignature(t *testing.T) {
	signature := codex.EncodeReasoning("rs_1", "opaque")
	row := messageRowOf(llm.Message{Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "probe"}},
		Thinking:  llm.Thinking{Text: "thinking", Signature: signature}})
	if row.Reasoning == nil || row.Reasoning.ID != "rs_1" || row.Reasoning.EncryptedContent != "opaque" {
		t.Fatalf("the row reasoning item is %+v", row.Reasoning)
	}
	if row.ThinkingSignature != "" {
		t.Fatalf("a codex reasoning item also wrote the signature field: %q", row.ThinkingSignature)
	}

	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"reasoning":{"id":"rs_1","encrypted_content":"opaque"}`) {
		t.Fatalf("the row does not write a named reasoning field: %s", raw)
	}

	message, err := row.Message()
	if err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if message.Thinking.Signature != signature {
		t.Fatalf("the row read back a different signature than it stored: %q", message.Thinking.Signature)
	}
}

func TestAnAnthropicSignatureStillWritesToTheSignatureField(t *testing.T) {
	row := messageRowOf(llm.Message{Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "probe"}},
		Thinking:  llm.Thinking{Text: "thinking", Signature: "sig_recorded_turn_bytes"}})
	if row.Reasoning != nil {
		t.Fatalf("an anthropic signature was read as a codex reasoning item: %+v", row.Reasoning)
	}
	if row.ThinkingSignature != "sig_recorded_turn_bytes" {
		t.Fatalf("the anthropic signature is %q", row.ThinkingSignature)
	}
}

func TestAMessageRowWrittenBeforeTheReasoningFieldStillLoads(t *testing.T) {
	preRound2 := MessageRow{Role: "assistant", ToolCalls: []MessageToolCall{{ID: "call_1", Name: "probe"}},
		Thinking: "thinking", ThinkingSignature: codex.EncodeReasoning("rs_1", "opaque")}
	raw, err := json.Marshal(preRound2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var row MessageRow
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatalf("a session written before the reasoning field failed to read: %v", err)
	}
	message, err := row.Message()
	if err != nil {
		t.Fatalf("turning it into a message: %v", err)
	}
	id, encrypted, ok := codex.DecodeReasoning(message.Thinking.Signature)
	if !ok || id != "rs_1" || encrypted != "opaque" {
		t.Fatalf("the pre-field session no longer replays: id %q encrypted %q ok %v", id, encrypted, ok)
	}

	beforeThinkingExisted := `{"role":"assistant","content":"hello"}`
	if err := json.Unmarshal([]byte(beforeThinkingExisted), &row); err != nil {
		t.Fatalf("a session written before thinking existed at all failed to read: %v", err)
	}
	if _, err := row.Message(); err != nil {
		t.Fatalf("turning the oldest shape into a message: %v", err)
	}
}

func TestARecordedSessionCarriesTheSystemPromptAndToolsItSent(t *testing.T) {
	row := Row{ID: "turn-1", Task: "a task", Outcome: OutcomeStopped,
		System: "[tool_guidance, from tofu itself]\nprefer the tool over the shell", Tools: []string{"read", "bash"}}
	_, events, err := row.Record()
	if err != nil {
		t.Fatalf("record a row carrying a system prompt: %v", err)
	}
	prompt, ok, err := PromptFrom(events)
	if err != nil {
		t.Fatalf("read the prompt back: %v", err)
	}
	if !ok {
		t.Fatal("a session that carried a system prompt reads back as one that never did")
	}
	if prompt.System != row.System {
		t.Fatalf("the recorded system prompt is %q, want %q", prompt.System, row.System)
	}
	if len(prompt.Tools) != 2 || prompt.Tools[0] != "read" || prompt.Tools[1] != "bash" {
		t.Fatalf("the recorded tool list is %v, want [read bash]", prompt.Tools)
	}
	if events[0].Kind != session.EventPrompt {
		t.Fatalf("the first event is %q, want the prompt to lead the body the way session_init does upstream", events[0].Kind)
	}
}

func TestTwoSessionsWithDifferentPromptsRecordDifferently(t *testing.T) {
	first := Row{ID: "turn-1", Task: "fix the failing test", Outcome: OutcomeStopped, System: "prefer the tool over the shell"}
	second := Row{ID: "turn-2", Task: "fix the flaky test", Outcome: OutcomeStopped, System: "prefer the shell over the tool"}

	_, firstEvents, err := first.Record()
	if err != nil {
		t.Fatalf("record the first row: %v", err)
	}
	_, secondEvents, err := second.Record()
	if err != nil {
		t.Fatalf("record the second row: %v", err)
	}

	firstPrompt, _, err := PromptFrom(firstEvents)
	if err != nil {
		t.Fatalf("read the first prompt back: %v", err)
	}
	secondPrompt, _, err := PromptFrom(secondEvents)
	if err != nil {
		t.Fatalf("read the second prompt back: %v", err)
	}
	if firstPrompt.System == secondPrompt.System {
		t.Fatalf("two sessions with different prompts both recorded %q", firstPrompt.System)
	}
}

func TestASessionWrittenBeforeThePromptFieldStillLoads(t *testing.T) {
	store := session.NewStore(filepath.Join("..", "session", "testdata"))
	const id = "turn-18d6d295dfac466c-f2"

	header, err := store.Header(id)
	if err != nil {
		t.Fatalf("a session written before the prompt field is refused at the header: %v", err)
	}
	if header.ID == "" {
		t.Fatalf("the header of %s reads back empty", id)
	}
	events, err := store.Body(id)
	if err != nil {
		t.Fatalf("a session written before the prompt field is refused at the body: %v", err)
	}
	prompt, ok, err := PromptFrom(events)
	if err != nil {
		t.Fatalf("reading a session that never recorded a prompt failed: %v", err)
	}
	if ok {
		t.Fatalf("%s was written before the prompt field existed and should read as absent, not %+v", id, prompt)
	}
}

func TestTheRecordedPromptNeverCarriesTheAccountItWasSpentOn(t *testing.T) {
	row := Row{ID: "turn-1", Task: "a task", Outcome: OutcomeStopped, Account: 424242,
		System: "prefer the tool over the shell, and nothing about billing"}
	_, events, err := row.Record()
	if err != nil {
		t.Fatalf("record a row with an account and a prompt: %v", err)
	}
	prompt, ok, err := PromptFrom(events)
	if err != nil || !ok {
		t.Fatalf("read the prompt back: ok=%v err=%v", ok, err)
	}
	if prompt.System != row.System {
		t.Fatalf("the recorded prompt %q is not the byte-identical text that was composed, so something besides the caller's text reached the record", prompt.System)
	}
	if strings.Contains(prompt.System, "424242") {
		t.Fatalf("the recorded prompt %q carries the account number", prompt.System)
	}
}

func TestARowThatNamesNoRootIsItsOwnRoot(t *testing.T) {
	header, events, err := Row{ID: "turn-1", Task: "a task", Outcome: OutcomeStopped}.Record()
	if err != nil {
		t.Fatalf("record a row with no lineage: %v", err)
	}
	if header.Root != "turn-1" || header.Parent != "" {
		t.Fatalf("the header reads root %q parent %q", header.Root, header.Parent)
	}
	if len(events) != 1 || events[0].Kind != session.EventOutcome {
		t.Fatalf("a row with no steps produced %d events", len(events))
	}
}

func conversationByWalkingEvents(t *testing.T, events []session.Event) []llm.Message {
	t.Helper()
	var messages []llm.Message
	for _, event := range events {
		if event.Kind != session.EventMessage {
			continue
		}
		var row MessageRow
		if err := json.Unmarshal(event.Body, &row); err != nil {
			t.Fatalf("the walk could not read a message event: %v", err)
		}
		message, err := row.Message()
		if err != nil {
			t.Fatalf("the walk could not turn a message row into a message: %v", err)
		}
		messages = append(messages, message)
	}
	return Sendable(messages)
}

func TestTheConversationReadsTheSameThroughTheOneReaderAsThroughTheWalkItReplaces(t *testing.T) {
	row := Row{ID: "turn-1", Task: "a task", Outcome: OutcomeStopped,
		System: "prefer the tool over the shell", Tools: []string{"read", "bash"},
		Conversation: []llm.Message{
			{Role: llm.RoleUser, Content: "read a.txt"},
			{Role: llm.RoleAssistant, Content: "reading it",
				Thinking:  llm.Thinking{Text: "the file is small", Signature: codex.EncodeReasoning("rs_1", "opaque")},
				ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}}},
			{Role: llm.RoleTool, ToolCallID: "call_1", Content: "file contents",
				ToolOutcome: llm.ToolOutcomeRan, ToolResultBytes: 13},
			{Role: llm.RoleAssistant, Content: "it says file contents",
				Thinking: llm.Thinking{Text: "answer plainly", Signature: "sig_anthropic"}},
		}}
	_, events, err := row.Record()
	if err != nil {
		t.Fatalf("record a session: %v", err)
	}
	if events[0].Kind != session.EventPrompt {
		t.Fatalf("the first event is %q, want a prompt", events[0].Kind)
	}

	before := conversationByWalkingEvents(t, events)
	after, err := ConversationFrom(events)
	if err != nil {
		t.Fatalf("read the conversation back: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("the walk read %d messages and the reader read %d", len(before), len(after))
	}
	for i := range before {
		if !reflect.DeepEqual(before[i], after[i]) {
			t.Fatalf("message %d reads back differently:\nbefore %+v\nafter  %+v", i, before[i], after[i])
		}
	}
	t.Logf("%d messages read identically, the last two carrying the signatures %q and %q",
		len(after), after[1].Thinking.Signature, after[3].Thinking.Signature)
}
