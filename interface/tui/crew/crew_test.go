package crew

import (
	"testing"

	roster "tofu/internal/crew"
)

func pairedStates() map[State]roster.State {
	return map[State]roster.State{
		Running:          roster.Working,
		WaitingForAnswer: roster.WaitingAnswer,
		HandedBack:       roster.InReview,
		Parked:           roster.Parked,
		Errored:          roster.Errored,
		Done:             roster.Finished,
	}
}

func TestTheCrewViewDrawsExactlyTheStatesTheRosterCanReach(t *testing.T) {
	drawn, reachable, paired := AllStates(), roster.States(), pairedStates()
	if len(drawn) != len(reachable) || len(paired) != len(drawn) {
		t.Fatalf("the crew view draws %d states, the roster reaches %d and %d are paired", len(drawn), len(reachable), len(paired))
	}
	reached := map[roster.State]bool{}
	for _, state := range drawn {
		held, pairs := paired[state]
		if !pairs {
			t.Fatalf("the crew view draws %q and no roster state is paired with it", state.Label())
		}
		reached[held] = true
		t.Logf("%s%s is the roster's %s", Mark(state), state.Label(), held)
	}
	for _, held := range reachable {
		if !reached[held] {
			t.Fatalf("the roster reaches %q and the crew view draws nothing for it", held)
		}
	}
}

func TestTheTwoNamesForOneStateAreTheSameWordsInADifferentSpelling(t *testing.T) {
	spelled := map[roster.State]string{
		roster.Working:       "working",
		roster.WaitingAnswer: "waiting for an answer",
		roster.InReview:      "in review",
		roster.Parked:        "parked",
		roster.Errored:       "errored",
		roster.Finished:      "finished",
	}
	for state, held := range pairedStates() {
		if state.Label() != spelled[held] {
			t.Fatalf("the roster's %s reads %q in the crew view, want %q", held, state.Label(), spelled[held])
		}
	}
}
