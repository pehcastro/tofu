package subagent

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
)

const childSteps = 200

func holdingOneChild(t *testing.T) (*Roster, time.Time) {
	t.Helper()
	start := time.Unix(1758585600, 0)
	roster := &Roster{}
	if err := roster.Hold(SubAgent{ID: "c1", Owns: []string{"internal/subagent/**"}, Started: start}); err != nil {
		t.Fatal(err)
	}
	return roster, start
}

func TestRosterRefusesASecondHolderOfAnOverlappingPath(t *testing.T) {
	cases := []struct {
		name     string
		held     []string
		wanted   []string
		collides bool
	}{
		{"a_tree_and_one_file_inside_it", []string{"internal/subagent/**"}, []string{"internal/subagent/owns.go"}, true},
		{"one_file_inside_a_tree_and_the_tree", []string{"internal/subagent/owns.go"}, []string{"internal/subagent/**"}, true},
		{"the_same_glob_twice", []string{"internal/subagent/**"}, []string{"internal/subagent/**"}, true},
		{"two_trees_under_one_parent", []string{"internal/subagent/**"}, []string{"internal/turn/**"}, false},
		{"a_segment_star_and_a_nested_file", []string{"internal/turn/*.go"}, []string{"internal/turn/tools/edit.go"}, false},
		{"a_segment_star_and_a_file_beside_it", []string{"internal/turn/*.go"}, []string{"internal/turn/spawn.go"}, true},
		{"two_files_in_one_directory", []string{"internal/turn/spawn.go"}, []string{"internal/turn/loop.go"}, false},
		{"a_tree_star_in_the_middle_and_a_matching_file", []string{"internal/**/edit.go"}, []string{"internal/turn/tools/edit.go"}, true},
		{"a_tree_star_in_the_middle_and_a_file_it_misses", []string{"internal/**/edit.go"}, []string{"internal/turn/tools/glob.go"}, false},
		{"case_does_not_hide_a_collision", []string{"Internal/Subagent/**"}, []string{"internal/subagent/owns.go"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			roster := &Roster{}
			if err := roster.Hold(SubAgent{ID: "child-1", Owns: c.held}); err != nil {
				t.Fatalf("the first hold failed: %v", err)
			}
			err := roster.Hold(SubAgent{ID: "child-2", Owns: c.wanted})
			if c.collides != (err != nil) {
				t.Fatalf("holding %v against %v gave %v, wanted a collision: %v", c.wanted, c.held, err, c.collides)
			}
			if !c.collides {
				return
			}
			var collision CollisionError
			if !errors.As(err, &collision) {
				t.Fatalf("got %v, want a CollisionError", err)
			}
			if collision.Child != "child-2" || collision.Holder != "child-1" {
				t.Fatalf("the collision does not name both children: %+v", collision)
			}
		})
	}
}

func TestRosterHoldsNothingWhenOneGlobOfTheListCollides(t *testing.T) {
	roster := &Roster{}
	if err := roster.Hold(SubAgent{ID: "child-1", Owns: []string{"internal/subagent/**"}}); err != nil {
		t.Fatal(err)
	}
	if err := roster.Hold(SubAgent{ID: "child-2", Owns: []string{"internal/turn/**", "internal/subagent/owns.go"}}); err == nil {
		t.Fatal("expected the second hold to be refused")
	}
	if err := roster.Hold(SubAgent{ID: "child-3", Owns: []string{"internal/turn/**"}}); err != nil {
		t.Fatalf("the refused list left a partial hold behind: %v", err)
	}
}

func TestACollisionCarriesWhatTheHolderReported(t *testing.T) {
	roster := &Roster{}
	if err := roster.Hold(SubAgent{ID: "child-1", Owns: []string{"internal/subagent/**"}}); err != nil {
		t.Fatal(err)
	}
	var before CollisionError
	if !errors.As(roster.Hold(SubAgent{ID: "child-2", Owns: []string{"internal/subagent/owns.go"}}), &before) {
		t.Fatal("expected a collision")
	}
	if before.HolderReport != "" {
		t.Fatalf("a holder that has reported nothing has nothing to hand back: %q", before.HolderReport)
	}

	roster.Reached("child-1", InReview, "the roster now carries a report")
	var after CollisionError
	if !errors.As(roster.Hold(SubAgent{ID: "child-3", Owns: []string{"internal/subagent/owns.go"}}), &after) {
		t.Fatal("expected a collision")
	}
	if after.HolderReport != "the roster now carries a report" {
		t.Fatalf("the collision does not carry the holder's report: %q", after.HolderReport)
	}
}

