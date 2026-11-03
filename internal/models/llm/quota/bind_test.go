package quota

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func perModelWindow(t *testing.T, report Report) Window {
	t.Helper()
	for _, window := range report.Windows {
		if !window.Binds(nil) {
			return window
		}
	}
	t.Fatalf("no per-model window in %v", report.Windows)
	return Window{}
}

func TestAPerModelWindowAtItsCapLeavesTheAccountServing(t *testing.T) {
	report, err := FromAnthropicUsage(recordedBody(t, "anthropic-usage.json"), recordedNow)
	if err != nil {
		t.Fatalf("parsing the anthropic usage payload: %v", err)
	}
	if scoped := perModelWindow(t, report); scoped.State() != StateExhausted {
		t.Fatalf("the recorded per-model window reads %+v, want exhausted", scoped)
	}
	if report.Exhausted() {
		t.Fatal("an account reads spent while both of its own windows have room")
	}
	if Spent(report, nil, recordedNow) {
		t.Fatal("the picker calls the account spent while both of its own windows have room")
	}
	if got := Diagnose(report, nil); got != ConditionServing {
		t.Fatalf("Diagnose read %v, want %v", got, ConditionServing)
	}
	if _, waiting := report.WaitUntil(recordedNow); waiting {
		t.Fatal("a per-model window parked the account until its reset")
	}
}

func TestAnAccountWhoseOwnWindowIsSpentStaysSpent(t *testing.T) {
	scoped := Window{ID: sevenDayWindow + windowModelMark + "any", Used: Used{Fraction: 1, Reported: true}}
	resets := recordedNow.Add(time.Hour)
	for _, id := range []string{fiveHourWindow, sevenDayWindow} {
		report := Report{Provider: ClaudeSub, Windows: []Window{
			{ID: id, Used: Used{Fraction: 1, Reported: true}, ResetsAt: resets},
			scoped,
		}}
		if !report.Exhausted() {
			t.Fatalf("an account whose %s window is at its cap reads as serving", id)
		}
		if !Spent(report, nil, recordedNow) {
			t.Fatalf("the picker offers an account whose %s window is at its cap", id)
		}
		if got := Diagnose(report, nil); got != ConditionWindowSpent {
			t.Fatalf("Diagnose read %v for a spent %s window, want %v", got, id, ConditionWindowSpent)
		}
		until, waiting := report.WaitUntil(recordedNow)
		if !waiting || !until.Equal(resets) {
			t.Fatalf("a spent %s window gave %v %v, want its own reset", id, until, waiting)
		}
	}
}

func TestAnAccountReportingOnlyAPerModelWindowIsNotCalledServing(t *testing.T) {
	report := Report{Provider: ClaudeSub, Windows: []Window{
		{ID: sevenDayWindow + windowModelMark + "any", Used: Used{Fraction: 0.2, Reported: true}},
	}}
	if got := Diagnose(report, nil); got != ConditionUnknown {
		t.Fatalf("Diagnose read %v with no window of its own reported, want %v", got, ConditionUnknown)
	}
}

func TestHeadroomIgnoresAWindowThatBindsNothing(t *testing.T) {
	room := claudeAccount(1,
		reported(0.09, fiveHourWindow, recordedNow.Add(time.Hour)),
		reported(0.68, sevenDayWindow, recordedNow.Add(96*time.Hour)),
		reported(1, sevenDayWindow+windowModelMark+"any", recordedNow.Add(96*time.Hour)))
	none := claudeAccount(2, reported(1, fiveHourWindow, recordedNow.Add(time.Hour)))

	left := Left(room.Report, nil, recordedNow)
	if left.Window != sevenDayWindow || left.Fraction < 0.31 || left.Fraction > 0.33 {
		t.Fatalf("headroom reads %+v, want about a third bound by the week", left)
	}
	choice, found := Pick([]Candidate{none, room}, ClaudeSub, nil, recordedNow)
	if !found || choice.ID != 1 {
		t.Fatalf("Pick chose %d, want the account with nine tenths of its five hours left", choice.ID)
	}
}

func TestAScopedWindowAtItsCapTurnsAwayOnlyTheModelItNames(t *testing.T) {
	scopedRuns := []string{fiveHourWindow, sevenDayWindow, "7d:Fable"}
	plainRuns := []string{fiveHourWindow, sevenDayWindow}
	for _, displayName := range []string{"Fable", "Claude Fable 5", "fable-5-1", "CLAUDE FABLE 5.1"} {
		body := fmt.Sprintf(`{"five_hour":{"utilization":10,"resets_at":"2026-06-02T14:00:00Z"},
			"seven_day":{"utilization":20,"resets_at":"2026-06-06T00:00:00Z"},
			"limits":[{"kind":"weekly_scoped","percent":100,"resets_at":"2026-06-06T00:00:00Z",
			"scope":{"model":{"display_name":%q}}}]}`, displayName)
		report, err := FromAnthropicUsage([]byte(body), recordedNow)
		if err != nil {
			t.Fatalf("parsing a payload scoped to %q: %v", displayName, err)
		}
		scopedFull := Candidate{ID: 1, Provider: ClaudeSub, Report: report}
		busier := claudeAccount(2,
			reported(0.5, fiveHourWindow, recordedNow.Add(time.Hour)),
			reported(0.5, sevenDayWindow, recordedNow.Add(96*time.Hour)))
		candidates := []Candidate{scopedFull, busier}
		if choice, _ := Pick(candidates, ClaudeSub, scopedRuns, recordedNow); choice.ID != 2 {
			t.Errorf("display name %q: the model the full window names was given account %d, want 2", displayName, choice.ID)
		}
		if !Spent(report, scopedRuns, recordedNow) {
			t.Errorf("display name %q: the account reads unspent for the model its full window names", displayName)
		}
		if choice, _ := Pick(candidates, ClaudeSub, plainRuns, recordedNow); choice.ID != 1 {
			t.Errorf("display name %q: a model the full window does not name was given account %d, want 1", displayName, choice.ID)
		}
	}
	other := claudeAccount(1,
		reported(0.1, fiveHourWindow, recordedNow.Add(time.Hour)),
		reported(1, sevenDayWindow+windowModelMark+"claude-opus-5", recordedNow.Add(96*time.Hour)))
	if Spent(other.Report, scopedRuns, recordedNow) {
		t.Error("a full window scoped to another model binds the model that does not share it")
	}
}

var recordedNow = time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

func recordedBody(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the recorded body: %v", err)
	}
	return raw
}

func reported(fraction float64, id string, resetsAt time.Time) Window {
	return Window{ID: id, Used: Used{Fraction: fraction, Reported: true}, ResetsAt: resetsAt}
}

func claudeAccount(id int64, windows ...Window) Candidate {
	return Candidate{ID: id, Provider: ClaudeSub, Report: Report{Provider: ClaudeSub, Windows: windows}}
}
