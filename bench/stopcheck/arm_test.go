package stopcheck

import (
	"testing"

	"boji/internal/konst"
)

func turnOf(commands ...[]string) Turn {
	turn := Turn{ID: "turn-fixture", Task: "do the thing"}
	for i, step := range commands {
		converted := Step{Index: i + 1}
		for _, command := range step {
			converted.Calls = append(converted.Calls, Call{Tool: "bash", Command: command})
		}
		turn.Steps = append(turn.Steps, converted)
	}
	return turn
}

func TestCheapArmStopsWhenTheStepCalledNoTool(t *testing.T) {
	turn := turnOf([]string{"ls"}, nil)
	turn.Steps[1].AssistantText = "done"

	if got := CheapArm(turn, 0); got.Answer != Continue {
		t.Errorf("step 1 = %+v, want continue", got)
	}
	got := CheapArm(turn, 1)
	if got.Answer != Stop {
		t.Fatalf("step 2 = %+v, want stop", got)
	}
	t.Logf("step 2: %s, because %s", got.Answer, got.Rule)
}

func TestCheapArmStopsOnAStepThatOnlyRunsCommandsItAlreadyRan(t *testing.T) {
	turn := turnOf([]string{"bun --version; ls node_modules"}, []string{"bun --version"})
	got := CheapArm(turn, 1)
	if got.Answer != Stop {
		t.Fatalf("a step rerunning bun --version = %+v, want stop", got)
	}
	t.Logf("step 2: %s, because %s", got.Answer, got.Rule)
}

func TestCheapArmMissesARepeatWrittenWithADifferentCommand(t *testing.T) {
	turn := turnOf([]string{"ls node_modules | head"}, []string{"dir node_modules"})
	got := CheapArm(turn, 1)
	if got.Answer != Continue {
		t.Fatalf("dir node_modules after ls node_modules = %+v; if the cheap arm now catches this, the report's named disagreement is stale", got)
	}
	t.Logf("the cheap arm reads the same listing run twice as new work: %s, because %s", got.Answer, got.Rule)
}

func TestCheapArmStopsAtTheLoopsOwnStepCap(t *testing.T) {
	turn := turnOf([]string{"ls"})
	turn.Steps[0].Index = konst.TurnMaxSteps
	got := CheapArm(turn, 0)
	if got.Answer != Stop {
		t.Fatalf("step %d = %+v, want stop at the cap", konst.TurnMaxSteps, got)
	}
}

func TestStateAtCarriesEveryStepSoFarAndMarksTheRepeat(t *testing.T) {
	turn := turnOf([]string{"bun --version; ls"}, []string{"bun --version"})
	built := StateAt(turn, 1)

	if len(built.RecentSteps) != 2 {
		t.Fatalf("state carries %d steps, want both steps so far", len(built.RecentSteps))
	}
	if built.RecentSteps[0].RepeatsEarlierStep {
		t.Error("the first step is marked as repeating something earlier")
	}
	if !built.RecentSteps[1].RepeatsEarlierStep {
		t.Error("the second step reran bun --version and is not marked as repeating earlier work")
	}
	if built.Budget.AtStepCap || built.Budget.AtDecisionCap || built.Budget.AtWallClockCap {
		t.Errorf("budget = %+v, want every cap unreached on a two step turn", built.Budget)
	}
	if built.Task != turn.Task {
		t.Errorf("task = %q, want %q", built.Task, turn.Task)
	}
}

func TestTheRecordedHonoTurnIsWhereTheArmsPartCompany(t *testing.T) {
	turns, _, err := ReadSessions(sessionsDir)
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	var hono Turn
	for _, turn := range turns {
		if turn.ID == "turn-18d6913a528f4528" {
			hono = turn
		}
	}
	if len(hono.Steps) < 10 {
		t.Skipf("turn-18d6913a528f4528 is no longer in %s, so this evidence cannot be rechecked", sessionsDir)
	}
	ninth, tenth := CheapArm(hono, 8), CheapArm(hono, 9)
	t.Logf("step 9 (%q): cheap arm says %s, because %s", hono.Steps[8].Calls[0].Command, ninth.Answer, ninth.Rule)
	t.Logf("step 10 (%q): cheap arm says %s, because %s", hono.Steps[9].Calls[0].Command, tenth.Answer, tenth.Rule)
	if ninth.Answer != Stop {
		t.Errorf("step 9 reran a command from step 8 and the cheap arm said %s", ninth.Answer)
	}
	if tenth.Answer != Continue {
		t.Errorf("step 10 = %s, the report names it as the repeat the cheap arm cannot see", tenth.Answer)
	}
}
