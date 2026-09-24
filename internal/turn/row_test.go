package turn

import (
	"encoding/json"
	"path/filepath"
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
