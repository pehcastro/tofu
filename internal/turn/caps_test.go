package turn

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

func readCall(index int) llm.Decision {
	return toolCallDecision(llm.ToolCall{ID: "call-" + strconv.Itoa(index), Name: "read", Arguments: json.RawMessage(`{"path":"` + strconv.Itoa(index) + `.txt"}`)})
}

func lastUserText(request llm.Request) string {
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role == llm.RoleUser {
			return request.Messages[i].Content
		}
	}
	return ""
}

func TestATurnCappedAtFiveStepsIsWarnedOnItsFourthRequestAndAnswersOnItsFifth(t *testing.T) {
	for _, last := range []llm.Decision{
		claimDecision("read four of five; the fifth is left"),
		{Build: "m1", Outcome: llm.OutcomeToolCalls, Content: "read four of five; the fifth is left", ToolCalls: []llm.ToolCall{{ID: "call-5", Name: "read", Arguments: json.RawMessage(`{"path":"5.txt"}`)}}},
	} {
		read := &stubTool{name: "read", result: Result{Content: "a note"}, varying: true}
		model := &stubModel{decisions: []llm.Decision{readCall(1), readCall(2), readCall(3), readCall(4), last}}
		row, err := Run(context.Background(), Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(read), Task: "read the five notes",
			Caps: Caps{MaxSteps: 5}, ResultBytesCap: 4096, ArtifactDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		for i, request := range model.requests {
			t.Logf("request %d: %d tools, last user message %q", i+1, len(request.Tools), lastUserText(request))
			noticed := strings.Contains(lastUserText(request), "steps left before its step cap of 5")
			if noticed != (i == 3) {
				t.Errorf("request %d carries the notice: %v, want it on request 4 alone", i+1, noticed)
			}
		}
		if len(model.requests) != 5 || !strings.Contains(lastUserText(model.requests[4]), "this is its last step") {
			t.Fatalf("the turn sent %d requests, want 5 with the last-step message on the fifth", len(model.requests))
		}
		if read.calls != 4 {
			t.Errorf("read ran %d times, want 4: the last step runs no tool", read.calls)
		}
		answer := row.Steps[len(row.Steps)-1]
		if row.Outcome != OutcomeStepCap || len(row.Steps) != 5 || answer.AssistantText != last.Content {
			t.Errorf("outcome %s after %d steps answering %q, want step_cap after 5 answering %q", row.Outcome, len(row.Steps), answer.AssistantText, last.Content)
		}
		for _, refused := range answer.ToolCalls {
			if !strings.Contains(refused.Error, theLastStepRunsNoTool) {
				t.Errorf("the call asked on the last step was not refused: %+v", refused)
			}
		}
	}
}

type sleepingTool time.Duration

func (sleepingTool) Name() string { return "bash" }

func (sleepingTool) Definition() llm.Tool {
	return llm.Tool{Name: "bash", Parameters: map[string]any{"type": "object"}}
}

func (s sleepingTool) Run(context.Context, json.RawMessage) (Result, error) {
	time.Sleep(time.Duration(s))
	return Result{Content: "slept"}, nil
}

func TestASubAgentPastItsWallClockFinishesItsStepAndAnswersOnceRetired(t *testing.T) {
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-sleep", Name: "bash", Arguments: json.RawMessage(`{"command":"sleep 3"}`)}),
		claimDecision("slept once; the build is not started"),
	}}
	config := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(sleepingTool(3 * time.Second)), ResultBytesCap: 4096,
		ArtifactDir: t.TempDir(), NewID: func() string { return "turn-orchestrator" }}
	spawn := NewSpawnTool("turn-orchestrator", config, &subagent.Roster{})
	spawn.Limits = func() SubAgentLimits { return SubAgentLimits{Running: 1, Depth: 1, WallClock: 2 * time.Second} }
	if _, err := spawn.Run(context.Background(), json.RawMessage(`{"task":"sleep, then build","owns":["build/**"]}`)); err != nil {
		t.Fatal(err)
	}
	report := reported(t, spawn)
	t.Logf("the lead was told:\n%s", report)
	if len(model.requests) != 2 || !strings.Contains(lastUserText(model.requests[1]), "retired at its wall clock cap of 2s") {
		t.Fatalf("the sub-agent sent %d requests, want 2 with the retirement on the second", len(model.requests))
	}
	if !strings.Contains(report, OutcomeRetiredWallClockCap.String()) || !strings.Contains(report, "\n> slept once; the build is not started") {
		t.Errorf("the report does not carry the wall clock outcome and the last answer")
	}
	if tool := model.requests[1].Messages[len(model.requests[1].Messages)-2]; tool.Role != llm.RoleTool || tool.Content != "slept" {
		t.Errorf("the step running when the clock passed did not finish: %+v", tool)
	}
}
