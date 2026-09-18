package ledger

import (
	"encoding/json"
	"testing"
)

func TestSchemaBumpedForStateBuilder(t *testing.T) {
	if SchemaVersion < 3 {
		t.Fatalf("schema version = %d, want at least 3, state_builder was added at 3", SchemaVersion)
	}
}

func TestRowWrittenUnderOldSchemaStillReads(t *testing.T) {
	old := `{"id":"2026-09-01-aabb","schema":2,"at":"2026-09-01T00:00:00Z","point":"tool_gate","questions":"tool_gate","version":1,"build":"jev-1.13.0","model":"~typesafe/jev-latest","state_hash":"deadbeef","answers":[],"verdict":"ask","latency_ms":10,"cost":0.0001,"request_id":"or-req-1"}`

	var row Row
	if err := json.Unmarshal([]byte(old), &row); err != nil {
		t.Fatalf("an old-schema row must still decode: %v", err)
	}
	if row.Schema != 2 {
		t.Fatalf("schema = %d, want 2, the value the old row actually carried", row.Schema)
	}
	if row.StateBuilder != "" {
		t.Fatalf("state_builder = %q, want empty for a row written before the field existed", row.StateBuilder)
	}
	if row.StateHash != "deadbeef" {
		t.Fatalf("state_hash = %q, an unrelated field must still decode correctly", row.StateHash)
	}
}

func TestACurrentSchemaRowWithNoStateBuilderStillReads(t *testing.T) {
	current := `{"id":"2026-09-18-bbcc","schema":4,"at":"2026-09-18T00:00:00Z","point":"tool_gate","questions":"tool_gate","version":1,"build":"jev-1.13.0","model":"~typesafe/jev-latest","state_hash":"deadbeef","answers":[],"verdict":"ask","latency_ms":10,"cost":0.0001,"request_id":"or-req-2"}`

	var row Row
	if err := json.Unmarshal([]byte(current), &row); err != nil {
		t.Fatalf("a schema-4 row with no state_builder key must still decode: %v", err)
	}
	if row.Schema != SchemaVersion {
		t.Fatalf("schema = %d, want the current schema %d", row.Schema, SchemaVersion)
	}
	if row.StateBuilder != "" {
		t.Fatalf("state_builder = %q, want empty: this schema carries the field but nothing wrote it yet", row.StateBuilder)
	}
}

func TestSchemaBumpedForMode(t *testing.T) {
	if SchemaVersion != 4 {
		t.Fatalf("schema version = %d, want 4 after adding mode", SchemaVersion)
	}
}

func TestRowWrittenBeforeModeExistedReadsAsUnknownNotDefaulted(t *testing.T) {
	old := `{"id":"2026-09-01-aabb","schema":3,"at":"2026-09-01T00:00:00Z","point":"tool_gate","questions":"tool_gate","version":1,"build":"jev-1.13.0","model":"~typesafe/jev-latest","state_hash":"deadbeef","answers":[],"verdict":"ask","reason":{"question":"risk","comparison":"risk_ask_at","threshold":2.0,"value":1.5},"latency_ms":10,"cost":0.0001,"request_id":"or-req-1"}`

	var row Row
	if err := json.Unmarshal([]byte(old), &row); err != nil {
		t.Fatalf("a row written before mode existed must still decode: %v", err)
	}
	if row.Mode() != ModeUnknown {
		t.Fatalf("mode = %v, want unknown: a row from before the field existed is not a shadow row", row.Mode())
	}
	if row.Mode() == ModeShadow {
		t.Fatal("an absent mode must never read back as shadow")
	}
}

func TestWrittenRowCarriesItsStateBuilderThroughToJSON(t *testing.T) {
	dir := t.TempDir()
	before, err := NewWriter(dir).Append(Row{Point: "tool_gate", Questions: "tool_gate", Version: 1})
	if err != nil {
		t.Fatalf("Append before: %v", err)
	}
	after, err := NewWriter(dir).Append(Row{Point: "tool_gate", Questions: "tool_gate", Version: 1, StateBuilder: "tool_gate.9f3a21c4"})
	if err != nil {
		t.Fatalf("Append after: %v", err)
	}

	beforeJSON, err := json.Marshal(before)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}
	t.Logf("row before this ticket: %s", beforeJSON)
	t.Logf("row after this ticket:  %s", afterJSON)

	var beforeFields, afterFields map[string]any
	if err := json.Unmarshal(beforeJSON, &beforeFields); err != nil {
		t.Fatalf("unmarshal before: %v", err)
	}
	if err := json.Unmarshal(afterJSON, &afterFields); err != nil {
		t.Fatalf("unmarshal after: %v", err)
	}
	if _, present := beforeFields["state_builder"]; present {
		t.Fatalf("an empty state_builder must be omitted, not printed as \"\", got %s", beforeJSON)
	}
	if got := afterFields["state_builder"]; got != "tool_gate.9f3a21c4" {
		t.Fatalf("state_builder in the written row's JSON = %v, want tool_gate.9f3a21c4", got)
	}
}
