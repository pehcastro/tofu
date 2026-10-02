package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type sideCallModel struct {
	script   *crew
	spawn    *SpawnTool
	fails    bool
	sideSaw  []llm.Message
	waiting  []subagent.State
	sideRuns int
}

func (m *sideCallModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	if len(request.Tools) == 0 {
		m.sideRuns++
		m.sideSaw = request.Messages
		for _, held := range m.spawn.roster.SubAgents() {
			m.waiting = append(m.waiting, held.State)
		}
		if m.fails {
			return llm.Decision{}, errors.New("the orchestrator's wire is down")
		}
	}
	return m.script.Ask(ctx, request)
}

func askTurn(t *testing.T, fails bool, side []llm.Decision, subAgent ...llm.Decision) *sideCallModel {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plan.md"), []byte("bash-2 already serves the app, so the users route goes first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orchestrator, spawn := orchestratorTurn(t, root, nil)
	args, err := json.Marshal(map[string]string{"question": "may I start the dev server?", "why": "the brief says check the route", "default": "I start it on 3000"})
	if err != nil {
		t.Fatal(err)
	}
	script := newCrew(map[string][]llm.Decision{
		leadKey: {toolCallDecision(llm.ToolCall{ID: "call-plan", Name: "read", Arguments: json.RawMessage(`{"path":"plan.md"}`)}),
			spawnCall("call-spawn", usersRoute, "src/users.ts"), claimDecision("sub-1 is on it"), claimDecision("sub-1 is done")},
		usersRoute: append([]llm.Decision{toolCallDecision(llm.ToolCall{ID: "call-ask", Name: "ask", Arguments: args})}, subAgent...),
		sideKey:    side,
	})
	release := script.hold(usersRoute, 1)
	model := &sideCallModel{script: script, spawn: spawn, fails: fails}
	orchestrator.Model, spawn.base.Model = model, model
	led := startLead(context.Background(), orchestrator, nil)
	waitFor(t, "the lead's first turn to end", func() bool { return len(led.turns()) == 1 })
	release()
	led.wait(t)
	return model
}

func subAgentSaw(t *testing.T, model *sideCallModel) string {
	t.Helper()
	for _, request := range model.script.requests(usersRoute) {
		if last := request.Messages[len(request.Messages)-1]; last.Role == llm.RoleTool && last.ToolCallID == "call-ask" {
			return last.Content
		}
	}
	t.Fatalf("no step of the sub-agent was asked after its ask call answered")
	return ""
}

func TestASubAgentAsksAndItsNextStepSeesTheOrchestratorsAnswer(t *testing.T) {
	const answer = "no, bash-2 serves it on 3003"
	model := askTurn(t, false, []llm.Decision{claimDecision(answer)}, claimDecision("the route is added and checked on 3003"))
	if model.sideRuns != 1 {
		t.Fatalf("the side call ran %d times, want 1", model.sideRuns)
	}
	if len(model.waiting) != 1 || model.waiting[0] != subagent.WaitingAnswer {
		t.Errorf("while the side call ran the roster showed %v, want [%s]", model.waiting, subagent.WaitingAnswer)
	}
	if system := model.sideSaw[0]; system.Role != llm.RoleSystem || !strings.Contains(system.Content, "answer a sub-agent's ask in one line") {
		t.Errorf("the side call's instruction is not the answer_asks rule: %+v", system)
	}
	planned := false
	for _, message := range model.sideSaw {
		planned = planned || (message.Role == llm.RoleTool && strings.Contains(message.Content, "the users route goes first"))
	}
	if !planned {
		t.Errorf("the side call did not see the orchestrator's step before the spawn: %+v", model.sideSaw)
	}
	asked := model.sideSaw[len(model.sideSaw)-1].Content
	for _, want := range []string{"may I start the dev server?", "I start it on 3000", "add the users route"} {
		if !strings.Contains(asked, want) {
			t.Errorf("the side call was not told %q:\n%s", want, asked)
		}
	}
	if saw := subAgentSaw(t, model); !strings.Contains(saw, answer) || strings.Contains(saw, "assumed") {
		t.Errorf("the sub-agent's next step saw %q, want the answer %q and no assumption", saw, answer)
	}
}

func TestAFailedSideCallAnswersWithTheDefaultMarkedAssumed(t *testing.T) {
	model := askTurn(t, true, nil, claimDecision("started on 3000"))
	saw := subAgentSaw(t, model)
	if !strings.Contains(saw, "I start it on 3000") || !strings.Contains(saw, "assumed") {
		t.Errorf("after a failed side call the sub-agent saw %q, want its default marked assumed", saw)
	}
}
