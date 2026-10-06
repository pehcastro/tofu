package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/interface/tui"
	"tofu/interface/tui/subagent"
	"tofu/internal/llm"
	roster "tofu/internal/subagent"
)

func TestTheSubAgentViewShowsTheStepsASubAgentHasTakenWhileItIsStillRunning(t *testing.T) {
	dir := scratchProject(t)
	model := noteSubAgent(writeNote("call-2"))
	driver := driveApp(t)
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "hand the note to a sub-agent", driver.emit)

	var stepping []subagent.Row
	for _, event := range driver.of(tui.EventSubAgent) {
		for _, subAgent := range event.SubAgents {
			if subAgent.State == roster.Working && subAgent.Steps > 0 {
				stepping = append(stepping, subAgent)
			}
		}
	}
	if len(stepping) == 0 {
		t.Fatal("no sub-agent event carried a running sub-agent with a step behind it, so nothing can tell a sub-agent two steps in from one that has done nothing")
	}
	t.Logf("the running sub-agent was drawn %d times with the steps it had taken, first %+v", len(stepping), stepping[0])
}

func noteSubAgent(write llm.ToolCall) *queuedModel {
	spawnCall := llm.ToolCall{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)}
	return &queuedModel{
		decisions: []llm.Decision{
			{Build: "stub-model", Outcome: llm.OutcomeToolCalls, Content: "the sub-agent is on it", ToolCalls: []llm.ToolCall{spawnCall}},
			{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the sub-agent did it"},
		},
		subAgents: []llm.Decision{
			{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{write}},
			{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the sub-agent wrote it"},
		},
	}
}

func subAgentCallsWhileRunningAndOnceFinished(t *testing.T, dir, planted string) (running, finished []subagent.Call) {
	t.Helper()
	_ = os.Remove(filepath.Join(dir, "note.txt"))
	model := noteSubAgent(llm.ToolCall{ID: "call-2", Name: "write", Arguments: json.RawMessage(`{"path":"note.txt","content":"` + planted + `"}`)})
	driver := driveApp(t)
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "hand the note to a sub-agent", driver.emit)

	for _, event := range driver.of(tui.EventSubAgent) {
		for _, subAgent := range event.SubAgents {
			for _, call := range subAgent.Calls {
				if strings.Contains(call.Tool+call.Text+call.Result, planted) {
					t.Fatalf("a %s sub-agent drew %+v, which carries the argument the sub-agent was given", subAgent.State, call)
				}
			}
			if subAgent.State == roster.Working && len(subAgent.Calls) > 0 && running == nil {
				running = subAgent.Calls
			}
			if subAgent.State != roster.Working {
				finished = subAgent.Calls
			}
		}
	}
	return running, finished
}

func TestASubAgentsCallsGainTheirTextWhenItFinishesAndItsArgumentsNeverReachTheView(t *testing.T) {
	const planted = "sk-live-9f3a1c7e4b2d8a6f0e5c3b1a"
	dir := scratchProject(t)

	running, finished := subAgentCallsWhileRunningAndOnceFinished(t, dir, planted)
	if len(running) == 0 {
		t.Fatal("the sub-agent called write and no sub-agent event drew a call while it was still running")
	}
	if running[0].Tool != "write" || running[0].Text != "" {
		t.Fatalf("the running sub-agent drew %+v, want the bare name the roster holds", running[0])
	}
	if len(finished) == 0 || finished[0].Text == "" {
		t.Fatalf("the finished sub-agent drew %+v, want the spawner's list with the text each call produced", finished)
	}
	t.Logf("running draws %+v and finished draws %+v", running, finished)
}
