package turn

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/judge/method"
	"tofu/internal/llm"
	"tofu/internal/sift"
)

func methodTable(t *testing.T, named string) method.Table {
	t.Helper()
	rows := "kind: method_table\ntable_version: 1\nnotes: a fixture\nmethods:\n"
	for _, point := range []string{shellSiftPoint, "stop_check"} {
		rows += "  " + point + ":\n    method: " + named + "\n    why: a fixture\n"
		if named != string(method.Unwired) {
			rows += "    cost: nothing, it is a fixture\n"
		}
	}
	table, err := method.Parse([]byte(rows), "testdata/methods@1.yaml")
	if err != nil {
		t.Fatalf("parsing the fixture table: %v", err)
	}
	return table
}

const siftedCommand = "go test ./internal/turn/..."

func shellOutput() string {
	var out strings.Builder
	out.WriteString("=== RUN   TestOne\n--- PASS: TestOne (0.01s)\n\n")
	for i := 1; i <= 20; i++ {
		out.WriteString("ok  \tpackage number " + strconv.Itoa(i) + "\t0.0" + strconv.Itoa(i%9) + "s\n")
	}
	out.WriteString("\nPASS\nok  \ttofu/internal/turn\t1.2s\n")
	return out.String()
}

type fakeScores struct {
	needed map[int]float64
	asked  int
}

func (f *fakeScores) Score(_ context.Context, _ sift.Shell, units []sift.Unit, _ string) (map[int]float64, error) {
	scores := map[int]float64{}
	for _, unit := range units {
		if unit.Held != sift.NotHeld {
			continue
		}
		f.asked++
		scores[unit.Index] = f.needed[unit.Index]
	}
	return scores, nil
}

func siftedTurn(t *testing.T, sifter *ShellSift) (*stubModel, Row) {
	t.Helper()
	zero := 0
	shell := &stubTool{name: bashToolName, result: Result{Content: shellOutput(), Command: siftedCommand, ExitCode: &zero}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: bashToolName, Arguments: json.RawMessage(`{"command":"` + siftedCommand + `"}`)}),
		messageDecision(),
	}}
	config := baseConfig(t, model, NewRegistry(shell))
	config.Sift = sifter
	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	return model, row
}

func resultTheModelSaw(t *testing.T, model *stubModel) string {
	t.Helper()
	for _, message := range model.requests[len(model.requests)-1].Messages {
		if message.Role == llm.RoleTool {
			return message.Content
		}
	}
	t.Fatal("the model was sent no tool result")
	return ""
}

