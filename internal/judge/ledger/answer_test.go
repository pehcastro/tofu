package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLegacyLine(t *testing.T, dir, line string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-09-18.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestANoulRoundTripsAsANumberNotAString(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	row := Row{
		At: at, Point: "tool_gate", Questions: "tool_gate", Version: 1,
		Build: "jev-1.13.0-2026-09-11", Model: "~typesafe/jev-latest", StateHash: "9d1f0c",
		Answers: []Answer{{Question: "approval", Wording: 1, Kind: AnswerNoul, Noul: 0.09}},
	}
	stored, err := NewWriter(dir).Append(row)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, stored.Day()+logSuffix))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("decode: %v", err)
	}
	answers, ok := onDisk["answers"].([]any)
	if !ok || len(answers) != 1 {
		t.Fatalf("expected one answer on disk, got %+v", onDisk["answers"])
	}
	stored0 := answers[0].(map[string]any)
	if _, isString := stored0["noul"].(string); isString {
		t.Fatalf("noul is stored as a string, the row on disk: %s", raw)
	}
	if _, isNumber := stored0["noul"].(float64); !isNumber {
		t.Fatalf("noul must be a JSON number, the row on disk: %s", raw)
	}
	if _, hasChoice := stored0["choice"]; hasChoice {
		t.Fatalf("a noul answer must not carry a choice field, the row on disk: %s", raw)
	}

	rows, _ := readAll(t, dir, Filter{})
	if len(rows) != 1 || len(rows[0].Answers) != 1 {
		t.Fatalf("expected one row with one answer, got %+v", rows)
	}
	got := rows[0].Answers[0]
	if got.Kind != AnswerNoul {
		t.Fatalf("kind = %q, want %q", got.Kind, AnswerNoul)
	}
	if got.Noul != 0.09 {
		t.Fatalf("noul = %v, want 0.09", got.Noul)
	}
	if got.Choice != "" {
		t.Fatalf("a decoded noul must not carry a choice, got %q", got.Choice)
	}
}

func TestAnOldShapeRowStillReads(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"answers":[{"choice":"0.09","dist":null,"question":"approval","wording":1},{"choice":"0","dist":[{"option":"0","p":1},{"option":"1","p":0},{"option":"2","p":0},{"option":"3","p":0}],"question":"risk","wording":1}],"at":"2026-09-18T19:24:34.242Z","build":"typesafe/jev-1.13-20260917","cost":3.7842e-05,"id":"2026-09-18-legacy0000000000000000000000000","latency_ms":503,"model":"~typesafe/jev-latest","point":"tool_gate","questions":"tool_gate","request_id":"gen-dec-legacy","schema":1,"state_hash":"b1620c36","verdict":"","version":1}`
	writeLegacyLine(t, dir, legacy)

	rows, report := readAll(t, dir, Filter{})
	if len(report.Corrupt) != 0 {
		t.Fatalf("an old-shape row must not be reported corrupt, got %+v", report.Corrupt)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	answers := rows[0].Answers
	if len(answers) != 2 {
		t.Fatalf("expected 2 answers, got %d", len(answers))
	}

	approval := answers[0]
	if approval.Kind != AnswerNoul {
		t.Fatalf("approval kind = %q, want %q", approval.Kind, AnswerNoul)
	}
	if approval.Noul != 0.09 {
		t.Fatalf("approval noul = %v, want 0.09", approval.Noul)
	}

	risk := answers[1]
	if risk.Kind != AnswerScore {
		t.Fatalf("risk kind = %q, want %q, a numeric-level distribution reads as a score", risk.Kind, AnswerScore)
	}
	if risk.Score != 0 {
		t.Fatalf("risk score = %v, want 0", risk.Score)
	}
	if len(risk.Dist) != 4 {
		t.Fatalf("risk must keep its distribution, got %+v", risk.Dist)
	}
	t.Logf("old-shape row read as: approval %+v, risk %+v", approval, risk)
}

func TestALegacyNoulThatIsNotAParsableNumberIsReportedCorrupt(t *testing.T) {
	dir := t.TempDir()
	bad := `{"answers":[{"choice":"not-a-number","dist":null,"question":"approval","wording":1}],"at":"2026-09-18T19:24:34.242Z","build":"b","cost":0,"id":"2026-09-18-bad00000000000000000000000000000","latency_ms":0,"model":"m","point":"tool_gate","questions":"tool_gate","request_id":"r","schema":1,"state_hash":"h","verdict":"","version":1}`
	writeLegacyLine(t, dir, bad)
	_, report := readAll(t, dir, Filter{})
	if len(report.Corrupt) != 1 || !strings.Contains(report.Corrupt[0].Err.Error(), "approval") {
		t.Fatalf("an unparsable legacy noul must be reported corrupt and name the question, got %+v", report.Corrupt)
	}
}
