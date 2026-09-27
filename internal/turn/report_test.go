package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type failsOnceTool struct{ calls int }

func (t *failsOnceTool) Name() string { return "check" }

func (t *failsOnceTool) Definition() llm.Tool {
	return llm.Tool{Name: "check", Description: "a check that fails once", Parameters: map[string]any{"type": "object"}}
}

func (t *failsOnceTool) Run(context.Context, json.RawMessage) (Result, error) {
	t.calls++
	exit := 0
	if t.calls == 1 {
		exit = 2
	}
	return Result{Content: "checked", Command: "check", ExitCode: &exit}, nil
}

func TestASubAgentThatRetriedAFailureAndLearnedNothingReportsNeitherAndEndsDone(t *testing.T) {
	model := &stubModel{decisions: []llm.Decision{
		spawnCall("call-1", "run the check under mine/", "mine/**"),
		toolCallDecision(llm.ToolCall{ID: "check-1", Name: "check", Arguments: json.RawMessage(`{}`)}),
		toolCallDecision(llm.ToolCall{ID: "check-2", Name: "check", Arguments: json.RawMessage(`{}`)}),
		claimDecision("the check passes"),
		messageDecision(),
	}}
	root := t.TempDir()
	base := Config{
		Model:          model,
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(&failsOnceTool{}),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(root, "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
	}
	spawn := NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
	orchestrator := base
	orchestrator.Task = "hand the check to a sub-agent"
	orchestrator.Tools = NewRegistry(spawn)

	if _, err := Run(context.Background(), orchestrator); err != nil {
		t.Fatalf("Run: %v", err)
	}

	last := model.requests[len(model.requests)-1].Messages
	report := last[len(last)-1].Content
	if strings.Contains(report, "learned nothing") || strings.Contains(report, "dismissed") {
		t.Fatalf("the report carries a line about nothing:\n%s", report)
	}
	if !strings.Contains(report, "is finished, done,") {
		t.Fatalf("the report does not say finished and done:\n%s", report)
	}
	if held := onlySubAgent(t, spawn); held.State != subagent.Finished {
		t.Fatalf("a sub-agent nothing reviewed ended %s, want finished", held.State)
	}
}
