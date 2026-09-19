package state

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildStopCheckLeavesEveryListPresentAndEmpty(t *testing.T) {
	got, _, err := BuildStopCheck(StopCheckState{
		Task:        "write hello.txt",
		RecentSteps: []StopCheckStep{{Index: 1, AssistantText: "done"}},
	})
	if err != nil {
		t.Fatalf("BuildStopCheck: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("decode built state: %v", err)
	}
	steps, ok := decoded["recent_steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("recent_steps = %#v, want one step", decoded["recent_steps"])
	}
	step, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("recent_steps[0] = %#v, want an object", steps[0])
	}
	calls, present := step["tool_calls"]
	if !present {
		t.Fatal("tool_calls is absent on a step that called no tool, it must be present and empty")
	}
	list, ok := calls.([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("tool_calls = %#v, want an empty array", calls)
	}
	budget, present := decoded["budget"]
	if !present {
		t.Fatal("budget is absent, it must be present with every cap false")
	}
	if _, ok := budget.(map[string]any); !ok {
		t.Fatalf("budget = %#v, want an object", budget)
	}
}

func TestBuildStopCheckWithNoStepsStillCarriesTheField(t *testing.T) {
	got, _, err := BuildStopCheck(StopCheckState{Task: "say ok"})
	if err != nil {
		t.Fatalf("BuildStopCheck: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("decode built state: %v", err)
	}
	steps, present := decoded["recent_steps"]
	if !present {
		t.Fatal("recent_steps is absent before the first step, it must be present and empty")
	}
	list, ok := steps.([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("recent_steps = %#v, want an empty array", steps)
	}
}

func TestBuildStopCheckDoesNotWriteIntoTheCallersSteps(t *testing.T) {
	steps := []StopCheckStep{{Index: 1}}
	if _, _, err := BuildStopCheck(StopCheckState{Task: "say ok", RecentSteps: steps}); err != nil {
		t.Fatalf("BuildStopCheck: %v", err)
	}
	if steps[0].ToolCalls != nil {
		t.Fatalf("the builder filled the caller's step with %#v, it must leave the caller's value alone", steps[0].ToolCalls)
	}
}

func TestStopCheckVersionIsDottedByPointAndDiffersFromTheGate(t *testing.T) {
	_, version, err := BuildStopCheck(StopCheckState{Task: "say ok"})
	if err != nil {
		t.Fatalf("BuildStopCheck: %v", err)
	}
	if !strings.HasPrefix(version, StopCheckPoint+".") {
		t.Fatalf("version = %q, want prefix %q", version, StopCheckPoint+".")
	}
	if version == ToolGateVersion() {
		t.Fatalf("stop_check and tool_gate share the state builder version %q", version)
	}
}
