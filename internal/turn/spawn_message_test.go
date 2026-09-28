package turn

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

func messageTurn(t *testing.T, to string, decisions ...llm.Decision) (*stubModel, Row) {
	t.Helper()
	orchestrator, spawn := orchestratorTurn(t, t.TempDir(), nil)
	spawn.SubAgents.Defined = []subagent.Definition{{Name: "ts-dev", Runs: subagent.RunsModel, Instructions: "you write typescript"}}
	brief, err := json.Marshal(spawnArgs{Task: "add the users route", Owns: []string{"src/users.ts"}, Agent: "ts-dev"})
	if err != nil {
		t.Fatal(err)
	}
	sent, err := json.Marshal(map[string]string{"to": to, "text": "also add /health"})
	if err != nil {
		t.Fatal(err)
	}
	model := &stubModel{decisions: append([]llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-spawn", Name: "spawn", Arguments: brief}),
		claimDecision("the users route is added"),
		toolCallDecision(llm.ToolCall{ID: "call-message", Name: "message", Arguments: sent})}, decisions...)}
	orchestrator.Model, spawn.base.Model = model, model
	row, err := Run(context.Background(), orchestrator)
	if err != nil {
		t.Fatalf("the orchestrator's turn failed: %v", err)
	}
	return model, row
}

func messageResult(t *testing.T, model *stubModel) string {
	t.Helper()
	for _, request := range model.requests {
		if last := request.Messages[len(request.Messages)-1]; last.Role == llm.RoleTool && last.ToolCallID == "call-message" {
			return last.Content
		}
	}
	t.Fatalf("the orchestrator was never asked after its message call")
	return ""
}

func TestAMessageResumesAFinishedSubAgentWithItsHistory(t *testing.T) {
	model, row := messageTurn(t, "ts-dev-1", claimDecision("the health route is added"), messageDecision())
	resumed := model.requests[3].Messages
	said := func(role llm.Role, text string) bool {
		return slices.ContainsFunc(resumed, func(message llm.Message) bool {
			return message.Role == role && strings.Contains(message.Content, text)
		})
	}
	if !said(llm.RoleUser, "add the users route") || !said(llm.RoleAssistant, "the users route is added") {
		t.Errorf("the resumed sub-agent did not see its own history: %+v", resumed)
	}
	if last := resumed[len(resumed)-1]; last.Role != llm.RoleUser || !strings.Contains(last.Content, "also add /health") {
		t.Errorf("the resumed sub-agent's newest message is %+v, want the orchestrator's text", last)
	}
	if answer := messageResult(t, model); !strings.Contains(answer, "the health route is added") {
		t.Errorf("the message's result is %q, want the sub-agent's answer", answer)
	}
	if called := row.Steps[1].ToolCalls; len(called) != 1 || called[0].SubAgentID != "ts-dev-1" {
		t.Errorf("the message call was recorded as %+v, want it carrying ts-dev-1", called)
	}
}

func TestAMessageToAnUnknownNameIsRefusedListingTheNames(t *testing.T) {
	model, _ := messageTurn(t, "ts-dev-9", messageDecision())
	refused := messageResult(t, model)
	if !strings.HasPrefix(refused, "error:") || !strings.Contains(refused, "ts-dev-9") || !strings.Contains(refused, "ts-dev-1") {
		t.Errorf("a message to ts-dev-9 answered %q, want a refusal naming ts-dev-1", refused)
	}
}
