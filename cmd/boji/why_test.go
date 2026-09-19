package main

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"boji/internal/judge/ledger"
	"boji/internal/transport"
)

func writeFixtureLedger(t *testing.T, dir string, decide ...func(*ledger.Row)) (original, replay ledger.Row) {
	t.Helper()
	writer := ledger.NewWriterWithClock(dir, func() time.Time {
		return time.Date(2026, 9, 18, 19, 24, 34, 0, time.UTC)
	})
	row := ledger.Row{
		Point:     "tool_gate",
		Questions: "tool_gate",
		Version:   1,
		Build:     "typesafe/jev-1.13-20260917",
		Model:     "~typesafe/jev-latest",
		StateHash: "b1620c36",
		Answers: []ledger.Answer{
			{Question: "approval", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.06},
			{Question: "risk", Wording: 1, Kind: ledger.AnswerScore, Score: 0, Dist: []ledger.Slice{
				{Option: "0", P: 1}, {Option: "1", P: 0}, {Option: "2", P: 0}, {Option: "3", P: 0},
			}},
		},
		LatencyMS: 503,
		Cost:      3.7842e-05,
		RequestID: "gen-dec-stub",
	}
	for _, mutate := range decide {
		mutate(&row)
	}
	stored, err := writer.Append(row)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	replayed, err := writer.Append(ledger.Row{
		Point:         "tool_gate",
		Questions:     "tool_gate",
		Version:       1,
		Model:         "~typesafe/jev-latest",
		StateHash:     stored.StateHash,
		StateBuilder:  stored.StateBuilder,
		Answers:       stored.Answers,
		Build:         stored.Build,
		RequestID:     stored.RequestID,
		ReplayOf:      stored.ID,
		Verdict:       stored.Verdict,
		Policy:        stored.Policy,
		PolicyVersion: stored.PolicyVersion,
		Reason:        stored.Reason,
		TurnID:        stored.TurnID,
	})
	if err != nil {
		t.Fatalf("Append replay: %v", err)
	}
	return stored, replayed
}

func TestWhyLastPrintsTheMostRecentRowWithEveryDistribution(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	_, replay := writeFixtureLedger(t, dir)

	var out, errOut bytes.Buffer
	code := whyVerb([]string{"--last"}, &out, &errOut, time.Now)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, replay.ID) {
		t.Fatalf("the last row must be the replay %s, got:\n%s", replay.ID, text)
	}
	if !strings.Contains(text, "risk") || !strings.Contains(text, "approval") {
		t.Fatalf("every answer must appear, got:\n%s", text)
	}
	if !strings.Contains(text, "chose 0") {
		t.Fatalf("the distribution-bearing answer must show its distribution, got:\n%s", text)
	}
	if !strings.Contains(text, "threshold  absent") {
		t.Fatalf("the threshold line must say it is absent, got:\n%s", text)
	}
	if strings.Contains(text, "verdict") {
		t.Fatalf("a row with no verdict must print no verdict line, got:\n%s", text)
	}
	t.Logf("boji why --last:\n%s", text)
}

func TestWhyPrintsTheRuleAndThresholdWhenAReasonIsPresent(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	_, _ = writeFixtureLedger(t, dir, func(r *ledger.Row) {
		r.Verdict = ledger.VerdictAsk
		r.Policy = "tool_gate"
		r.PolicyVersion = 1
		r.Reason = &ledger.Reason{Question: "risk", Comparison: "risk_ask_at", Threshold: 1.5, Value: 2, Ambiguous: "user_requested", Mode: ledger.ModeShadow}
	})

	var out, errOut bytes.Buffer
	code := whyVerb([]string{"--last"}, &out, &errOut, time.Now)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "verdict  ASK") {
		t.Fatalf("expected the verdict to print, got:\n%s", text)
	}
	if !strings.Contains(text, "threshold  risk 2.00 vs risk_ask_at 1.50") {
		t.Fatalf("expected the rule that fired to name the question, comparison and threshold, got:\n%s", text)
	}
	if !strings.Contains(text, "ambiguous  user_requested sat inside the dead band") {
		t.Fatalf("expected the ambiguous question to be named, got:\n%s", text)
	}
	if strings.Contains(text, "absent: no policy or calibration lock yet") {
		t.Fatalf("a row with a reason must not print the absent line, got:\n%s", text)
	}
	t.Logf("boji why --last (policy-bearing, ambiguous):\n%s", text)
}

