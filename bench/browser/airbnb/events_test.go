package airbnb

import (
	"slices"
	"strings"
	"testing"
)

func TestAnArmThatSpawnedAgainstItsDriverReadsMixedAndTheCredentialComesFromTheSpawn(t *testing.T) {
	for arm, mixed := range map[Arm]bool{ArmA: true, ArmB1: false, ArmC: true} {
		run, err := RunFromEvents(arm, "testdata/events/b1.jsonl")
		if err == nil {
			err = arm.Stamp(&run, "2026-09-29", "test")
		}
		if err != nil {
			t.Fatal(err)
		}
		table, err := Render([]Row{Score(Task{}, run)})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("arm %s:\n%s", arm, table)
		if run.Mixed != mixed || strings.Contains(table, "| mixed |") != mixed {
			t.Errorf("arm %s, which spawned browser, reads mixed = %v in the run and %v in the table, want %v", arm, run.Mixed, strings.Contains(table, "| mixed |"), mixed)
		}
		if run.Conditions.BrowserCredential != "subscription" {
			t.Errorf("arm %s: the browser turns ran on claude-sub/claude-haiku-5, and the row says %q", arm, run.Conditions.BrowserCredential)
		}
	}
}

func TestAForkedSessionIsReadFromItsRootThroughEveryFork(t *testing.T) {
	paths, err := Lineage("testdata/forked")
	if err != nil {
		t.Fatal(err)
	}
	run, err := RunFromEvents(ArmA, paths...)
	if err != nil {
		t.Fatal(err)
	}
	wantVisits := []string{
		"https://www.google.com/search?q=airbnb",
		"https://www.airbnb.com.br/",
		"https://www.airbnb.com.br/s/Atibaia--SP/homes?query=Atibaia%2C%20SP&checkin=2026-10-09&checkout=2026-10-15&adults=2&room_types%5B%5D=Entire%20home%2Fapt&price_max=4000&display_total_price=true&min_bedrooms=2&amenities%5B%5D=7",
		"https://www.airbnb.com.br/rooms/10000001",
		"https://www.airbnb.com.br/rooms/10000002",
		"https://www.airbnb.com.br/rooms/10000003",
	}
	if !slices.Equal(run.Visits, wantVisits) {
		t.Errorf("visits %q, want %q", run.Visits, wantVisits)
	}
	if run.WallMS != 300000 || run.Forks != 2 || run.MainTokens != 6600 {
		t.Errorf("wall %d ms, %d forks, %d main tokens, want 300000, 2 and 6600", run.WallMS, run.Forks, run.MainTokens)
	}
	task, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	table, err := Render([]Row{Score(task, run)})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + table)
	if !strings.Contains(table, "| forks main+sub |") || !strings.Contains(table, "| A | as set | 12 of 12 | 300 s | 6600 | - | 2+0 |") {
		t.Error("the row does not carry the whole lineage")
	}
}

func TestARunSplitsItsWallIntoModelBrowserAndOtherTime(t *testing.T) {
	run, err := RunFromEvents(ArmA, "testdata/events/timing.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	phases := run.Phases
	if run.WallMS != 60000 || phases.ModelMS != 15000 || phases.BrowserMS != 10000 || phases.OtherMS() != 35000 || phases.Steps != 2 {
		t.Errorf("wall %d, model %d, browser %d, other %d ms over %d steps, want 60000, 15000, 10000, 35000 over 2", run.WallMS, phases.ModelMS, phases.BrowserMS, phases.OtherMS(), phases.Steps)
	}
	if phases.ModelMedianMS != 7500 || phases.BrowserMedianMS != 5000 {
		t.Errorf("median model %d and browser %d ms, want 7500 and 5000", phases.ModelMedianMS, phases.BrowserMedianMS)
	}
	table, err := Render([]Row{Score(Task{}, run)})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + table)
	if !strings.Contains(table, "| model s | browser s | other s | steps |") || !strings.Contains(table, "| 15 (7.5) | 10 (5.0) | 35 | 2 |") {
		t.Error("the row does not carry the phase columns")
	}
}

