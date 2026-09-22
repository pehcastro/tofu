package harness

import "testing"

func rowUnder(setup Setup, run int, gates []GateResult) Row {
	return Row{
		Arm: ArmTofu, Task: "hono-routes", Version: 2, Run: run,
		Setup:          setup,
		CredentialKind: CredentialKindSubscription,
		WallClockMS:    100000,
		Turns:          4,
		Gates:          gates,
		Checklist:      checklistFullPass(),
	}
}

func runsPassing(passing, failing int) []Row {
	var rows []Row
	for i := 0; i < passing; i++ {
		rows = append(rows, rowUnder(Setup{Name: "stock"}, i+1, gatesOK()))
	}
	for i := 0; i < failing; i++ {
		rows = append(rows, rowUnder(Setup{Name: "stock"}, passing+i+1, gatesFailing("test")))
	}
	return rows
}

func TestAnOutcomeIsATierOverRepeatedRunsRatherThanAPassOrAFail(t *testing.T) {
	for _, c := range []struct {
		passing, failing int
		want             Tier
	}{
		{3, 0, TierAlwaysPasses},
		{5, 1, TierUsuallyPasses},
		{2, 1, TierUsuallyPasses},
		{1, 1, TierUsuallyFails},
		{1, 3, TierUsuallyFails},
		{0, 3, TierUsuallyFails},
	} {
		rows := runsPassing(c.passing, c.failing)
		if got := TierOf(rows); got != c.want {
			t.Errorf("%d passing of %d: TierOf = %s, want %s", c.passing, len(rows), got, c.want)
		}
		if got := PassCount(rows); got != c.passing {
			t.Errorf("%d passing of %d: PassCount = %d", c.passing, len(rows), got)
		}
	}
}

func TestARuleThatFiresOnMostRunsIsNotTheSameOutcomeAsOneThatAlwaysFires(t *testing.T) {
	always := TierOf(runsPassing(5, 0))
	usually := TierOf(runsPassing(4, 1))
	if always == usually {
		t.Fatalf("five of five and four of five both read %s, so a rule that fails to fire once in five is hidden", always)
	}
}

func TestAChecklistMissIsAFailedRunEvenWhenEveryGatePassed(t *testing.T) {
	row := rowUnder(Setup{Name: "stock"}, 1, gatesOK())
	row.Checklist = []ChecklistResult{{Item: "returns 400 on missing field", Passed: false}}
	if Passed(row) {
		t.Fatal("a run that missed a checklist item counted as a pass")
	}
}
