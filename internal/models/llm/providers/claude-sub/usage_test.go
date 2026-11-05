package claudesub

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tofu/internal/models/llm/quota"
)

var recordedNow = time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

func reported(fraction float64, id string, resetsAt time.Time) quota.Window {
	return quota.Window{ID: id, Used: quota.Used{Fraction: fraction, Reported: true}, ResetsAt: resetsAt}
}

func claudeAccount(id int64, windows ...quota.Window) quota.Candidate {
	return quota.Candidate{ID: id, Provider: quota.ClaudeSub, Report: quota.Report{Provider: quota.ClaudeSub, Windows: windows}}
}

func perModelWindow(t *testing.T, report quota.Report) quota.Window {
	t.Helper()
	for _, window := range report.Windows {
		if !window.Binds(nil) {
			return window
		}
	}
	t.Fatalf("no per-model window in %v", report.Windows)
	return quota.Window{}
}

func TestAPerModelWindowAtItsCapLeavesTheAccountServing(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "anthropic-usage.json"))
	if err != nil {
		t.Fatalf("reading the recorded body: %v", err)
	}
	report, err := parseUsage(raw, recordedNow)
	if err != nil {
		t.Fatalf("parsing the anthropic usage payload: %v", err)
	}
	if scoped := perModelWindow(t, report); scoped.State() != quota.StateExhausted {
		t.Fatalf("the recorded per-model window reads %+v, want exhausted", scoped)
	}
	if report.Exhausted() {
		t.Fatal("an account reads spent while both of its own windows have room")
	}
	if quota.Spent(report, nil, recordedNow) {
		t.Fatal("the picker calls the account spent while both of its own windows have room")
	}
	if got := quota.Diagnose(report, nil); got != quota.ConditionServing {
		t.Fatalf("Diagnose read %v, want %v", got, quota.ConditionServing)
	}
	if _, waiting := report.WaitUntil(recordedNow); waiting {
		t.Fatal("a per-model window parked the account until its reset")
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
		report, err := parseUsage([]byte(body), recordedNow)
		if err != nil {
			t.Fatalf("parsing a payload scoped to %q: %v", displayName, err)
		}
		scopedFull := quota.Candidate{ID: 1, Provider: quota.ClaudeSub, Report: report}
		busier := claudeAccount(2,
			reported(0.5, fiveHourWindow, recordedNow.Add(time.Hour)),
			reported(0.5, sevenDayWindow, recordedNow.Add(96*time.Hour)))
		candidates := []quota.Candidate{scopedFull, busier}
		if choice, _ := quota.Pick(candidates, quota.ClaudeSub, scopedRuns, recordedNow); choice.ID != 2 {
			t.Errorf("display name %q: the model the full window names was given account %d, want 2", displayName, choice.ID)
		}
		if !quota.Spent(report, scopedRuns, recordedNow) {
			t.Errorf("display name %q: the account reads unspent for the model its full window names", displayName)
		}
		if choice, _ := quota.Pick(candidates, quota.ClaudeSub, plainRuns, recordedNow); choice.ID != 1 {
			t.Errorf("display name %q: a model the full window does not name was given account %d, want 1", displayName, choice.ID)
		}
	}
	other := claudeAccount(1,
		reported(0.1, fiveHourWindow, recordedNow.Add(time.Hour)),
		reported(1, sevenDayWindow+quota.WindowModelMark+"claude-opus-5", recordedNow.Add(96*time.Hour)))
	if quota.Spent(other.Report, scopedRuns, recordedNow) {
		t.Error("a full window scoped to another model binds the model that does not share it")
	}
}