func TestWhyOnAReplayedRowNamesTheRowItReplays(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	original, replay := writeFixtureLedger(t, dir)

	var replayOut, replayErr bytes.Buffer
	if code := whyVerb([]string{replay.ID}, &replayOut, &replayErr, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, replayErr.String())
	}
	replayText := replayOut.String()
	if !strings.Contains(replayText, replay.ID) || !strings.Contains(replayText, "is a replay of") || !strings.Contains(replayText, original.ID) {
		t.Fatalf("the replayed row must name what it replays, got:\n%s", replayText)
	}
	if !strings.Contains(replayText, "chose 0") {
		t.Fatalf("the replayed row must print the original's chain, got:\n%s", replayText)
	}

	var originalOut, originalErr bytes.Buffer
	if code := whyVerb([]string{original.ID}, &originalOut, &originalErr, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, originalErr.String())
	}
	originalText := originalOut.String()
	if strings.Contains(originalText, "is a replay of") {
		t.Fatalf("the original row is not a replay, got:\n%s", originalText)
	}

	t.Logf("boji why %s:\n%s", replay.ID, replayText)
	t.Logf("boji why %s:\n%s", original.ID, originalText)
}

func TestWhyOnAnUnknownIDExitsTwoAndNamesWhereItLooked(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	writeFixtureLedger(t, dir)

	var out, errOut bytes.Buffer
	code := whyVerb([]string{"2026-09-18-doesnotexist00000000000000000000"}, &out, &errOut, time.Now)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must stay empty on a miss, got %q", out.String())
	}
	msg := errOut.String()
	if !strings.Contains(msg, "2026-09-18-doesnotexist00000000000000000000") {
		t.Fatalf("the error must name the id, got %q", msg)
	}
	if !strings.Contains(msg, dir) {
		t.Fatalf("the error must name where it looked, got %q", msg)
	}
}

func TestWhyJSONParsesAndCarriesEveryStoredField(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	modeReason := "declared shadow in catalog/policy/tool_gate@1.yaml"
	_, replay := writeFixtureLedger(t, dir, func(r *ledger.Row) {
		r.Verdict = ledger.VerdictAllow
		r.Policy = "tool_gate"
		r.PolicyVersion = 1
		r.Reason = &ledger.Reason{
			Question: "risk", Comparison: "risk_ask_at", Threshold: 1.5, Value: 0,
			DeadBand: true, RelaxedBy: "user_requested", Blocked: true, Ambiguous: "user_requested",
			Mode: ledger.ModeShadow, ModeReason: &modeReason,
		}
		r.StateBuilder = "tool_gate.9f3a21c4"
		r.TurnID = "turn-7"
	})
	if err := ledger.NewWriter(dir).Backfill(replay.ID, ledger.Outcome{Kind: "reverted", Detail: "test"}); err != nil {
		t.Fatalf("Backfill: %v", err)
	}

	var out, errOut bytes.Buffer
	code := whyVerb([]string{replay.ID, "--json"}, &out, &errOut, time.Now)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}

	var fields map[string]any
	if err := json.Unmarshal(out.Bytes(), &fields); err != nil {
		t.Fatalf("--json output does not parse: %v\n%s", err, out.String())
	}

	rowType := reflect.TypeOf(ledger.Row{})
	for i := 0; i < rowType.NumField(); i++ {
		tag := rowType.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if _, ok := fields[name]; !ok {
			t.Errorf("the row carries field %q, --json dropped it: %s", name, out.String())
		}
	}

	reason, ok := fields["reason"].(map[string]any)
	if !ok {
		t.Fatalf("the row carries a reason, --json dropped it: %s", out.String())
	}
	reasonType := reflect.TypeOf(ledger.Reason{})
	for i := 0; i < reasonType.NumField(); i++ {
		tag := reasonType.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if _, ok := reason[name]; !ok {
			t.Errorf("the reason carries field %q, --json dropped it: %s", name, out.String())
		}
	}
	t.Logf("boji why %s --json:\n%s", replay.ID, out.String())
}