func TestEachWireCountsItsCacheReadsOnceAndSubAgentForksAreCounted(t *testing.T) {
	run, err := RunFromEvents(ArmB1, "testdata/events/meta.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if run.BrowserTokens == nil {
		t.Fatal("a sub-agent arm recorded no browser tokens")
	}
	if run.MainTokens != 1550 || *run.BrowserTokens != 3300 || run.Forks != 0 || run.SubForks != 1 {
		t.Errorf("main %d, browser %d, forks %d+%d, want 1550, 3300 and 0+1", run.MainTokens, *run.BrowserTokens, run.Forks, run.SubForks)
	}
	table, err := Render([]Row{Score(Task{}, run)})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + table)
	if !strings.Contains(table, "| 4850 | - | 0+1 | 1550 | 3300 |") {
		t.Error("the row does not print the total and both fork counts")
	}
}

func TestAGoalArmReadsEveryPageBrowserDoAnswered(t *testing.T) {
	run, err := RunFromEvents(ArmC, "testdata/events/c.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Visits) != 5 || snapshotURL(run.Snapshot) != "https://www.airbnb.com.br/rooms/10000003?adults=2" {
		t.Errorf("visits %q, final %q, want 5 visits ending on /rooms/10000003", run.Visits, snapshotURL(run.Snapshot))
	}
	task, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if row := Score(task, run); row.Passed != 12 {
		t.Errorf("the goal session scores %d of 12: %+v", row.Passed, row.Steps)
	}
}

func TestASubAgentSessionReadsBackAsARunThatScoresTwelve(t *testing.T) {
	run, err := RunFromEvents(ArmB1, "testdata/events/b1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	wantVisits := []string{
		"https://www.google.com/search?q=airbnb",
		"https://www.airbnb.com.br/",
		"https://www.airbnb.com.br/s/Atibaia--SP/homes?query=Atibaia%2C%20SP&checkin=2026-10-09&checkout=2026-10-15&adults=2",
		"https://www.airbnb.com.br/s/Atibaia--SP/homes?query=Atibaia%2C%20SP&checkin=2026-10-09&checkout=2026-10-15&adults=2&room_types%5B%5D=Entire%20home%2Fapt&price_max=4000&display_total_price=true&min_bedrooms=2&amenities%5B%5D=7",
		"https://www.airbnb.com.br/rooms/10000001?adults=2",
		"https://www.airbnb.com.br/rooms/10000002?adults=2",
		"https://www.airbnb.com.br/rooms/10000003?adults=2",
	}
	if !slices.Equal(run.Visits, wantVisits) {
		t.Errorf("visits %q, want %q", run.Visits, wantVisits)
	}
	var tabs []string
	for _, tab := range run.Tabs {
		tabs = append(tabs, tab.ID)
	}
	if !slices.Equal(tabs, []string{"11", "12", "13", "14"}) {
		t.Errorf("tabs %v, want 11 12 13 14", tabs)
	}
	if run.BrowserTokens == nil {
		t.Fatal("a sub-agent arm recorded no browser tokens")
	}
	if run.MainTokens != 6000 || *run.BrowserTokens != 1300 {
		t.Errorf("main tokens %d and browser tokens %d, want 6000 and 1300", run.MainTokens, *run.BrowserTokens)
	}
	if run.Repeated != 1 || run.Refused != 1 || run.WallMS != 180000 {
		t.Errorf("repeated %d, refused %d, wall %d ms, want 1, 1 and 180000", run.Repeated, run.Refused, run.WallMS)
	}
	if run.Conditions.MainBuild != "claude-opus-5-20260901" || run.Conditions.BrowserBuild != "claude-haiku-5-20260901" || run.Conditions.Wire != "claude-sub" {
		t.Errorf("conditions %+v", run.Conditions)
	}
	if snapshotURL(run.Snapshot) != "https://www.airbnb.com.br/rooms/10000003?adults=2" {
		t.Errorf("the final snapshot is %q", run.Snapshot)
	}
	task, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if row := Score(task, run); row.Passed != 12 {
		t.Errorf("the session scores %d of 12: %+v", row.Passed, row.Steps)
	}
}