func TestRosterRefusesAGlobItCannotParse(t *testing.T) {
	roster := &Roster{}
	err := roster.Hold(SubAgent{ID: "child-1", Owns: []string{"internal/subagent/??.go"}})
	var target UnparseableGlobError
	if !errors.As(err, &target) {
		t.Fatalf("got %v, want an UnparseableGlobError", err)
	}
}

func TestAHeldSubAgentStartsWorkingAndCarriesItsMissionAndBrief(t *testing.T) {
	roster := &Roster{}
	const brief = "BOJI-196: read the ticket, then the spec, then write the states"
	if err := roster.Hold(SubAgent{ID: "c1", Mission: "work on BOJI-196", Brief: brief, Owns: []string{"internal/subagent/**"}}); err != nil {
		t.Fatal(err)
	}
	held := roster.SubAgents()
	if len(held) != 1 {
		t.Fatalf("the roster holds %d sub-agents, want 1", len(held))
	}
	if held[0].State != Working {
		t.Fatalf("a sub-agent that was just held reads as %s, want working", held[0].State)
	}
	if held[0].Mission != "work on BOJI-196" || held[0].Brief != brief {
		t.Fatalf("the roster lost the mission or the brief: %+v", held[0])
	}
}

func TestEveryStateHasAName(t *testing.T) {
	named := map[State]string{
		Working:       "working",
		WaitingAnswer: "waiting_answer",
		InReview:      "in_review",
		Parked:        "parked",
		Errored:       "errored",
		Finished:      "finished",
	}
	if len(named) != len(States()) || len(named) != int(Finished)+1 {
		t.Fatalf("%d states are named, States() lists %d and the enum runs to %d", len(named), len(States()), int(Finished))
	}
	for state, want := range named {
		if state.String() != want {
			t.Fatalf("state %d reads %q, want %q", int(state), state.String(), want)
		}
	}
}

func TestAStateThisBuildDoesNotKnowFailsRatherThanRendering(t *testing.T) {
	defer func() {
		recovered := recover()
		message, isText := recovered.(string)
		if !isText || message != "subagent: unknown sub-agent state 9" {
			t.Fatalf("rendering an unknown state gave %v, want a panic naming it", recovered)
		}
		t.Logf("panicked: %v", recovered)
	}()
	t.Log(State(9).String())
	t.Fatal("an unknown state rendered instead of failing")
}

func TestARunningChildCarriesWhenItStartedAndWhenItLastStepped(t *testing.T) {
	roster, start := holdingOneChild(t)
	if held := roster.SubAgents()[0]; !held.Active.Equal(start) {
		t.Fatalf("a child that has taken no step reads as last active at %v, want its start %v", held.Active, start)
	}

	roster.Stepped("c1", 3, start.Add(90*time.Second))

	held := roster.SubAgents()[0]
	if held.State != Working {
		t.Fatalf("the child reads as %s, so it is not running and this proves nothing", held.State)
	}
	if !held.Started.Equal(start) || !held.Active.Equal(start.Add(90*time.Second)) || held.Steps != 3 {
		t.Fatalf("a running child does not carry its start, its last step and its step count: %+v", held)
	}
}

