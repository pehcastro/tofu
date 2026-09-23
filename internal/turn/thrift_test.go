package turn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"tofu/internal/judge/thrift"
	"tofu/internal/llm"
	"tofu/internal/sift"
)

type fakeThriftScores struct {
	needed map[int]float64
	asked  int
}

func (f *fakeThriftScores) Score(_ context.Context, _, _, _ string, parts []sift.Part) (map[int]float64, error) {
	f.asked++
	scores := map[int]float64{}
	for i, score := range f.needed {
		if i < len(parts) {
			scores[i] = score
		}
	}
	return scores, nil
}

const readParagraphs = "func One() int {\n\treturn 1\n\t// filler filler filler filler filler filler filler filler\n\t// filler filler filler filler filler filler filler filler\n}\n\n" +
	"func Two() int {\n\treturn 2\n}\n\n" +
	"func Three() int {\n\treturn 3\n\t// filler filler filler filler filler filler filler filler\n\t// filler filler filler filler filler filler filler filler\n}\n"

func readTurn(t *testing.T, thrifter *ThriftSift) (*stubModel, Row) {
	t.Helper()
	read := &stubTool{name: "read", result: Result{Content: readParagraphs, Command: "read file.go"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"file.go"}`)}),
		messageDecision(),
	}}
	config := baseConfig(t, model, NewRegistry(read))
	config.Thrift = thrifter
	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	return model, row
}

func TestWithThriftOffNoJudgmentIsAskedAndNoTokenIsSpent(t *testing.T) {
	scores := &fakeThriftScores{needed: map[int]float64{0: 0.01, 2: 0.01}}
	thrifter := &ThriftSift{Rule: thrift.Rule{Mode: thrift.ModeOff, KeepAt: 0.5}, Scores: scores}

	model, row := readTurn(t, thrifter)

	if scores.asked != 0 {
		t.Fatalf("the off setting still asked the judged arm %d times", scores.asked)
	}
	if sent := resultTheModelSaw(t, model); sent != readParagraphs {
		t.Fatalf("the off setting changed the result the model saw:\n%s", sent)
	}
	if row.Steps[0].ToolCalls[0].SiftSavedBytes != 0 {
		t.Fatalf("the off setting removed %d bytes", row.Steps[0].ToolCalls[0].SiftSavedBytes)
	}
}

func TestAThriftCutResultTellsTheModelItWasCompacted(t *testing.T) {
	scores := &fakeThriftScores{needed: map[int]float64{0: 0.01, 2: 0.9}}
	thrifter := &ThriftSift{Rule: thrift.Rule{Mode: thrift.ModeEnforced, KeepAt: 0.5}, Scores: scores}

	model, row := readTurn(t, thrifter)

	sent := resultTheModelSaw(t, model)
	if len(sent) >= len(readParagraphs) {
		t.Fatalf("the model was sent %d bytes of a %d byte result, so nothing was cut:\n%s", len(sent), len(readParagraphs), sent)
	}
	if !strings.Contains(sent, "[thrift:") {
		t.Fatalf("the cut result does not say it was compacted:\n%s", sent)
	}
	if row.Steps[0].ToolCalls[0].SiftSavedBytes == 0 {
		t.Fatal("a cut that shrank the result recorded zero bytes saved")
	}
	if scores.asked != 1 {
		t.Fatalf("the judged arm was asked %d times, want 1", scores.asked)
	}
}

const realRecordedReadFailure = "read: internal/sift/policy.go is not a file under the working directory, and nothing there is named policy.go: nothing was run"

func TestAFailingReadsErrorTextIsNeverDroppedByThrift(t *testing.T) {
	scores := &fakeThriftScores{needed: map[int]float64{0: 0.01}}
	thrifter := &ThriftSift{Rule: thrift.Rule{Mode: thrift.ModeEnforced, KeepAt: 0.5}, Scores: scores}
	read := &stubTool{name: "read", err: errors.New(realRecordedReadFailure)}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"internal/sift/policy.go"}`)}),
		messageDecision(),
	}}
	config := baseConfig(t, model, NewRegistry(read))
	config.Thrift = thrifter

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	if scores.asked != 0 {
		t.Fatalf("a failed call still asked the judged arm %d times", scores.asked)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.Error != realRecordedReadFailure {
		t.Fatalf("the row lost the failure: %q", call.Error)
	}
	sent := resultTheModelSaw(t, model)
	if !strings.Contains(sent, realRecordedReadFailure) {
		t.Fatalf("the model was not told the real error:\n%s", sent)
	}
}
