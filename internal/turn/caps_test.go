package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"boji/internal/llm"
)

func alwaysToolCallModel(n int) *stubModel {
	decisions := make([]llm.Decision, n)
	for i := range decisions {
		decisions[i] = llm.Decision{
			Build:   "m1",
			Outcome: llm.OutcomeToolCalls,
			ToolCalls: []llm.ToolCall{
				{ID: "call", Name: "noop", Arguments: json.RawMessage(`{}`)},
			},
		}
	}
	return &stubModel{decisions: decisions}
}

func cappedConfig(model Model, caps Caps) Config {
	return Config{
		Model:          model,
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(&stubTool{name: "noop", result: Result{Content: "ok"}}),
		Task:           "loop forever",
		Caps:           caps,
		ResultBytesCap: 4096,
	}
}

func TestRunAnswersWithWhatItHasAtTheStepCap(t *testing.T) {
	model := alwaysToolCallModel(3)
	model.decisions = append(model.decisions, llm.Decision{
		Build:   "m1",
		Outcome: llm.OutcomeMessage,
		Content: "i read three files and did not finish",
	})

	row, err := Run(context.Background(), cappedConfig(model, Caps{MaxSteps: 3}))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeStepCap {
		t.Fatalf("outcome = %s, want the row to still name the cap it reached", row.Outcome)
	}
	if len(row.Steps) != 4 {
		t.Fatalf("expected 3 capped steps and one answer, got %d steps", len(row.Steps))
	}
	last := row.Steps[3]
	if last.Index != 4 || last.AssistantText != "i read three files and did not finish" {
		t.Fatalf("the last step is %+v, want the answer the model gave when its budget ran out", last)
	}
	if len(row.Warnings) != 0 {
		t.Fatalf("an answered turn warns about nothing, got %v", row.Warnings)
	}
}

func TestRunAnswersWithWhatItHasAtTheDecisionCap(t *testing.T) {
	model := alwaysToolCallModel(2)
	model.decisions = append(model.decisions, llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "one file read, no verdict left"})
	config := cappedConfig(model, Caps{MaxDecisions: 1})
	config.Gate = gateSaying("allow")

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeDecisionCap {
		t.Fatalf("outcome = %s, want decision_cap", row.Outcome)
	}
	last := row.Steps[len(row.Steps)-1]
	if last.AssistantText != "one file read, no verdict left" {
		t.Fatalf("last step = %+v, want the answer the model gave when its budget ran out", last)
	}
}

func TestTheLastCallIsOfferedNoTools(t *testing.T) {
	model := alwaysToolCallModel(2)
	model.decisions = append(model.decisions, messageDecision())

	if _, err := Run(context.Background(), cappedConfig(model, Caps{MaxSteps: 2})); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if len(model.requests) != 3 {
		t.Fatalf("expected 2 working calls and one last call, got %d", len(model.requests))
	}
	if len(model.requests[0].Tools) == 0 {
		t.Fatal("the working calls were offered no tools, so this proves nothing")
	}
	if tools := model.requests[2].Tools; len(tools) != 0 {
		t.Fatalf("the last call was offered %d tools, and it must be offered none", len(tools))
	}
}

func TestTheLastCallAsksForWhatIsDoneAndWhatIsLeft(t *testing.T) {
	model := alwaysToolCallModel(1)
	model.decisions = append(model.decisions, messageDecision())

	if _, err := Run(context.Background(), cappedConfig(model, Caps{MaxSteps: 1})); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if len(model.requests) != 2 {
		t.Fatalf("expected one working call and one last call, got %d", len(model.requests))
	}
	asked := model.requests[1].Messages
	instruction := asked[len(asked)-1]
	if instruction.Role != llm.RoleUser {
		t.Fatalf("the instruction was sent as %s, and only a user message is read by every wire", instruction.Role)
	}
	for _, part := range []string{"step_cap", "what you did", "what is left undone", "what to do next"} {
		if !strings.Contains(instruction.Content, part) {
			t.Fatalf("the instruction %q does not name %q", instruction.Content, part)
		}
	}
}

func TestACapOnTheFirstStepGetsNoLastCall(t *testing.T) {
	model := &stubModel{decisions: []llm.Decision{toolCallDecision(
		llm.ToolCall{ID: "c1", Name: "noop", Arguments: json.RawMessage(`{}`)},
		llm.ToolCall{ID: "c2", Name: "noop", Arguments: json.RawMessage(`{}`)},
	)}}
	config := cappedConfig(model, Caps{MaxDecisions: 1})
	config.Gate = gateSaying("allow")

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeDecisionCap {
		t.Fatalf("outcome = %s, want decision_cap", row.Outcome)
	}
	if model.calls != 1 {
		t.Fatalf("the model was called %d times: a cap on the first step has nothing to answer with", model.calls)
	}
}

func TestALastCallThatFailsEndsTheTurnAtTheCap(t *testing.T) {
	model := alwaysToolCallModel(2)

	row, err := Run(context.Background(), cappedConfig(model, Caps{MaxSteps: 2}))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeStepCap {
		t.Fatalf("outcome = %s, want the cap to stand when the last call fails", row.Outcome)
	}
	if len(row.Steps) != 2 {
		t.Fatalf("a failed last call records no step, got %d", len(row.Steps))
	}
	if len(row.Warnings) != 1 || !strings.Contains(row.Warnings[0], "the last answer was not obtained") {
		t.Fatalf("warnings = %v, want one saying the answer was not obtained", row.Warnings)
	}
}

func TestNoLastWordTurnsTheLastCallOff(t *testing.T) {
	model := alwaysToolCallModel(2)
	config := cappedConfig(model, Caps{MaxSteps: 2})
	config.NoLastWord = true

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if model.calls != 2 || len(row.Steps) != 2 || len(row.Warnings) != 0 {
		t.Fatalf("with the last call off the turn stops at the cap: %d calls, %d steps, warnings %v",
			model.calls, len(row.Steps), row.Warnings)
	}
}

func TestNoStepCapByDefault(t *testing.T) {
	model := alwaysToolCallModel(60)
	model.decisions = append(model.decisions, messageDecision())

	row, err := Run(context.Background(), cappedConfig(model, Caps{}))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeStopped || len(row.Steps) != 61 {
		t.Fatalf("outcome %s after %d steps, want a zero cap to bound nothing", row.Outcome, len(row.Steps))
	}
}

func TestARowWrittenUnderTheCostCapStillReads(t *testing.T) {
	stored, err := os.ReadFile(filepath.Join("testdata", "row-written-under-the-cost-cap.json"))
	if err != nil {
		t.Fatalf("read the stored row: %v", err)
	}
	var row Row
	if err := json.Unmarshal(stored, &row); err != nil {
		t.Fatalf("a turn row stored when the cost cap existed no longer reads: %v", err)
	}
	if row.Outcome != OutcomeRetiredCostCap {
		t.Fatalf("outcome = %s, want the retired cost cap", row.Outcome)
	}
	if len(row.Steps) == 0 || row.TotalCostUSD == 0 {
		t.Fatalf("the rest of the stored row did not survive: %+v", row)
	}
}
