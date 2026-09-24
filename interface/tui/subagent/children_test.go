package subagent

import (
	"reflect"
	"testing"
	"time"

	"tofu/internal/konst"
	roster "tofu/internal/subagent"
)

const drawnChild = "turn-1-c1"

func holding(t *testing.T, started time.Time) *roster.Roster {
	t.Helper()
	held := &roster.Roster{}
	if err := held.Hold(roster.SubAgent{
		ID:      drawnChild,
		Mission: "read the policy loader",
		Owns:    []string{"internal/judge/policy/**"},
		Started: started,
	}); err != nil {
		t.Fatal(err)
	}
	return held
}

func TestAChildIsDrawnFromTheRosterRatherThanFromValuesTypedBesideIt(t *testing.T) {
	started := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	held := holding(t, started)
	held.Stepped(drawnChild, 2, started.Add(30*time.Second), "read")
	held.Reached(drawnChild, roster.InReview, "the loader reads the lock before the mode")

	drawn := Children(held.SubAgents(), started.Add(time.Minute), 0, map[string]int{drawnChild: 9400},
		func(agent roster.SubAgent) []Call { return []Call{{Tool: agent.Calling[0]}} })

	want := []Child{{
		Name:   "c1",
		Owns:   []string{"internal/judge/policy/**"},
		Doing:  "read the policy loader",
		Since:  30 * time.Second,
		Steps:  2,
		Total:  konst.TurnMaxSteps,
		Tokens: 9400,
		State:  HandedBack,
		Calls:  []Call{{Tool: "read"}},
		Report: "the loader reads the lock before the mode",
	}}
	if !reflect.DeepEqual(drawn, want) {
		t.Fatalf("the roster draws as %+v, want %+v", drawn, want)
	}
}

func TestARunningChildsClockRunsToNowAndAStoppedOneStopsAtItsLastStep(t *testing.T) {
	started := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	held := holding(t, started)
	held.Stepped(drawnChild, 1, started.Add(10*time.Second))
	now := started.Add(time.Minute)

	if running := Children(held.SubAgents(), now, 0, nil, nil)[0]; running.Since != time.Minute || running.State != Running {
		t.Fatalf("a working child reads %s in state %s, want 1m0s while running", running.Since, running.State.Label())
	}
	held.Reached(drawnChild, roster.Finished, "done")
	if stopped := Children(held.SubAgents(), now, 0, nil, nil)[0]; stopped.Since != 10*time.Second {
		t.Fatalf("a finished child reads %s, want the 10s it last moved at", stopped.Since)
	}
}