func TestWhyStateBuilderReadsDifferentlyForAnOldSchemaRowAndAnUnadoptedWriter(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	unadopted, _ := writeFixtureLedger(t, dir)

	oldID := "2026-09-01-00000000000000000000000000000000"
	oldLine := `{"id":"2026-09-01-00000000000000000000000000000000","schema":2,"at":"2026-09-01T12:00:00Z","point":"tool_gate","questions":"tool_gate","version":1,"build":"jev-1.13.0","model":"~typesafe/jev-latest","state_hash":"deadbeef","answers":[],"verdict":"ask","latency_ms":10,"cost":0.0001,"request_id":"or-req-old"}`
	if err := os.WriteFile(filepath.Join(dir, "2026-09-01.jsonl"), []byte(oldLine+"\n"), 0o644); err != nil {
		t.Fatalf("writing old-schema fixture: %v", err)
	}

	var unadoptedOut, unadoptedErr bytes.Buffer
	if code := whyVerb([]string{unadopted.ID}, &unadoptedOut, &unadoptedErr, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, unadoptedErr.String())
	}
	var oldOut, oldErr bytes.Buffer
	if code := whyVerb([]string{oldID}, &oldOut, &oldErr, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, oldErr.String())
	}

	unadoptedText, oldText := unadoptedOut.String(), oldOut.String()
	if !strings.Contains(unadoptedText, "state absent, writer has not adopted the state builder") {
		t.Fatalf("a current-schema row with no state builder must say the writer has not adopted it, got:\n%s", unadoptedText)
	}
	if !strings.Contains(oldText, "state absent, schema 2 predates the state builder") {
		t.Fatalf("an old-schema row must say its schema predates the state builder, got:\n%s", oldText)
	}
	t.Logf("current schema, empty state builder:\n%s", unadoptedText)
	t.Logf("old schema, empty state builder:\n%s", oldText)
}

func TestWhyOnAFallbackRowSaysTheTypedDecisionWasNotMade(t *testing.T) {
	reader, _ := replayTestReader(t)
	row := gateFallbackRow(t, reader, stubJevWire{err: transport.Fail("stub", transport.KindTimeout, nil, "no answer in 2.5 s")})

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{row.ID}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "the typed decision was not made") {
		t.Fatalf("a fallback row must say the decision was not made, got:\n%s", text)
	}
	if strings.Contains(text, " vs ") {
		t.Fatalf("a fallback row must print no threshold comparison, got:\n%s", text)
	}

	var jsonOut, jsonErr bytes.Buffer
	if code := whyVerb([]string{row.ID, "--json"}, &jsonOut, &jsonErr, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, jsonErr.String())
	}
	var fields map[string]any
	if err := json.Unmarshal(jsonOut.Bytes(), &fields); err != nil {
		t.Fatalf("--json output does not parse: %v\n%s", err, jsonOut.String())
	}
	threshold, ok := fields["threshold"].(map[string]any)
	if !ok || threshold["present"] != false {
		t.Fatalf("--json claims a threshold was compared: %s", jsonOut.String())
	}
	t.Logf("boji why %s:\n%s", row.ID, text)
	t.Logf("boji why %s --json:\n%s", row.ID, jsonOut.String())
}

func TestWhyNeedsAnIDOrLast(t *testing.T) {
	var out, errOut bytes.Buffer
	code := whyVerb(nil, &out, &errOut, time.Now)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "id or --last") {
		t.Fatalf("the error must say an id or --last is needed, got %q", errOut.String())
	}
}

func TestWhyLastFiveTakesFiveMostRecentAtAPoint(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	writer := ledger.NewWriterWithClock(dir, func() time.Time { return time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC) })
	at := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		point := "tool_gate"
		if i%2 == 0 {
			point = "recall_drop"
		}
		row := ledger.Row{
			Point: point, Questions: point, Version: 1, Build: "b", Model: "m",
			StateHash: strconv.Itoa(i), At: at.Add(time.Duration(i) * time.Minute),
			Answers: []ledger.Answer{{Question: "q", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.1}},
		}
		if _, err := writer.Append(row); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	var out, errOut bytes.Buffer
	code := whyVerb([]string{"--point", "tool_gate", "--last", "5"}, &out, &errOut, time.Now)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	got := strings.Count(out.String(), "tool_gate  ")
	if got != 4 {
		t.Fatalf("only 4 tool_gate rows exist across 8 writes, --last 5 must report all 4, got %d in:\n%s", got, out.String())
	}
}

func TestWhyMakesNoNetworkCall(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "why.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing why.go: %v", err)
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if strings.Contains(path, "jev") || strings.Contains(path, "transport") || strings.Contains(path, "net/http") || strings.Contains(path, "openrouter") {
			t.Fatalf("why.go imports %q, a command that explains the ledger must never be able to reach a wire", path)
		}
	}

	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	writeFixtureLedger(t, dir)
	t.Setenv("OPENROUTER_KEY", "")

	var out, errOut bytes.Buffer
	code := whyVerb([]string{"--last"}, &out, &errOut, time.Now)
	if code != exitOK {
		t.Fatalf("boji why must work with no key and no wire configured, exit = %d, stderr %s", code, errOut.String())
	}
	if out.Len() == 0 {
		t.Fatal("boji why produced no output despite a fixture ledger")
	}
}
