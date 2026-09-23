package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/interface/tui"
	"tofu/interface/tui/crew"
	roster "tofu/internal/crew"
	"tofu/internal/llm"
)

func TestTheCrewViewShowsTheStepsAChildHasTakenWhileItIsStillRunning(t *testing.T) {
	dir := scratchProject(t)
	spawnCall := llm.ToolCall{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)}
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{spawnCall}},
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-2")}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child wrote it"},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child did it"},
	}}
	driver := driveApp(t)
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "hand the note to a child", driver.emit)

	var stepping []crew.Child
	for _, event := range driver.of(tui.EventCrew) {
		for _, child := range event.Children {
			if child.State == crew.Running && child.Steps > 0 {
				stepping = append(stepping, child)
			}
		}
	}
	if len(stepping) == 0 {
		t.Fatal("no crew event carried a running child with a step behind it, so nothing can tell a child two steps in from one that has done nothing")
	}
	t.Logf("the running child was drawn %d times with the steps it had taken, first %+v", len(stepping), stepping[0])
}

func rosterHolding(t *testing.T, agents ...roster.SubAgent) *roster.Roster {
	t.Helper()
	held := &roster.Roster{}
	for _, agent := range agents {
		if err := held.Hold(agent); err != nil {
			t.Fatalf("hold %s: %v", agent.ID, err)
		}
	}
	return held
}

func watching(held *roster.Roster, at time.Time) (*appWatcher, func() []crew.Child) {
	var drawn []crew.Child
	watch := &appWatcher{
		held: held,
		now:  func() time.Time { return at },
		emit: func(event tui.Event) { drawn = event.Children },
	}
	return watch, func() []crew.Child { return drawn }
}

func TestTheCrewDrawnIsTheRosterItselfAndNotACopyBesideIt(t *testing.T) {
	start := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	held := rosterHolding(t,
		roster.SubAgent{ID: "turn-1-c1", Mission: "write the note", Owns: []string{"note.txt"}, Started: start},
		roster.SubAgent{ID: "turn-1-c2", Mission: "read the rules", Owns: []string{"library/**"}, Started: start},
	)
	watch, drawn := watching(held, start)

	watch.sendCrew()
	if len(drawn()) != len(held.SubAgents()) {
		t.Fatalf("the view drew %d children and the roster holds %d: the view is keeping a list of its own", len(drawn()), len(held.SubAgents()))
	}

	held.Stepped("turn-1-c2", 4, start.Add(time.Minute))
	held.Reached("turn-1-c1", roster.Parked, "the parent ran out of context")
	watch.sendCrew()

	after := drawn()
	if len(after) != 2 {
		t.Fatalf("the view drew %d children, want the roster's two", len(after))
	}
	if after[0].State != crew.Parked || after[0].Report != "the parent ran out of context" {
		t.Errorf("c1 is %s in the roster and the view drew %+v", roster.Parked, after[0])
	}
	if after[1].Steps != 4 {
		t.Errorf("c2 has taken 4 steps in the roster and the view drew %d", after[1].Steps)
	}
	for index, agent := range held.SubAgents() {
		if after[index].Doing != agent.Mission || !slices.Equal(after[index].Owns, agent.Owns) {
			t.Errorf("row %d draws %+v and the roster holds %+v", index, after[index], agent)
		}
	}
}

func TestEveryStateTheRosterCanReachIsDrawnAsItsOwnMark(t *testing.T) {
	start := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	marks := map[string]roster.State{}
	for _, state := range roster.States() {
		held := rosterHolding(t, roster.SubAgent{ID: "turn-1-c1", Mission: "work", Owns: []string{"x"}, Started: start})
		held.Reached("turn-1-c1", state, "")
		watch, drawn := watching(held, start)
		watch.sendCrew()

		shown := drawn()[0].State
		mark := crew.Mark(shown) + shown.Label()
		if other, taken := marks[mark]; taken {
			t.Errorf("the roster's %s and %s are both drawn as %q, so a person cannot tell them apart", state, other, mark)
		}
		marks[mark] = state
	}
	if len(marks) != len(crew.AllStates()) {
		t.Fatalf("the roster reaches %d marks and the view draws %d states", len(marks), len(crew.AllStates()))
	}
	t.Logf("every roster state draws its own mark: %v", marks)
}

