package phase

import (
	"testing"

	"tofu/internal/agent/loop"
)

const leastPhaseAgreement = 0.8

func shell(command string) loop.ToolCallRow { return loop.ToolCallRow{Tool: "bash", Command: command} }

func called(tools ...string) []loop.ToolCallRow {
	calls := make([]loop.ToolCallRow, len(tools))
	for index, tool := range tools {
		calls[index] = loop.ToolCallRow{Tool: tool, Command: "internal/widget/widget_test.go"}
	}
	return calls
}

func TestPhaseLabelsAgreeWithHandLabelsOnMockedSteps(t *testing.T) {
	steps := []struct {
		calls []loop.ToolCallRow
		hand  loop.Phase
	}{
		{called("read"), loop.PhaseExplore},
		{called("glob", "search"), loop.PhaseExplore},
		{called("symbols", "read", "read"), loop.PhaseExplore},
		{called("plan", "read"), loop.PhaseExplore},
		{called("project_report"), loop.PhaseExplore},
		{[]loop.ToolCallRow{shell("grep -rn test internal/widget")}, loop.PhaseExplore},
		{[]loop.ToolCallRow{shell("git log --oneline -5")}, loop.PhaseExplore},
		{called("edit"), loop.PhaseEdit},
		{called("write", "plan"), loop.PhaseEdit},
		{called("edit", "test"), loop.PhaseEdit},
		{called("test"), loop.PhaseVerify},
		{called("typecheck"), loop.PhaseVerify},
		{called("tofu_lint_comments"), loop.PhaseVerify},
		{[]loop.ToolCallRow{shell("go vet ./internal/widget/...")}, loop.PhaseVerify},
		{[]loop.ToolCallRow{shell("cd web && npm test")}, loop.PhaseVerify},
		{[]loop.ToolCallRow{shell("npx tsc --noEmit")}, loop.PhaseVerify},
		{[]loop.ToolCallRow{shell("python -m pytest tests/")}, loop.PhaseVerify},
		{[]loop.ToolCallRow{shell("bash scripts/check_port.sh 8080")}, loop.PhaseExplore},
		{nil, loop.PhaseReport},
		{[]loop.ToolCallRow{}, loop.PhaseReport},
	}
	agreed := 0
	for index, step := range steps {
		labelled := loop.PhaseOf(step.calls)
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
