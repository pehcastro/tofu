package quota

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm/models"
)

func perModelWindow(t *testing.T, report Report) Window {
	t.Helper()
	for _, window := range report.Windows {
		if !window.Binds() {
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
	if Spent(report, recordedNow) {
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
		if !Spent(report, recordedNow) {
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

	left := Left(room.Report, recordedNow)
	if left.Window != sevenDayWindow || left.Fraction < 0.31 || left.Fraction > 0.33 {
		t.Fatalf("headroom reads %+v, want about a third bound by the week", left)
	}
	choice, found := Pick([]Candidate{none, room}, ClaudeSub, recordedNow)
	if !found || choice.ID != 1 {
		t.Fatalf("Pick chose %d, want the account with nine tenths of its five hours left", choice.ID)
	}
}

func TestTheCatalogRunsNoModelThatSpendsAPerModelWindow(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "library")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("skip 1, the shipped library is not beside the source at %s: %v", dir, err)
	}
	library, err := models.Load([]models.Layer{{Name: "library", Origin: "library", FS: os.DirFS(dir)}})
	if err != nil {
		t.Fatalf("loading the shipped model library: %v", err)
	}
	if len(library.Models) == 0 {
		t.Fatal("the shipped model library lists no model")
	}
	for _, model := range library.Models {
		if model.Use == models.UseExcluded {
			continue
		}
		for _, window := range model.Windows {
			if strings.Contains(window, windowModelMark) {
				t.Fatalf("%s is a model tofu will run and it spends a per-model window, so Window.Binds must read the catalog rather than the window id alone", model.File)
			}
		}
	}
}