func TestOnlyAStepMovesTheLastActivityOfARunningChild(t *testing.T) {
	roster, start := holdingOneChild(t)
	roster.Stepped("c1", 1, start.Add(time.Minute))
	if err := roster.Hold(SubAgent{ID: "c2", Owns: []string{"internal/turn/**"}, Started: start.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	roster.Stepped("c2", 1, start.Add(2*time.Hour))
	roster.Reached("c2", Finished, "c2 reported")
	if held := roster.SubAgents()[0]; !held.Active.Equal(start.Add(time.Minute)) || held.Steps != 1 {
		t.Fatalf("another child working or reporting moved this one: %+v", held)
	}
}

func TestARosterCanReachEveryStateTheSubAgentViewDraws(t *testing.T) {
	for _, state := range States() {
		t.Run(state.String(), func(t *testing.T) {
			roster, start := holdingOneChild(t)
			roster.Stepped("c1", 1, start.Add(time.Second))
			if state != Working {
				roster.Reached("c1", state, "the child reported")
			}
			held := roster.SubAgents()[0]
			if held.State != state {
				t.Fatalf("the roster reads %s, want %s", held.State, state)
			}
			if !held.Started.Equal(start) || !held.Active.Equal(start.Add(time.Second)) {
				t.Fatalf("a child in %s lost its start or its last activity: %+v", state, held)
			}
		})
	}
}

func TestARunningChildCarriesTheToolNamesOfItsLatestStep(t *testing.T) {
	roster, start := holdingOneChild(t)

	roster.Stepped("c1", 1, start.Add(time.Second), "read", "bash")

	held := roster.SubAgents()[0]
	if held.State != Working {
		t.Fatalf("the child reads as %s, so it is not running and this proves nothing", held.State)
	}
	if !slices.Equal(held.Calling, []string{"read", "bash"}) {
		t.Fatalf("a running child carries %v, want the names of the two calls its step made", held.Calling)
	}
}

func steppingOncePerCall(t *testing.T, calls int) (SubAgent, []string) {
	t.Helper()
	roster, start := holdingOneChild(t)
	called := make([]string, calls)
	for step := 1; step <= calls; step++ {
		called[step-1] = "tool" + strconv.Itoa(step)
		roster.Stepped("c1", step, start.Add(time.Duration(step)*time.Second), called[step-1])
	}
	return roster.SubAgents()[0], called
}

func TestAtTheBoundARunningChildKeepsItsNewestCallsAndSaysHowManyItDropped(t *testing.T) {
	held, called := steppingOncePerCall(t, konst.SubAgentCallsWatched+3)

	kept := konst.SubAgentCallsWatched - 1
	if want := called[len(called)-kept:]; !slices.Equal(held.Calling, want) {
		t.Fatalf("after %d calls the child carries %v, want the newest %d, %v", len(called), held.Calling, kept, want)
	}
	if want := len(called) - kept; held.CallsDropped != want {
		t.Fatalf("the child dropped %d of %d calls and reports %d", want, len(called), held.CallsDropped)
	}
	if rows := len(held.Calling) + 1; rows != konst.SubAgentCallsWatched {
		t.Fatalf("the calls plus the one line reporting the drop come to %d rows, want the measured %d", rows, konst.SubAgentCallsWatched)
	}
}

func TestAChildUnderTheBoundDropsNothing(t *testing.T) {
	held, called := steppingOncePerCall(t, konst.SubAgentCallsWatched)

	if !slices.Equal(held.Calling, called) {
		t.Fatalf("a child at exactly the bound carries %v, want all %d of %v", held.Calling, len(called), called)
	}
	if held.CallsDropped != 0 {
		t.Fatalf("a child at exactly the bound reports %d dropped calls, want none", held.CallsDropped)
	}
}

func TestAPersonCannotTellARunningChildsHiddenCountFromAFinishedOnes(t *testing.T) {
	held, called := steppingOncePerCall(t, konst.SubAgentCallsWatched*2)

	finishedHidden := len(called) - konst.SubAgentCallsWatched + 1
	finishedDrawn := konst.SubAgentCallsWatched - 1
	if held.CallsDropped != finishedHidden {
		t.Fatalf("the same %d calls leave a running child with %d hidden and a finished one with %d, so the pane changes its sentence the moment the child stops", len(called), held.CallsDropped, finishedHidden)
	}
	if len(held.Calling) != finishedDrawn {
		t.Fatalf("a running child offers %d call names and a finished one %d, so the cut lands somewhere else", len(held.Calling), finishedDrawn)
	}
}

func TestAStepThatCalledNothingLeavesTheEarlierCallsAlone(t *testing.T) {
	roster, start := holdingOneChild(t)
	roster.Stepped("c1", 1, start.Add(time.Second), "read")
	roster.Stepped("c1", 2, start.Add(2*time.Second))
	if held := roster.SubAgents()[0]; !slices.Equal(held.Calling, []string{"read"}) {
		t.Fatalf("a step with no tool call left %v behind, want the one call before it", held.Calling)
	}
}

func TestReadingTheCallsOfAChildThatKeepsSteppingIsNotARace(t *testing.T) {
	roster, start := holdingOneChild(t)
	stepped := make(chan struct{})
	go func() {
		for step := 1; step <= childSteps; step++ {
			roster.Stepped("c1", step, start.Add(time.Duration(step)*time.Second), "tool"+strconv.Itoa(step))
		}
		close(stepped)
	}()
	for range childSteps {
		for _, name := range roster.SubAgents()[0].Calling {
			if !strings.HasPrefix(name, "tool") {
				t.Errorf("a call read while the child stepped reads %q", name)
			}
		}
	}
	<-stepped
}

func TestReadingTheRosterWhileAChildStepsIsNotARace(t *testing.T) {
	roster, start := holdingOneChild(t)
	stepped := make(chan struct{})
	go func() {
		for step := 1; step <= childSteps; step++ {
			roster.Stepped("c1", step, start.Add(time.Duration(step)*time.Second))
		}
		close(stepped)
	}()
	for range childSteps {
		if held := roster.SubAgents()[0]; held.State != Working {
			t.Errorf("the child reads as %s while it is still stepping", held.State)
		}
	}
	<-stepped
}
