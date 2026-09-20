package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func runPlan(t *testing.T, tool *tools.Plan, args string) string {
	t.Helper()
	result, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("plan %s: %v", args, err)
	}
	return result.Content
}

const threeItems = `{"op":"set","items":[` +
	`{"phase":"read","text":"read the gate"},` +
	`{"phase":"write","text":"write the tool"},` +
	`{"phase":"write","text":"draw it in the session view"}]}`

func TestAPlanIsStatedThenOneItemRunsThenItIsDone(t *testing.T) {
	tool := tools.NewPlan()

	stated := runPlan(t, tool, threeItems)
	t.Logf("set returned:\n%s", stated)
	for _, want := range []string{"plan, 3 items", "read", "pending  read the gate", "pending  write the tool"} {
		if !strings.Contains(stated, want) {
			t.Fatalf("the stated plan does not carry %q:\n%s", want, stated)
		}
	}

	started := runPlan(t, tool, `{"op":"start","item":"write the tool"}`)
	t.Logf("start returned:\n%s", started)
	if !strings.Contains(started, "running  write the tool") {
		t.Fatalf("the started item is not running:\n%s", started)
	}
	if strings.Count(started, "running") != 1 {
		t.Fatalf("more than one item reads as running:\n%s", started)
	}

	finished := runPlan(t, tool, `{"op":"done","item":"write the tool"}`)
	t.Logf("done returned:\n%s", finished)
	if !strings.Contains(finished, "done  write the tool") {
		t.Fatalf("the finished item is not done:\n%s", finished)
	}
	if strings.Contains(finished, "running") {
		t.Fatalf("an item is still running after the only running one was finished:\n%s", finished)
	}

	held := tool.Items()
	if len(held) != 3 || held[1].Text != "write the tool" || held[1].State != tools.PlanDone {
		t.Fatalf("the plan the interface would be handed is %v", held)
	}
	if tool.Definition().Name != tool.Name() || tool.Name() != "plan" {
		t.Fatalf("the tool is offered as %q and defined as %q", tool.Name(), tool.Definition().Name)
	}
}

func TestASecondRunningItemIsRefusedAndNamesTheOneAlreadyRunning(t *testing.T) {
	tool := tools.NewPlan()
	runPlan(t, tool, threeItems)
	runPlan(t, tool, `{"op":"start","item":"read the gate"}`)

	_, err := tool.Run(context.Background(), json.RawMessage(`{"op":"start","item":"write the tool"}`))
	if err == nil {
		t.Fatal("a second item started while another was running")
	}
	t.Logf("the refusal reads: %v", err)
	for _, want := range []string{"read the gate", "one item runs at a time", "done", "drop"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not say %q: %v", want, err)
		}
	}
	if held := runPlan(t, tool, `{"op":"done","item":"read the gate"}`); strings.Contains(held, "running") {
		t.Fatalf("the refused start moved the plan anyway:\n%s", held)
	}
}

func TestAnItemIsAddressedByItsWordsRatherThanByANumber(t *testing.T) {
	tool := tools.NewPlan()
	runPlan(t, tool, threeItems)

	_, err := tool.Run(context.Background(), json.RawMessage(`{"op":"start","item":"2"}`))
	if err == nil {
		t.Fatal("an item answered to a number")
	}
	t.Logf("addressing by a number reads: %v", err)
	if !strings.Contains(err.Error(), "named by its words") || !strings.Contains(err.Error(), `"write the tool"`) {
		t.Fatalf("the refusal does not say how an item is named: %v", err)
	}

	started := runPlan(t, tool, `{"op":"start","item":"Draw It In The Session View"}`)
	if !strings.Contains(started, "running  draw it in the session view") {
		t.Fatalf("an item did not answer to its own words:\n%s", started)
	}
}

func planCall(id, args string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: "plan", Arguments: json.RawMessage(args)}
}

func TestTheRecordedSessionCarriesThePlanAsItStoodAtEachStep(t *testing.T) {
	row := runCalls(t, t.TempDir(), konst.TurnResultBytesCap, false,
		planCall("c1", threeItems),
		planCall("c2", `{"op":"start","item":"read the gate"}`),
		planCall("c3", `{"op":"done","item":"read the gate"}`),
		planCall("c4", `{"op":"start","item":"write the tool"}`),
		planCall("c5", `{"op":"start","item":"draw it in the session view"}`),
		planCall("c6", `{"op":"done","item":"write the tool"}`),
	)
	row.ID = "sess-plan"

	header, events, err := row.Record()
	if err != nil {
		t.Fatalf("recording the row: %v", err)
	}
	store := session.NewStore(t.TempDir())
	if err := store.Write(header, events); err != nil {
		t.Fatalf("writing the record: %v", err)
	}
	body, err := store.Body(row.ID)
	if err != nil {
		t.Fatalf("reading the record back: %v", err)
	}

	var steps []turn.StepRow
	for _, event := range body {
		if event.Kind != session.EventStep {
			continue
		}
		var step turn.StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatalf("reading step %d back: %v", len(steps), err)
		}
		steps = append(steps, step)
	}
	if len(steps) != len(row.Steps) {
		t.Fatalf("the record holds %d steps, want %d", len(steps), len(row.Steps))
	}

	over := tools.PlanOverSteps(steps)
	running := make([]string, 0, len(over))
	for index, stood := range over {
		if len(stood) != 3 {
			t.Fatalf("the plan at step %d holds %d items, want 3", index, len(stood))
		}
		at := "nothing"
		for _, item := range stood {
			if item.State == tools.PlanRunning {
				at = item.Text
			}
		}
		running = append(running, at)
		t.Logf("after step %d: %v", index, stood)
	}
	want := []string{"nothing", "read the gate", "nothing", "write the tool", "write the tool", "nothing", "nothing"}
	if len(running) != len(want) {
		t.Fatalf("the record holds %d plans, want %d", len(running), len(want))
	}
	for index := range want {
		if running[index] != want[index] {
			t.Fatalf("after step %d the plan was running %q, want %q", index, running[index], want[index])
		}
	}
	if over[2][0].State != tools.PlanDone || over[5][1].State != tools.PlanDone || over[5][2].State != tools.PlanPending {
		t.Fatalf("the plan at the end reads %v", over[5])
	}
	refused := ""
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			if call.Error != "" {
				refused = call.Error
			}
		}
	}
	if !strings.Contains(refused, "one item runs at a time") {
		t.Fatalf("the record does not carry the refused second start: %q", refused)
	}
}