func TestARunningChildDrawsTheToolItIsCallingRatherThanNoToolCallYet(t *testing.T) {
	start := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	held := rosterHolding(t, roster.SubAgent{ID: "turn-1-c1", Mission: "write the note", Owns: []string{"note.txt"}, Started: start})
	held.Stepped("turn-1-c1", 1, start.Add(time.Second), "read", "write")
	watch, drawn := watching(held, start)

	watch.sendCrew()
	var tools []string
	for _, call := range drawn()[0].Calls {
		tools = append(tools, call.Tool)
		if call.Text != "" || call.Result != "" {
			t.Errorf("the roster gave the view %+v, and it knows nothing but a tool name", call)
		}
	}
	if !slices.Equal(tools, []string{"read", "write"}) {
		t.Fatalf("the roster has held read and write since the child's first step and the view drew %v", tools)
	}
	view := crew.Model{Children: drawn()}
	view.SetSize(100, 24)
	view.Key("down")
	if pane := view.View(); strings.Contains(pane, "no tool call yet") {
		t.Fatalf("a child two tools in reads as quiet:\n%s", pane)
	}
}

func TestTheSpawnersRecordedCallsWinOverTheRostersNamesWheneverItHasAny(t *testing.T) {
	recorded := []crew.Call{{Tool: "write", Text: "write note.txt", Result: "no such directory"}}
	if got := recordedOrCalling(recorded, []string{"read", "write"}); !slices.Equal(got, recorded) {
		t.Fatalf("with both sources holding something the view drew %+v, want the spawner's %+v", got, recorded)
	}
	watched := recordedOrCalling(nil, []string{"read", "write"})
	if !slices.Equal(watched, []crew.Call{{Tool: "read"}, {Tool: "write"}}) {
		t.Fatalf("with only the roster holding names the view drew %+v", watched)
	}
	if len(recordedOrCalling(nil, nil)) != 0 {
		t.Fatal("neither source holds anything and the view was given a call")
	}
}

func TestAChildsCallsGainTheirTextWhenItFinishesAndItsArgumentsNeverReachTheView(t *testing.T) {
	const planted = "sk-live-9f3a1c7e4b2d8a6f0e5c3b1a"
	dir := scratchProject(t)
	spawnCall := llm.ToolCall{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)}
	secret := llm.ToolCall{ID: "call-2", Name: "write", Arguments: json.RawMessage(`{"path":"note.txt","content":"` + planted + `"}`)}
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{spawnCall}},
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{secret}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child wrote it"},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child did it"},
	}}
	driver := driveApp(t)
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "hand the note to a child", driver.emit)

	var running, finished []crew.Call
	for _, event := range driver.of(tui.EventCrew) {
		for _, child := range event.Children {
			for _, call := range child.Calls {
				if strings.Contains(call.Tool+call.Text+call.Result, planted) {
					t.Fatalf("a %s child drew %+v, which carries the argument the child was given", child.State.Label(), call)
				}
			}
			if child.State == crew.Running && len(child.Calls) > 0 && running == nil {
				running = child.Calls
			}
			if child.State != crew.Running {
				finished = child.Calls
			}
		}
	}
	if len(running) == 0 {
		t.Fatal("the child called write and no crew event drew a call while it was still running")
	}
	if running[0].Tool != "write" || running[0].Text != "" {
		t.Fatalf("the running child drew %+v, want the bare name the roster holds", running[0])
	}
	if len(finished) == 0 || finished[0].Text == "" {
		t.Fatalf("the finished child drew %+v, want the spawner's list with the text each call produced", finished)
	}
	t.Logf("running draws %+v and finished draws %+v", running, finished)
}

func TestTheElapsedTimeIsTheChildsOwnAndNotTheViewsClock(t *testing.T) {
	born := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	held := rosterHolding(t, roster.SubAgent{ID: "turn-1-c1", Mission: "work", Owns: []string{"x"}, Started: born})
	watch, drawn := watching(held, born.Add(90*time.Second))

	watch.sendCrew()
	if since := drawn()[0].Since; since != 90*time.Second {
		t.Errorf("the child started 90s before the view drew it and its row reads %s", since)
	}

	held.Stepped("turn-1-c1", 3, born.Add(30*time.Second))
	held.Reached("turn-1-c1", roster.Finished, "done")
	watch.sendCrew()
	if since := drawn()[0].Since; since != 30*time.Second {
		t.Errorf("the child last moved 30s after it started and the finished row reads %s", since)
	}
}
