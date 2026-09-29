package airbnb

import (
	"slices"
	"testing"
)

func TestASubAgentSessionReadsBackAsARunThatScoresTwelve(t *testing.T) {
	run, err := RunFromEvents(ArmB1, "testdata/events/b1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	wantVisits := []string{
		"https://www.google.com/search?q=airbnb",
		"https://www.airbnb.com.br/",
		"https://www.airbnb.com.br/s/Atibaia--SP/homes?query=Atibaia%2C%20SP&checkin=2026-10-09&checkout=2026-10-15&adults=2",
		"https://www.airbnb.com.br/s/Atibaia--SP/homes?query=Atibaia%2C%20SP&checkin=2026-10-09&checkout=2026-10-15&adults=2&room_types%5B%5D=Entire%20home%2Fapt&price_max=1500&min_bedrooms=2&amenities%5B%5D=7",
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
