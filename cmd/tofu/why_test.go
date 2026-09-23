package main

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/sys"
	"tofu/internal/transport"
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
		State:     []byte(`{"agent":"tofu","input":{"command":"ls"},"tool":"bash"}`),
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
		State:         stored.State,
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
	dir, err := sys.LogDir()
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
	t.Logf("tofu why --last:\n%s", text)
}

func TestWhyPrintsTheRuleAndThresholdWhenAReasonIsPresent(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
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
	if strings.Contains(text, "absent: no rule or calibration lock yet") {
		t.Fatalf("a row with a reason must not print the absent line, got:\n%s", text)
	}
	t.Logf("tofu why --last (rule-bearing, ambiguous):\n%s", text)
}

func TestWhyOnAReplayedRowNamesTheRowItReplays(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
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

	t.Logf("tofu why %s:\n%s", replay.ID, replayText)
	t.Logf("tofu why %s:\n%s", original.ID, originalText)
}

func TestWhyOnAnUnknownIDExitsTwoAndNamesWhereItLooked(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
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

func storedFields(t *testing.T, dir, id string) map[string]any {
	t.Helper()
	row, found, err := ledger.NewReader(dir).ByID(id)
	if err != nil || !found {
		t.Fatalf("reading row %s back: found %v, err %v", id, found, err)
	}
	line, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshalling the stored row: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(line, &fields); err != nil {
		t.Fatalf("the stored row does not parse: %v\n%s", err, line)
	}
	return fields
}

func TestWhyJSONParsesAndCarriesEveryStoredField(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	modeReason := "declared shadow in library/general/rules/tool_gate@1.yaml"
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

	carried := storedFields(t, dir, replay.ID)
	for name := range carried {
		if _, ok := fields[name]; !ok {
			t.Errorf("the stored row carries field %q, --json dropped it: %s", name, out.String())
		}
	}

	printed, ok := fields["reason"].(map[string]any)
	if !ok {
		t.Fatalf("the row carries a reason, --json dropped it: %s", out.String())
	}
	for name := range carried["reason"].(map[string]any) {
		if _, ok := printed[name]; !ok {
			t.Errorf("the stored reason carries field %q, --json dropped it: %s", name, out.String())
		}
	}
	t.Logf("tofu why %s --json:\n%s", replay.ID, out.String())
}

func TestARowCarryingAFieldThisBuildDoesNotKnowStillReadsAndPrints(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	original, _ := writeFixtureLedger(t, dir)

	fields := storedFields(t, dir, original.ID)
	fields["bench"] = "sift-1"
	fields["a_field_a_later_build_adds"] = "tenth"
	line, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshalling the fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, original.Day()+".jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{original.ID, "--json"}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("a row carrying a field this build does not know did not print: exit %d, stderr %s", code, errOut.String())
	}
	var printed map[string]any
	if err := json.Unmarshal(out.Bytes(), &printed); err != nil {
		t.Fatalf("--json output does not parse: %v\n%s", err, out.String())
	}
	if printed["bench"] != "sift-1" {
		t.Fatalf("the row carries bench sift-1, --json printed %v: %s", printed["bench"], out.String())
	}
	for _, name := range []string{"id", "point", "verdict", "answers", "schema"} {
		if _, ok := printed[name]; !ok {
			t.Fatalf("an unknown field cost the row its %q: %s", name, out.String())
		}
	}
	t.Logf("tofu why %s --json, one field this build knows and one it does not:\n%s", original.ID, out.String())
}

