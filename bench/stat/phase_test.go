package stat

import (
	"testing"

	"tofu/internal/agent/loop"
)

const leastPhaseAgreement = 0.8

func shell(command string) turn.ToolCallRow { return turn.ToolCallRow{Tool: "bash", Command: command} }

func called(tools ...string) []turn.ToolCallRow {
	calls := make([]turn.ToolCallRow, len(tools))
	for index, tool := range tools {
		calls[index] = turn.ToolCallRow{Tool: tool, Command: "internal/widget/widget_test.go"}
	}
	return calls
}

func TestPhaseLabelsAgreeWithHandLabelsOnMockedSteps(t *testing.T) {
	steps := []struct {
		calls []turn.ToolCallRow
		hand  turn.Phase
	}{
		{called("read"), turn.PhaseExplore},
		{called("glob", "search"), turn.PhaseExplore},
		{called("symbols", "read", "read"), turn.PhaseExplore},
		{called("plan", "read"), turn.PhaseExplore},
		{called("project_report"), turn.PhaseExplore},
		{[]turn.ToolCallRow{shell("grep -rn test internal/widget")}, turn.PhaseExplore},
		{[]turn.ToolCallRow{shell("git log --oneline -5")}, turn.PhaseExplore},
		{called("edit"), turn.PhaseEdit},
		{called("write", "plan"), turn.PhaseEdit},
		{called("edit", "test"), turn.PhaseEdit},
		{called("test"), turn.PhaseVerify},
		{called("typecheck"), turn.PhaseVerify},
		{called("tofu_lint_comments"), turn.PhaseVerify},
		{[]turn.ToolCallRow{shell("go vet ./internal/widget/...")}, turn.PhaseVerify},
		{[]turn.ToolCallRow{shell("cd web && npm test")}, turn.PhaseVerify},
		{[]turn.ToolCallRow{shell("npx tsc --noEmit")}, turn.PhaseVerify},
		{[]turn.ToolCallRow{shell("python -m pytest tests/")}, turn.PhaseVerify},
		{[]turn.ToolCallRow{shell("bash scripts/check_port.sh 8080")}, turn.PhaseExplore},
		{nil, turn.PhaseReport},
		{[]turn.ToolCallRow{}, turn.PhaseReport},
	}
	agreed := 0
	for index, step := range steps {
		labelled := turn.PhaseOf(step.calls)
		if labelled == step.hand {
			agreed++
			continue
		}
		t.Logf("step %d: labelled %s, hand label %s", index+1, labelled, step.hand)
	}
	agreement := float64(agreed) / float64(len(steps))
	t.Logf("phase labels agree with hand labels on %d of %d mocked steps: %.2f", agreed, len(steps), agreement)
	if agreement < leastPhaseAgreement {
		t.Fatalf("agreement %.2f is under %.2f", agreement, leastPhaseAgreement)
	}
}
