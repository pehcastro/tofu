package crew

import (
	"errors"
	"testing"
)

func TestRosterRefusesASecondHolderOfAnOverlappingPath(t *testing.T) {
	cases := []struct {
		name     string
		held     []string
		wanted   []string
		collides bool
	}{
		{"a_tree_and_one_file_inside_it", []string{"internal/crew/**"}, []string{"internal/crew/owns.go"}, true},
		{"one_file_inside_a_tree_and_the_tree", []string{"internal/crew/owns.go"}, []string{"internal/crew/**"}, true},
		{"the_same_glob_twice", []string{"internal/crew/**"}, []string{"internal/crew/**"}, true},
		{"two_trees_under_one_parent", []string{"internal/crew/**"}, []string{"internal/turn/**"}, false},
		{"a_segment_star_and_a_nested_file", []string{"internal/turn/*.go"}, []string{"internal/turn/tools/edit.go"}, false},
		{"a_segment_star_and_a_file_beside_it", []string{"internal/turn/*.go"}, []string{"internal/turn/spawn.go"}, true},
		{"two_files_in_one_directory", []string{"internal/turn/spawn.go"}, []string{"internal/turn/loop.go"}, false},
		{"a_tree_star_in_the_middle_and_a_matching_file", []string{"internal/**/edit.go"}, []string{"internal/turn/tools/edit.go"}, true},
		{"a_tree_star_in_the_middle_and_a_file_it_misses", []string{"internal/**/edit.go"}, []string{"internal/turn/tools/glob.go"}, false},
		{"case_does_not_hide_a_collision", []string{"Internal/Crew/**"}, []string{"internal/crew/owns.go"}, true},
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
	if err := roster.Hold(SubAgent{ID: "child-1", Owns: []string{"internal/crew/**"}}); err != nil {
		t.Fatal(err)
	}
	if err := roster.Hold(SubAgent{ID: "child-2", Owns: []string{"internal/turn/**", "internal/crew/owns.go"}}); err == nil {
		t.Fatal("expected the second hold to be refused")
	}
	if err := roster.Hold(SubAgent{ID: "child-3", Owns: []string{"internal/turn/**"}}); err != nil {
		t.Fatalf("the refused list left a partial hold behind: %v", err)
	}
}

func TestACollisionCarriesWhatTheHolderReported(t *testing.T) {
	roster := &Roster{}
	if err := roster.Hold(SubAgent{ID: "child-1", Owns: []string{"internal/crew/**"}}); err != nil {
		t.Fatal(err)
	}
	var before CollisionError
	if !errors.As(roster.Hold(SubAgent{ID: "child-2", Owns: []string{"internal/crew/owns.go"}}), &before) {
		t.Fatal("expected a collision")
	}
	if before.HolderReport != "" {
		t.Fatalf("a holder that has reported nothing has nothing to hand back: %q", before.HolderReport)
	}

	roster.Reached("child-1", InReview, "the roster now carries a report")
	var after CollisionError
	if !errors.As(roster.Hold(SubAgent{ID: "child-3", Owns: []string{"internal/crew/owns.go"}}), &after) {
		t.Fatal("expected a collision")
	}
	if after.HolderReport != "the roster now carries a report" {
		t.Fatalf("the collision does not carry the holder's report: %q", after.HolderReport)
	}
}

func TestRosterRefusesAGlobItCannotParse(t *testing.T) {
	roster := &Roster{}
	err := roster.Hold(SubAgent{ID: "child-1", Owns: []string{"internal/crew/??.go"}})
	var target UnparseableGlobError
	if !errors.As(err, &target) {
		t.Fatalf("got %v, want an UnparseableGlobError", err)
	}
}

func TestAHeldSubAgentStartsWorkingAndCarriesItsMissionAndBrief(t *testing.T) {
	roster := &Roster{}
	const brief = "BOJI-196: read the ticket, then the spec, then write the states"
	if err := roster.Hold(SubAgent{ID: "c1", Mission: "work on BOJI-196", Brief: brief, Owns: []string{"internal/crew/**"}}); err != nil {
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
	if len(named) != int(Finished)+1 {
		t.Fatalf("%d states are named and the enum runs to %d", len(named), int(Finished))
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
		if !isText || message != "crew: unknown sub-agent state 9" {
			t.Fatalf("rendering an unknown state gave %v, want a panic naming it", recovered)
		}
		t.Logf("panicked: %v", recovered)
	}()
	t.Log(State(9).String())
	t.Fatal("an unknown state rendered instead of failing")
}
