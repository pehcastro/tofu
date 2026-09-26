package tokencount

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/bench/corpus"
	"tofu/internal/sys"
)

func sessionsDir() string { return sys.RecordedStateDir("sessions") }

func rawCall(args string) corpus.RecordedCall {
	return corpus.RecordedCall{Tool: "read", Args: json.RawMessage(args)}
}

func TestMeasureStepClassifiesProseToolCallsAndMixed(t *testing.T) {
	cases := []struct {
		name       string
		step       corpus.RecordedStep
		wantShape  Shape
		wantSkip   bool
		wantReason string
	}{
		{"prose only", corpus.RecordedStep{AssistantText: "hello there", CompletionTokens: 3}, ShapeProse, false, ""},
		{"tool call only", corpus.RecordedStep{ToolCalls: []corpus.RecordedCall{rawCall(`{"path":"a.go"}`)}, CompletionTokens: 5}, ShapeToolCallArgs, false, ""},
		{"mixed is excluded", corpus.RecordedStep{AssistantText: "done", ToolCalls: []corpus.RecordedCall{rawCall(`{}`)}, CompletionTokens: 4}, "", true, "mixes assistant text and tool calls"},
		{"neither is excluded", corpus.RecordedStep{CompletionTokens: 4}, "", true, "neither assistant text nor a tool call"},
		{"zero completion tokens is excluded", corpus.RecordedStep{AssistantText: "hi"}, "", true, "no completion_tokens"},
	}
	for _, c := range cases {
		sample, reason := measureStep("turn-1", "anthropic", c.step)
		if c.wantSkip {
			if reason == "" || !strings.Contains(reason, c.wantReason) {
				t.Errorf("%s: reason = %q, want to contain %q", c.name, reason, c.wantReason)
			}
			continue
		}
		if reason != "" {
			t.Fatalf("%s: unexpected skip: %s", c.name, reason)
		}
		if sample.Shape != c.wantShape {
			t.Errorf("%s: shape = %q, want %q", c.name, sample.Shape, c.wantShape)
		}
	}
}

func TestMeasureStepComputesBytesOverFourAgainstReportedTokens(t *testing.T) {
	step := corpus.RecordedStep{AssistantText: "12345678", CompletionTokens: 2}
	sample, reason := measureStep("turn-1", "codex", step)
	if reason != "" {
		t.Fatalf("unexpected skip: %s", reason)
	}
	if sample.Bytes != 8 {
		t.Fatalf("Bytes = %d, want 8", sample.Bytes)
	}
	if sample.Estimate != 2 {
		t.Fatalf("Estimate = %d, want 2", sample.Estimate)
	}
	if sample.ErrorPct != 0 {
		t.Fatalf("ErrorPct = %v, want 0 when estimate matches actual exactly", sample.ErrorPct)
	}

	stepOff := corpus.RecordedStep{AssistantText: "12345678", CompletionTokens: 4}
	sampleOff, _ := measureStep("turn-1", "codex", stepOff)
	if got := sampleOff.ErrorPct; got != 50 {
		t.Fatalf("ErrorPct = %v, want 50, estimate 2 against actual 4", got)
	}
}

func TestStatsMedianWorstAndShareOver10(t *testing.T) {
	samples := []Sample{
		{ErrorPct: 0}, {ErrorPct: 5}, {ErrorPct: 10}, {ErrorPct: 20}, {ErrorPct: 50},
	}
	stats := statsOf("test", samples)
	if stats.N != 5 {
		t.Fatalf("N = %d, want 5", stats.N)
	}
	if stats.MedianPct != 10 {
		t.Fatalf("MedianPct = %v, want 10", stats.MedianPct)
	}
	if stats.WorstPct != 50 {
		t.Fatalf("WorstPct = %v, want 50", stats.WorstPct)
	}
	if stats.ShareOver10 != 40 {
		t.Fatalf("ShareOver10 = %v, want 40 (2 of 5 strictly over 10)", stats.ShareOver10)
	}
}

func TestMeasureStepClassifiesAccountability(t *testing.T) {
	possible := corpus.RecordedStep{AssistantText: "12345678", CompletionTokens: 4}
	sample, _ := measureStep("turn-1", "anthropic", possible)
	if !sample.Accountable {
		t.Fatalf("8 bytes for 4 billed tokens should be accountable")
	}

	impossible := corpus.RecordedStep{AssistantText: "ab", CompletionTokens: 40}
	sample2, _ := measureStep("turn-1", "anthropic", impossible)
	if sample2.Accountable {
		t.Fatalf("2 bytes cannot account for 40 billed tokens, no tokenizer produces more tokens than bytes")
	}
}

func TestByAccountableSplitsOnTheByteFloor(t *testing.T) {
	samples := []Sample{
		{Bytes: 10, Actual: 3, Accountable: true, ErrorPct: 1},
		{Bytes: 2, Actual: 30, Accountable: false, ErrorPct: 90},
	}
	rows := ByAccountable(samples)
	if len(rows) != 2 {
		t.Fatalf("ByAccountable returned %d groups, want 2", len(rows))
	}
	for _, row := range rows {
		if row.N != 1 {
			t.Fatalf("%s: N = %d, want 1", row.Key, row.N)
		}
	}
}

