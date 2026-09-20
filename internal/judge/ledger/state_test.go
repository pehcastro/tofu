package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaBumpedForTheStateBody(t *testing.T) {
	if SchemaVersion != StateBodySchema {
		t.Fatalf("schema version = %d, want %d after adding the state body", SchemaVersion, StateBodySchema)
	}
	if StateBodySchema != 6 {
		t.Fatalf("the state body arrived at schema %d, want 6", StateBodySchema)
	}
}

func TestASmallStateIsCarriedWholeOnTheRow(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{"agent":"tofu","input":{"command":"git push --force"},"tool":"bash"}`)

	stored, err := NewWriter(dir).Append(Row{Point: "tool_gate", Questions: "tool_gate", Version: 1, State: body})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if stored.StateElision != nil {
		t.Fatalf("a %d byte state was elided, the ceiling is %d", len(body), stateInlineCeiling)
	}

	read, ok, err := NewReader(dir).ByID(stored.ID)
	if err != nil || !ok {
		t.Fatalf("ByID(%s) = %v, %v", stored.ID, ok, err)
	}
	if string(read.State) != string(body) {
		t.Fatalf("state read back = %s, want %s", read.State, body)
	}
}

func TestAStateOverTheCeilingIsElidedAndTheRowStaysValid(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{"command":"` + strings.Repeat("x", 8*1024) + `","tool":"bash"}`)

	stored, err := NewWriter(dir).Append(Row{Point: "tool_gate", Questions: "tool_gate", Version: 1,
		StateHash: HashOf(body), State: body})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if stored.StateElision == nil {
		t.Fatalf("a %d byte state was written whole, nothing over %d may sit on a row", len(body), stateInlineCeiling)
	}
	if len(stored.State) != 0 {
		t.Fatalf("an elided row still carries %d bytes of state inline", len(stored.State))
	}
	if stored.StateElision.Bytes != len(body) {
		t.Fatalf("elision bytes = %d, want %d", stored.StateElision.Bytes, len(body))
	}
	if !strings.HasPrefix(string(body), stored.StateElision.Head) || !strings.HasSuffix(string(body), stored.StateElision.Tail) {
		t.Fatalf("the head and tail on the row are not the head and tail of the state: %q, %q", stored.StateElision.Head, stored.StateElision.Tail)
	}

	read, ok, err := NewReader(dir).ByID(stored.ID)
	if err != nil || !ok {
		t.Fatalf("ByID(%s) = %v, %v", stored.ID, ok, err)
	}
	if read.StateElision == nil || read.StateElision.Bytes != len(body) {
		t.Fatalf("the elision did not survive the round trip: %#v", read.StateElision)
	}
	if read.StateHash != HashOf(body) || read.Point != "tool_gate" || read.Version != 1 {
		t.Fatalf("an elided row lost a field: hash %q, point %q, version %d", read.StateHash, read.Point, read.Version)
	}

	kept, err := os.ReadFile(filepath.Join(dir, read.StateElision.File))
	if err != nil {
		t.Fatalf("the elided body is not at %s: %v", read.StateElision.File, err)
	}
	if string(kept) != string(body) {
		t.Fatalf("the elided body is %d bytes, want the whole %d", len(kept), len(body))
	}
	var tree map[string]any
	if err := json.Unmarshal(kept, &tree); err != nil {
		t.Fatalf("the elided body is not valid JSON: %v", err)
	}
}

func TestARowFromTheSchemaBeforeTheStateBodyStillReads(t *testing.T) {
	old := `{"id":"2026-09-19-b51afec433d77bfbf5390199529615b5","schema":5,"at":"2026-09-19T00:07:22.063Z","point":"tool_gate","questions":"tool_gate","version":1,"build":"typesafe/jev-1.13-20260917","model":"~typesafe/jev-latest","state_hash":"f732f4b88ab2eb02b162e8cb5ac8355c989c562409955a283262cf1755681907","state_builder":"tool_gate.5e17bb5c","answers":[{"kind":"noul","noul":0.09,"question":"approval","wording":1}],"verdict":"allow","policy":"tool_gate","policy_version":1,"reason":{"question":"risk","comparison":"risk_ask_at","threshold":1.5,"value":0,"mode":"shadow"},"latency_ms":655,"cost":3.7884e-05,"request_id":"gen-dec-1789776349-YuhkuIfcgjR7EE9jKT91"}`

	var row Row
	if err := json.Unmarshal([]byte(old), &row); err != nil {
		t.Fatalf("a schema-5 row must still decode: %v", err)
	}
	if len(row.State) != 0 || row.StateElision != nil {
		t.Fatalf("a row written before the state body carries state %s and elision %#v", row.State, row.StateElision)
	}
	if row.Schema != 5 {
		t.Fatalf("schema = %d, want 5, the value the row actually carried", row.Schema)
	}
	if row.ID != "2026-09-19-b51afec433d77bfbf5390199529615b5" || row.Point != "tool_gate" || row.Version != 1 {
		t.Fatalf("identity fields changed: %q, %q, %d", row.ID, row.Point, row.Version)
	}
	if row.StateHash != "f732f4b88ab2eb02b162e8cb5ac8355c989c562409955a283262cf1755681907" || row.StateBuilder != "tool_gate.5e17bb5c" {
		t.Fatalf("state_hash = %q, state_builder = %q", row.StateHash, row.StateBuilder)
	}
	if row.Verdict != VerdictAllow || row.Mode() != ModeShadow || row.Reason.Threshold != 1.5 {
		t.Fatalf("verdict %q, mode %q, threshold %v", row.Verdict, row.Mode(), row.Reason.Threshold)
	}
	if len(row.Answers) != 1 || row.Answers[0].Kind != AnswerNoul || row.Answers[0].Noul != 0.09 {
		t.Fatalf("answers = %#v", row.Answers)
	}
	if row.LatencyMS != 655 || row.Cost != 3.7884e-05 || row.RequestID != "gen-dec-1789776349-YuhkuIfcgjR7EE9jKT91" {
		t.Fatalf("latency %d, cost %v, request id %q", row.LatencyMS, row.Cost, row.RequestID)
	}
}