func TestWhyStateBuilderReadsDifferentlyForAnOldSchemaRowAndAnUnadoptedWriter(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
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
	if !strings.Contains(unadoptedText, "builder absent, writer has not adopted the state builder") {
		t.Fatalf("a current-schema row with no state builder must say the writer has not adopted it, got:\n%s", unadoptedText)
	}
	if !strings.Contains(oldText, "builder absent, schema 2 predates the state builder") {
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
	t.Logf("tofu why %s:\n%s", row.ID, text)
	t.Logf("tofu why %s --json:\n%s", row.ID, jsonOut.String())
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
	dir, err := sys.LogDir()
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

func writePrecedentLedger(t *testing.T, dir string) (target, nearest, sameCall ledger.Row) {
	t.Helper()
	at := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	writer := ledger.NewWriterWithClock(dir, func() time.Time { return at })
	gateRow := func(minute int, risk float64, print string) ledger.Row {
		return ledger.Row{
			Point: "tool_gate", Questions: "tool_gate", Version: 3, Build: "b", Model: "m",
			StateHash: strconv.Itoa(minute), Fingerprint: print, At: at.Add(time.Duration(minute) * time.Minute),
			Verdict: ledger.VerdictAsk,
			Answers: []ledger.Answer{{Question: "risk", Wording: 3, Kind: ledger.AnswerNoul, Noul: risk}},
		}
	}
	stored := make([]ledger.Row, 0, 4)
	for _, row := range []ledger.Row{
		gateRow(1, 0.90, "bash.ffffffffffffffff"),
		gateRow(2, 0.40, ""),
		gateRow(3, 0.42, ""),
		gateRow(4, 0.41, "bash.ffffffffffffffff"),
	} {
		written, err := writer.Append(row)
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		stored = append(stored, written)
	}
	return stored[3], stored[2], stored[0]
}

func TestWhyListsThePrecedentsAndPutsTheSameCallFirst(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	target, nearest, sameCall := writePrecedentLedger(t, dir)

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{target.ID}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "precedent  3 at this point") {
		t.Fatalf("the row has three precedents and why reports:\n%s", text)
	}
	first, second := strings.Index(text, sameCall.ID), strings.Index(text, nearest.ID)
	if first < 0 || second < 0 || first > second {
		t.Fatalf("the same call %s is ranked after the nearer answers %s:\n%s", sameCall.ID, nearest.ID, text)
	}
	if !strings.Contains(text, "the same call") || !strings.Contains(text, "answers 0.010 away") {
		t.Fatalf("the shortlist does not say why each row is on it:\n%s", text)
	}
	_, shortlist, _ := strings.Cut(text, "  precedent  ")
	t.Logf("precedent  %s", shortlist)
}

func TestWhyJSONCarriesTheWholeShortlist(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	target, _, sameCall := writePrecedentLedger(t, dir)

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{target.ID, "--json"}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	var fields struct {
		Precedents []struct {
			RowID           string  `json:"row_id"`
			Distance        float64 `json:"distance"`
			SameFingerprint bool    `json:"same_fingerprint"`
			Verdict         string  `json:"verdict"`
		} `json:"precedents"`
	}
	if err := json.Unmarshal(out.Bytes(), &fields); err != nil {
		t.Fatalf("the json does not parse: %v\n%s", err, out.String())
	}
	if len(fields.Precedents) != 3 {
		t.Fatalf("the json carries %d precedents, want the three the shortlist found: %s", len(fields.Precedents), out.String())
	}
	head := fields.Precedents[0]
	if head.RowID != sameCall.ID || !head.SameFingerprint || head.Verdict != string(ledger.VerdictAsk) {
		t.Fatalf("the first precedent reads %+v, want %s matched on its fingerprint", head, sameCall.ID)
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
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	writeFixtureLedger(t, dir)
	t.Setenv("OPENROUTER_KEY", "")

	var out, errOut bytes.Buffer
	code := whyVerb([]string{"--last"}, &out, &errOut, time.Now)
	if code != exitOK {
		t.Fatalf("tofu why must work with no key and no wire configured, exit = %d, stderr %s", code, errOut.String())
	}
	if out.Len() == 0 {
		t.Fatal("tofu why produced no output despite a fixture ledger")
	}
}

func TestWhyPrintsTheStateTheBuilderProduced(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	body := `{"agent":"tofu-run","context":{"user_recent_messages":["can you check if the tests pass?"]},"cwd":"/home/user/project","input":{"command":"git push --force"},"tool":"bash"}`
	original, _ := writeFixtureLedger(t, dir, func(r *ledger.Row) {
		r.StateBuilder = "tool_gate.5e17bb5c"
		r.State = []byte(body)
	})

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{original.ID}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, body) {
		t.Fatalf("tofu why did not print the state the builder produced, got:\n%s", text)
	}
	if !strings.Contains(text, "git push --force") {
		t.Fatalf("the command the decision was about is missing, got:\n%s", text)
	}
	t.Logf("tofu why on a row carrying its state:\n%s", text)
}

func TestWhyOnARowFromBeforeTheStateBodyDoesNotFail(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	old := `{"id":"2026-09-19-b51afec433d77bfbf5390199529615b5","schema":5,"at":"2026-09-19T00:07:22.063Z","point":"tool_gate","questions":"tool_gate","version":1,"build":"typesafe/jev-1.13-20260917","model":"~typesafe/jev-latest","state_hash":"f732f4b8","state_builder":"tool_gate.5e17bb5c","answers":[{"kind":"noul","noul":0.09,"question":"approval","wording":1}],"verdict":"allow","latency_ms":655,"cost":3.7884e-05,"request_id":"gen-dec-stub"}`
	if err := os.WriteFile(filepath.Join(dir, "2026-09-19.jsonl"), []byte(old+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{"2026-09-19-b51afec433d77bfbf5390199529615b5"}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "predates the state body") {
		t.Fatalf("a row from before the state body must say so, got:\n%s", text)
	}
	if !strings.Contains(text, "approval") || !strings.Contains(text, "ALLOW") {
		t.Fatalf("the rest of the row must still print, got:\n%s", text)
	}
	t.Logf("tofu why on a row written before this change:\n%s", text)
}

func TestWhySeparatesARowThatPredatesTheStateBodyFromOneThatSimplyHasNone(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	current, _ := writeFixtureLedger(t, dir, func(r *ledger.Row) {
		r.State = nil
	})

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{current.ID}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	text := out.String()
	if strings.Contains(text, "predates") {
		t.Fatalf("a row on the current schema with no state must not claim to predate the field, got:\n%s", text)
	}
	if !strings.Contains(text, "carries no state body") {
		t.Fatalf("a current-schema row with no state must say so plainly, got:\n%s", text)
	}
}

func TestWhySaysAuthorityCouldNotRelaxADenyAndNamesTheQuestion(t *testing.T) {
	shipped, err := filepath.Abs(filepath.Join("..", "..", "library", "general", "rules", "tool_gate@1.yaml"))
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	ruleBody, err := os.ReadFile(shipped)
	if err != nil {
		t.Fatalf("reading the shipped rule: %v", err)
	}
	root := t.TempDir()
	planted := filepath.Join(root, "library", "general", "rules", "tool_gate@1.yaml")
	if err := os.MkdirAll(filepath.Dir(planted), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(planted, ruleBody, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Chdir(root)

	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	blocked, _ := writeFixtureLedger(t, dir, func(r *ledger.Row) {
		r.Verdict = ledger.VerdictDeny
		r.Policy = "tool_gate"
		r.PolicyVersion = 1
		r.Answers = append(r.Answers,
			ledger.Answer{Question: "from_untrusted", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.97},
			ledger.Answer{Question: "user_requested", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.02})
		r.Reason = &ledger.Reason{Question: "risk", Comparison: "risk_deny_at", Threshold: 2.5, Value: 2.99, Blocked: true, Mode: ledger.ModeShadow}
	})
	plain, _ := writeFixtureLedger(t, dir, func(r *ledger.Row) {
		r.Verdict = ledger.VerdictDeny
		r.Policy = "tool_gate"
		r.PolicyVersion = 1
		r.Reason = &ledger.Reason{Question: "risk", Comparison: "risk_deny_at", Threshold: 2.5, Value: 2.99, Mode: ledger.ModeShadow}
	})

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{blocked.ID}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "authority  from_untrusted 0.97") {
		t.Fatalf("a deny authority could not relax must name from_untrusted and its value, got:\n%s", text)
	}
	if !strings.Contains(text, "nothing could relax this DENY") {
		t.Fatalf("the blocked deny must say nothing could relax it, got:\n%s", text)
	}

	var plainOut, plainErr bytes.Buffer
	if code := whyVerb([]string{plain.ID}, &plainOut, &plainErr, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, plainErr.String())
	}
	if strings.Contains(plainOut.String(), "authority") {
		t.Fatalf("a deny nothing tried to relax must print no authority line, got:\n%s", plainOut.String())
	}
	t.Logf("tofu why on a deny authority could not relax:\n%s", text)
	t.Logf("tofu why on a deny nothing tried to relax:\n%s", plainOut.String())
}

func TestWhyNamesTheQuestionThatRelaxedAVerdict(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	relaxed, _ := writeFixtureLedger(t, dir, func(r *ledger.Row) {
		r.Verdict = ledger.VerdictAsk
		r.Policy = "tool_gate"
		r.PolicyVersion = 1
		r.Answers = append(r.Answers,
			ledger.Answer{Question: "from_untrusted", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.02},
			ledger.Answer{Question: "user_requested", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.95})
		r.Reason = &ledger.Reason{Question: "risk", Comparison: "risk_deny_at", Threshold: 2.5, Value: 2.99, RelaxedBy: "user_requested", Mode: ledger.ModeShadow}
	})

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{relaxed.ID}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "authority  user_requested 0.95 relaxed the verdict to ASK") {
		t.Fatalf("a relaxed verdict must name the question that relaxed it, got:\n%s", text)
	}
	t.Logf("tofu why on a verdict authority relaxed:\n%s", text)
}

func TestWhyStateFetchesAnElidedBodyAndTheRowPointsAtIt(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	body := `{"agent":"tofu","input":{"command":"grep -r ` + strings.Repeat("a", 6*1024) + ` ."},"tool":"bash"}`
	row, _ := writeFixtureLedger(t, dir, func(r *ledger.Row) { r.State = []byte(body) })
	if row.StateElision == nil {
		t.Fatalf("a %d byte state was not elided", len(body))
	}

	var text, textErr bytes.Buffer
	if code := whyVerb([]string{row.ID}, &text, &textErr, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, textErr.String())
	}
	whole := filepath.Join(dir, row.StateElision.File)
	if !strings.Contains(text.String(), whole) {
		t.Fatalf("the elision line must name the whole path %s, got:\n%s", whole, text.String())
	}
	if !strings.Contains(text.String(), "tofu why "+row.ID+" --state") {
		t.Fatalf("the elision line must say how to read the rest, got:\n%s", text.String())
	}

	var fetched, fetchedErr bytes.Buffer
	if code := whyVerb([]string{row.ID, "--state"}, &fetched, &fetchedErr, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, fetchedErr.String())
	}
	if strings.TrimSpace(fetched.String()) != body {
		t.Fatalf("--state must print the whole elided body, got %d bytes:\n%.200s", fetched.Len(), fetched.String())
	}
	t.Logf("tofu why on an elided row:\n%s", text.String())
	t.Logf("tofu why --state printed %d bytes, head: %.120s", fetched.Len(), fetched.String())
}