func TestFilterKeepsMatchingSamples(t *testing.T) {
	samples := []Sample{{Shape: ShapeProse}, {Shape: ShapeToolCallArgs}, {Shape: ShapeProse}}
	prose := Filter(samples, func(s Sample) bool { return s.Shape == ShapeProse })
	if len(prose) != 2 {
		t.Fatalf("Filter returned %d, want 2", len(prose))
	}
}

func TestScanThinkingFindsMessagesInATempFixture(t *testing.T) {
	turn := corpus.Turn{RecordedTurn: corpus.RecordedTurn{ID: "turn-fixture", Messages: []corpus.RecordedMessage{
		{Role: "assistant", Content: "no thinking here"},
		{Role: "assistant", Thinking: "a captured thought"},
	}}, Schema: corpus.SchemaSession}
	if total, withThinking := scanThinking(turn); total != 2 || withThinking != 1 {
		t.Fatalf("total %d with thinking %d, want 2 messages and 1 with thinking", total, withThinking)
	}
	singleFile := corpus.Turn{RecordedTurn: corpus.RecordedTurn{ID: "turn-fixture"}, Schema: corpus.SchemaSingleFile}
	if total, withThinking := scanThinking(singleFile); total != 0 || withThinking != 0 {
		t.Fatalf("a single file turn holds no message: total %d with %d", total, withThinking)
	}
}

func TestByWireAndByShapeGroupSamples(t *testing.T) {
	samples := []Sample{
		{Wire: "anthropic", Shape: ShapeProse, ErrorPct: 1},
		{Wire: "anthropic", Shape: ShapeToolCallArgs, ErrorPct: 2},
		{Wire: "codex", Shape: ShapeProse, ErrorPct: 3},
	}
	byWire := ByWire(samples)
	if len(byWire) != 2 {
		t.Fatalf("ByWire returned %d groups, want 2", len(byWire))
	}
	byShape := ByShape(samples)
	if len(byShape) != 2 {
		t.Fatalf("ByShape returned %d groups, want 2", len(byShape))
	}
	for _, row := range byShape {
		if row.Key == string(ShapeProse) && row.N != 2 {
			t.Fatalf("prose group has %d samples, want 2", row.N)
		}
	}
}

func TestReadWireReadsBothStorageSchemasFromTheRealCorpus(t *testing.T) {
	walked, err := corpus.WalkSessions(sessionsDir())
	if err != nil {
		t.Fatalf("WalkSessions: %v", err)
	}
	sawSingleFile, sawHeaderJSONL := false, false
	for _, turn := range walked.Turns {
		switch turn.Schema {
		case corpus.SchemaSingleFile:
			sawSingleFile = true
		case corpus.SchemaHeaderJSONL, corpus.SchemaSession:
			sawHeaderJSONL = true
			if turn.Wire == "" {
				t.Errorf("%s: header/jsonl turn carries no wire field", turn.ID)
			}
		}
	}
	if !sawSingleFile || !sawHeaderJSONL {
		t.Fatalf("corpus does not exercise both storage schemas: single file %v, header/jsonl %v", sawSingleFile, sawHeaderJSONL)
	}
}

func TestRunOverTheRealCorpusProducesUsableSamples(t *testing.T) {
	result, err := Run(sessionsDir())
	if err != nil {
		t.Fatalf("Run(%q): %v", sessionsDir(), err)
	}
	if result.Turns == 0 {
		t.Fatal("no turn was read: the path is wrong or the corpus is empty")
	}
	if result.StepsRead != result.StepsUsable+len(result.StepsSkipped) {
		t.Fatalf("StepsRead %d != StepsUsable %d + skipped %d", result.StepsRead, result.StepsUsable, len(result.StepsSkipped))
	}
	if len(result.Samples) != result.StepsUsable {
		t.Fatalf("len(Samples) = %d, StepsUsable = %d", len(result.Samples), result.StepsUsable)
	}
	if len(result.Samples) == 0 {
		t.Fatal("no usable sample came out of a nonempty corpus")
	}
	for _, s := range result.Samples {
		if s.Wire == "" {
			t.Fatalf("%s step %d: usable sample carries no wire", s.Turn, s.Step)
		}
	}
}

func TestRenderCoversEveryAcceptanceLine(t *testing.T) {
	result, err := Run(sessionsDir())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rendered := Render(ReportInput{Date: "2026-09-24", Machine: "TESTHOST", TestCount: 11, Result: result})
	for _, want := range []string{
		"median", "worst", "share >10%", "tool-call arguments", "prose", "Corpus", "Is four good enough", "wire field",
		"The split: can the visible bytes account for the billed tokens at all",
		"does any recorded step carry its own thinking",
		"no tokenizer",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("report carries no %q", want)
		}
	}
	t.Log("\n" + rendered)
}
