package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/interface/tui"
	"tofu/interface/tui/subagent"
	"tofu/internal/konst"
	"tofu/internal/llm"
	roster "tofu/internal/subagent"
	"tofu/internal/turn"
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

func watching(held *roster.Roster, at time.Time) (*appWatcher, func() []subagent.Row) {
	var drawn []subagent.Row
	watch := &appWatcher{
		held: held,
		now:  func() time.Time { return at },
		emit: func(event tui.Event) { drawn = event.SubAgents },
	}
	return watch, func() []subagent.Row { return drawn }
}

func TestTheSubAgentsDrawnIsTheRosterItselfAndNotACopyBesideIt(t *testing.T) {
	start := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	held := rosterHolding(t,
		roster.SubAgent{ID: "turn-1-c1", Mission: "write the note", Owns: []string{"note.txt"}, Started: start},
		roster.SubAgent{ID: "turn-1-c2", Mission: "read the rules", Owns: []string{"library/**"}, Started: start},
	)
	watch, drawn := watching(held, start)

	watch.sendSubAgents()
	if len(drawn()) != len(held.SubAgents()) {
		t.Fatalf("the view drew %d sub-agents and the roster holds %d: the view is keeping a list of its own", len(drawn()), len(held.SubAgents()))
	}

	held.Stepped("turn-1-c2", 4, start.Add(time.Minute))
	held.Reached("turn-1-c1", roster.Parked, "the orchestrator ran out of context")
	watch.sendSubAgents()

	after := drawn()
	if len(after) != 2 {
		t.Fatalf("the view drew %d sub-agents, want the roster's two", len(after))
	}
	if after[0].State != roster.Parked || after[0].Report != "the orchestrator ran out of context" {
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

func TestARunningSubAgentDrawsTheToolItIsCallingRatherThanNoToolCallYet(t *testing.T) {
	start := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	held := rosterHolding(t, roster.SubAgent{ID: "turn-1-c1", Mission: "write the note", Owns: []string{"note.txt"}, Started: start})
	held.Stepped("turn-1-c1", 1, start.Add(time.Second), "read", "write")
	watch, drawn := watching(held, start)

	watch.sendSubAgents()
	var tools []string
	for _, call := range drawn()[0].Calls {
		tools = append(tools, call.Tool)
		if call.Text != "" || call.Result != "" {
			t.Errorf("the roster gave the view %+v, and it knows nothing but a tool name", call)
		}
	}
	if !slices.Equal(tools, []string{"read", "write"}) {
		t.Fatalf("the roster has held read and write since the sub-agent's first step and the view drew %v", tools)
	}
}

func TestTheSpawnersRecordedCallsWinOverTheRostersNamesWheneverItHasAny(t *testing.T) {
	recorded := []subagent.Call{{Tool: "write", Text: "write note.txt", Result: "no such directory"}}
	if got := recordedOrCalling(recorded, nil, []string{"read", "write"}, 0); !slices.Equal(got, recorded) {
		t.Fatalf("with both sources holding something the view drew %+v, want the spawner's %+v", got, recorded)
	}
	watched := recordedOrCalling(nil, nil, []string{"read", "write"}, 0)
	if !slices.Equal(watched, []subagent.Call{{Tool: "read"}, {Tool: "write"}}) {
		t.Fatalf("with only the roster holding names the view drew %+v", watched)
	}
	if len(recordedOrCalling(nil, nil, nil, 0)) != 0 {
		t.Fatal("neither source holds anything and the view was given a call")
	}
}

func TestACallOpenedAfterRecordedCallsKeepsItsRowAndItsStart(t *testing.T) {
	start := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	var asked, recorded []subagent.Call
	for index := range konst.SubAgentCallsWatched + 2 {
		id := "e" + strconv.Itoa(index)
		asked = append(asked, subagent.Call{ID: id, At: start.Add(time.Duration(index) * time.Second), Tool: "read"})
		recorded = append(recorded, subagent.Call{ID: id, Tool: "read", Text: "a.go", Result: "1 KB"})
	}
	open := subagent.Call{ID: "open", At: start.Add(time.Minute), Tool: "bash"}
	watched := recordedOrCalling(recorded, append(asked, open), nil, 0)
	if last := watched[len(watched)-1]; last != open {
		t.Fatalf("a bash call opened after %d recorded calls drew %+v last, want the open call with its start", len(recorded), last)
	}
	for _, call := range watched {
		if call.ID != "open" && call.ID != "" && call.Result == "" {
			t.Errorf("a recorded call came back open: %+v", call)
		}
	}
	if len(watched) != konst.SubAgentCallsWatched {
		t.Errorf("the pane holds %d calls, want %d", len(watched), konst.SubAgentCallsWatched)
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

func TestTheElapsedTimeIsTheSubAgentsOwnAndNotTheViewsClock(t *testing.T) {
	born := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	held := rosterHolding(t, roster.SubAgent{ID: "turn-1-c1", Mission: "work", Owns: []string{"x"}, Started: born})
	watch, drawn := watching(held, born.Add(90*time.Second))

	watch.sendSubAgents()
	if since := drawn()[0].Since; since != 90*time.Second {
		t.Errorf("the sub-agent started 90s before the view drew it and its row reads %s", since)
	}

	held.Stepped("turn-1-c1", 3, born.Add(30*time.Second))
	held.Reached("turn-1-c1", roster.Finished, "done")
	watch.sendSubAgents()
	if since := drawn()[0].Since; since != 30*time.Second {
		t.Errorf("the sub-agent last moved 30s after it started and the finished row reads %s", since)
	}
}

func recordedRow(id string, tools ...string) turn.Row {
	step := turn.StepRow{Index: 1}
	for _, tool := range tools {
		step.ToolCalls = append(step.ToolCalls, turn.ToolCallRow{Tool: tool, Command: "it"})
	}
	return turn.Row{ID: id, Steps: []turn.StepRow{step}}
}

func TestAFinishedSubAgentDrawsNoMoreCallsThanTheWatchPaneHoldsAndSaysHowManyItHid(t *testing.T) {
	ran := make([]string, konst.SubAgentCallsWatched*2)
	for index := range ran {
		ran[index] = "tool" + strconv.Itoa(index)
	}
	calls := recordedOrCalling(recordedCalls([]turn.Row{recordedRow("turn-1-c1", ran...)}, "turn-1-c1"), nil, nil, 0)

	if len(calls) != konst.SubAgentCallsWatched {
		t.Fatalf("a sub-agent that ran %d tools drew %d calls, want at most the %d the pane holds", len(ran), len(calls), konst.SubAgentCallsWatched)
	}
	if last := calls[len(calls)-1].Tool; last != ran[len(ran)-1] {
		t.Fatalf("the newest call is %q and the pane drew %q last: the cut kept the wrong end", ran[len(ran)-1], last)
	}
	hidden := len(ran) - konst.SubAgentCallsWatched + 1
	if want := strconv.Itoa(hidden) + earlierCallsHidden; calls[0].Tool != want {
		t.Fatalf("the first line reads %q, want %q so a person can tell a cut list from a whole one", calls[0].Tool, want)
	}
	t.Logf("%d recorded calls draw as %d lines, the first reading %q", len(ran), len(calls), calls[0].Tool)
}

func runningThroughCalls(t *testing.T, count int) ([]subagent.Row, []string) {
	t.Helper()
	start := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	held := rosterHolding(t, roster.SubAgent{ID: "turn-1-c1", Mission: "work", Owns: []string{"note.txt"}, Started: start})
	ran := make([]string, count)
	for index := range ran {
		ran[index] = "tool" + strconv.Itoa(index)
		held.Stepped("turn-1-c1", index+1, start.Add(time.Duration(index)*time.Second), ran[index])
	}
	watch, drawn := watching(held, start)
	watch.sendSubAgents()
	return drawn(), ran
}

func TestARunningSubAgentDrawsHowManyCallsItDroppedAndKeepsTheNewest(t *testing.T) {
	subAgents, ran := runningThroughCalls(t, konst.SubAgentCallsWatched*2)

	calls := subAgents[0].Calls
	if len(calls) != konst.SubAgentCallsWatched {
		t.Fatalf("a running sub-agent %d calls in drew %d lines, want the measured %d the pane holds", len(ran), len(calls), konst.SubAgentCallsWatched)
	}
	hidden := len(ran) - konst.SubAgentCallsWatched + 1
	if want := strconv.Itoa(hidden) + earlierCallsHidden; calls[0].Tool != want {
		t.Fatalf("the first line reads %q, want %q so a person can tell a cut list from a whole one", calls[0].Tool, want)
	}
}

func TestARunningSubAgentAndAFinishedOneHideTheSameCallsInTheSameWords(t *testing.T) {
	subAgents, ran := runningThroughCalls(t, konst.SubAgentCallsWatched*2)

	running := subAgents[0].Calls
	finished := recordedOrCalling(recordedCalls([]turn.Row{recordedRow("turn-1-c1", ran...)}, "turn-1-c1"), nil, nil, 0)
	if len(running) != len(finished) {
		t.Fatalf("the same %d calls draw %d lines while the sub-agent runs and %d once it stops", len(ran), len(running), len(finished))
	}
	for index := range running {
		if running[index].Tool != finished[index].Tool {
			t.Fatalf("line %d reads %q while the sub-agent runs and %q once it stops, so a person can tell which state it is in", index, running[index].Tool, finished[index].Tool)
		}
	}
}

func TestACompleteCallListIsDrawnWholeWithNoHiddenLine(t *testing.T) {
	calls := recordedCalls([]turn.Row{recordedRow("turn-1-c1", "read", "write")}, "turn-1-c1")
	if len(calls) != 2 || calls[0].Tool != "read" || calls[1].Text != "it" {
		t.Fatalf("two recorded calls drew %+v, want both whole and in order", calls)
	}
}
