package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"tofu/interface/tui"
	"tofu/interface/tui/session"
	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/turn/tools"
)

func TestADryRunListsThePlanToolAndTheOffArmDoesNot(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "")
	var out, errOut bytes.Buffer
	if code := runVerb([]string{"--dir", t.TempDir(), "--dry-run", "write hello.txt"}, &out, &errOut); code != exitOK {
		t.Fatalf("tofu run --dry-run exited %d: %s", code, errOut.String())
	}
	var body struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("the printed request is not the request that would be sent: %v", err)
	}
	wanted := anthropic.ClaudeCodeToolPrefix + tools.PlanToolName
	offered := make([]string, 0, len(body.Tools))
	described := ""
	for _, tool := range body.Tools {
		offered = append(offered, tool.Name)
		if tool.Name == wanted {
			described = tool.Description
		}
	}
	if !slices.Contains(offered, wanted) {
		t.Fatalf("a dry run offers %v and none of them is the plan tool", offered)
	}
	if !strings.Contains(described, "set") || !strings.Contains(described, "start") {
		t.Fatalf("the plan tool is offered without saying how to move an item: %q", described)
	}
	if three := toolNames(t, armOpts(t, "--tools", toolSetThree)); slices.Contains(three, tools.PlanToolName) {
		t.Fatalf("the off arm must stay the three tools the recorded runs had: %v", three)
	}
	t.Logf("%d tools offered, including %s", len(offered), tools.PlanToolName)
}

func planCall(id, args string) llm.Decision {
	return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
		{ID: id, Name: tools.PlanToolName, Arguments: json.RawMessage(args)},
	}}
}

func lastPlan(events []tui.Event) ([]session.PlanItem, int) {
	var plan []session.PlanItem
	seen := 0
	for _, event := range events {
		if event.Kind != tui.EventPlan {
			continue
		}
		seen++
		plan = event.Plan
	}
	return plan, seen
}

func TestAPlanStatedDuringATurnReachesTheView(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{queued: []llm.Decision{
		planCall("call-1", `{"op":"set","items":[{"phase":"read","text":"read the gate"},{"phase":"write","text":"wire the plan tool"}]}`),
		planCall("call-2", `{"op":"start","item":"read the gate"}`),
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "stated"},
	}}
	var events eventLog
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "state a plan and start on it", events.add)

	plan, seen := lastPlan(events.all())
	if seen == 0 {
		t.Fatal("a turn that stated a plan emitted no plan the view could draw")
	}
	want := []session.PlanItem{
		{Phase: "read", Text: "read the gate", State: session.PlanRunning},
		{Phase: "write", Text: "wire the plan tool", State: session.PlanPending},
	}
	if !slices.Equal(plan, want) {
		t.Fatalf("the view was handed %+v, want %+v", plan, want)
	}
	t.Logf("%d plan events, the last one carrying %+v", seen, plan)
}

func TestATurnThatStatesNoPlanHandsTheViewNothingToDraw(t *testing.T) {
	dir := scratchProject(t)
	var events eventLog
	stubbedTurn(dir, noteThenStop())(t.Context(), onTheSubscription, "write the note", events.add)

	plan, seen := lastPlan(events.all())
	if seen == 0 {
		t.Fatal("no step reported at all, so this proves nothing about a turn without a plan")
	}
	if len(plan) != 0 {
		t.Fatalf("a turn that never called the plan tool handed the view %+v", plan)
	}
}

func TestEveryPlanStateTheToolHasIsDrawable(t *testing.T) {
	drawn := statedPlan([]tools.PlanItem{
		{Text: "pending", State: tools.PlanPending},
		{Text: "running", State: tools.PlanRunning},
		{Text: "done", State: tools.PlanDone},
		{Text: "dropped", State: tools.PlanDropped},
	})
	want := []session.PlanItem{
		{Text: "pending", State: session.PlanPending},
		{Text: "running", State: session.PlanRunning},
		{Text: "done", State: session.PlanDone},
		{Text: "dropped", State: session.PlanDropped},
	}
	if !slices.Equal(drawn, want) {
		t.Fatalf("the view is handed %+v, want %+v", drawn, want)
	}
}

func TestDrawnPlanStateNamesEveryToolPlanState(t *testing.T) {
	want := map[tools.PlanState]session.PlanState{
		tools.PlanPending: session.PlanPending,
		tools.PlanRunning: session.PlanRunning,
		tools.PlanDone:    session.PlanDone,
		tools.PlanDropped: session.PlanDropped,
	}
	for _, state := range tools.AllPlanStates() {
		expected, named := want[state]
		if !named {
			t.Fatalf("%s carries no expected drawn state, so a new plan state can reach drawnPlanState untested", state)
		}
		if got := drawnPlanState(state); got != expected {
			t.Fatalf("drawnPlanState(%s) = %v, want %v", state, got, expected)
		}
	}
}