func TestAJudgedShellResultReachesTheModelSmallerThanTheRawOutput(t *testing.T) {
	scores := &fakeScores{needed: map[int]float64{1: 0.08}}
	sifter := &ShellSift{Methods: methodTable(t, string(method.Judged)), KeepAt: 0.5, Scores: scores}

	model, row := siftedTurn(t, sifter)

	raw := shellOutput()
	sent := resultTheModelSaw(t, model)
	if len(sent) >= len(raw) {
		t.Fatalf("the model was sent %d bytes of a %d byte result, so nothing was cut:\n%s", len(sent), len(raw), sent)
	}
	if scores.asked != 1 {
		t.Fatalf("the judged arm was asked about %d chunks, and only the middle chunk is a candidate", scores.asked)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.SiftSavedBytes != len(raw)-len(sent) {
		t.Fatalf("the row says %d bytes were saved and the model saw %d fewer", call.SiftSavedBytes, len(raw)-len(sent))
	}
	t.Logf("%d bytes became %d, %d saved:\n%s", len(raw), len(sent), call.SiftSavedBytes, sent)
}

func TestAnUnwiredShellSiftLeavesTheResultWhole(t *testing.T) {
	scores := &fakeScores{needed: map[int]float64{1: 0.08}}
	sifter := &ShellSift{Methods: methodTable(t, string(method.Unwired)), KeepAt: 0.5, Scores: scores}

	model, row := siftedTurn(t, sifter)

	if sent := resultTheModelSaw(t, model); sent != shellOutput() {
		t.Fatalf("an unwired point changed the result the model saw:\n%s", sent)
	}
	if scores.asked != 0 {
		t.Fatalf("an unwired point asked the judged arm about %d chunks", scores.asked)
	}
	if row.Steps[0].ToolCalls[0].SiftSavedBytes != 0 {
		t.Fatalf("an unwired point removed %d bytes", row.Steps[0].ToolCalls[0].SiftSavedBytes)
	}
}

func TestACutResultStatesHowManyBytesWereRemoved(t *testing.T) {
	sifter := &ShellSift{
		Methods: methodTable(t, string(method.Judged)),
		KeepAt:  0.5,
		Scores:  &fakeScores{needed: map[int]float64{1: 0.08}},
	}

	model, _ := siftedTurn(t, sifter)

	const want = "[sift: 20 lines, 572 bytes elided, still needed 0.08, under 0.50]\n" +
		"PASS\nok  \ttofu/internal/turn\t1.2s\n" +
		"[sift: kept 2 of 3 units, 79 of 651 bytes]\n" +
		"[sift: 572 of 651 bytes of output removed, the judged method]\n"
	sent := resultTheModelSaw(t, model)
	if !strings.HasSuffix(sent, want) {
		t.Fatalf("the model saw\n%q\nand the cut has to end\n%q", sent, want)
	}
}

func TestTheCheapMethodCutsWithNoJudgedArmAtAll(t *testing.T) {
	sifter := &ShellSift{Methods: methodTable(t, string(method.Cheap)), KeepAt: 0.5}

	model, row := siftedTurn(t, sifter)

	sent := resultTheModelSaw(t, model)
	if row.Steps[0].ToolCalls[0].SiftSavedBytes == 0 {
		t.Fatalf("the cheap method removed nothing:\n%s", sent)
	}
	if !strings.Contains(sent, "the cheap method") {
		t.Fatalf("the cut does not name the method that made it:\n%s", sent)
	}
}

func TestAJudgedPointWithNoJudgedArmLeavesTheResultWhole(t *testing.T) {
	sifter := &ShellSift{Methods: methodTable(t, string(method.Judged)), KeepAt: 0.5}

	model, _ := siftedTurn(t, sifter)

	if sent := resultTheModelSaw(t, model); sent != shellOutput() {
		t.Fatalf("a point with no arm to run changed the result:\n%s", sent)
	}
}

func TestAShadowRuleUnderANamedMethodStillRefusesToRun(t *testing.T) {
	shadow := sift.ShellRule{Mode: sift.ModeShadow, ModeDeclared: true, File: "testdata/shell_sift@1.yaml"}

	err := ruleAgreesWithTheTable(methodTable(t, string(method.Judged)), shadow)

	var contradiction RuleContradictsTheTableError
	if !errors.As(err, &contradiction) {
		t.Fatalf("a table naming a method over a rule declaring shadow returned %v", err)
	}
	if contradiction.Method == method.Unwired || contradiction.Mode != sift.ModeShadow {
		t.Fatalf("the refusal is not the judged-over-shadow pair: %+v", contradiction)
	}
	for _, named := range []string{"shell_sift@1.yaml", "methods@1.yaml"} {
		if !strings.Contains(err.Error(), named) {
			t.Fatalf("the refusal does not name %s: %v", named, err)
		}
	}
}

func TestTheShippedRuleAndTheShippedTableCutAResultTheModelSees(t *testing.T) {
	built, err := NewShellSift(&fakeScores{needed: map[int]float64{1: 0.08}})
	if err != nil {
		t.Fatalf("building the sift from the shipped table and the shipped rule: %v", err)
	}
	if siftOrNothing(nil) == nil {
		t.Fatal("a turn configured with no sift built none out of the shipped table and the shipped rule")
	}

	model, row := siftedTurn(t, &built)

	raw := shellOutput()
	sent := resultTheModelSaw(t, model)
	if len(sent) >= len(raw) {
		t.Fatalf("the model was sent %d bytes of a %d byte result, so the shipped pair cut nothing:\n%s", len(sent), len(raw), sent)
	}
	if saved := row.Steps[0].ToolCalls[0].SiftSavedBytes; saved != len(raw)-len(sent) {
		t.Fatalf("the row says %d bytes were saved and the model saw %d fewer", saved, len(raw)-len(sent))
	}
	t.Logf("the shipped pair turned %d bytes into %d:\n%s", len(raw), len(sent), sent)
}
