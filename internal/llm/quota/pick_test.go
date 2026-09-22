package quota

import (
	"testing"
	"time"
)

func reported(fraction float64, id string, resetsAt time.Time) Window {
	return Window{ID: id, Used: Used{Fraction: fraction, Reported: true}, ResetsAt: resetsAt}
}

func claudeAccount(id int64, windows ...Window) Candidate {
	return Candidate{ID: id, Provider: ClaudeSub, Report: Report{Provider: ClaudeSub, Windows: windows}}
}

func TestAFullerWeekDoesNotSaveAnAccountWhoseFiveHourWindowIsNearlySpent(t *testing.T) {
	soon := recordedNow.Add(time.Hour)
	later := recordedNow.Add(72 * time.Hour)
	candidates := []Candidate{
		claudeAccount(1, reported(0.9, fiveHourWindow, soon), reported(0.1, sevenDayWindow, later)),
		claudeAccount(2, reported(0.2, fiveHourWindow, soon), reported(0.8, sevenDayWindow, later)),
	}

	choice, found := Pick(candidates, ClaudeSub, recordedNow)
	if !found || choice.ID != 2 {
		t.Fatalf("Pick chose %d (%v), want the account whose tightest window has more left", choice.ID, found)
	}
	if choice.Headroom.Window != sevenDayWindow || choice.Headroom.Fraction > 0.21 {
		t.Fatalf("the choice is bound by %q at %v, want its tightest window", choice.Headroom.Window, choice.Headroom.Fraction)
	}
	if left := Left(candidates[0].Report, recordedNow); left.Window != fiveHourWindow || left.Fraction > 0.11 {
		t.Fatalf("headroom of the fuller week reads %v, want the five hour window at about a tenth", left)
	}
}

func TestAWindowWhoseResetHasPassedIsFullAgainAndASoonerResetBreaksATie(t *testing.T) {
	passed := claudeAccount(1, reported(1, fiveHourWindow, recordedNow.Add(-time.Minute)))
	if left := Left(passed.Report, recordedNow); left.Fraction != 1 {
		t.Fatalf("a window past its reset reads %v left, want all of it", left.Fraction)
	}
	if Spent(passed.Report, recordedNow) {
		t.Fatal("an account whose only window has already reset reads as spent")
	}

	pending := claudeAccount(2, reported(1, fiveHourWindow, recordedNow.Add(time.Minute)))
	if !Spent(pending.Report, recordedNow) {
		t.Fatal("a full window that has not reset yet reads as having room")
	}

	sooner := claudeAccount(3, reported(0.5, fiveHourWindow, recordedNow.Add(time.Hour)))
	later := claudeAccount(4, reported(0.5, sevenDayWindow, recordedNow.Add(96*time.Hour)))
	choice, found := Pick([]Candidate{later, sooner}, ClaudeSub, recordedNow)
	if !found || choice.ID != 3 {
		t.Fatalf("Pick chose %d at equal headroom, want the one whose window resets sooner", choice.ID)
	}
}

func TestOnlyTheNamedSubscriptionCompetes(t *testing.T) {
	claude := claudeAccount(1, reported(0.95, fiveHourWindow, recordedNow.Add(time.Hour)))
	codex := Candidate{ID: 2, Provider: CodexSub, Report: Report{
		Provider: CodexSub,
		Windows:  []Window{reported(0.05, fiveHourWindow, recordedNow.Add(time.Hour))},
	}}
	candidates := []Candidate{claude, codex}

	forTurn, found := Pick(candidates, ClaudeSub, recordedNow)
	if !found || forTurn.ID != 1 {
		t.Fatalf("a claude-sub turn chose %d, want the only claude-sub account however little it has left", forTurn.ID)
	}
	forChild, found := Pick(candidates, CodexSub, recordedNow)
	if !found || forChild.ID != 2 {
		t.Fatalf("a codex-sub child chose %d, want the only codex-sub account", forChild.ID)
	}
	if _, found := Pick([]Candidate{claude}, CodexSub, recordedNow); found {
		t.Fatal("a codex-sub child was given a claude-sub account")
	}
}

func TestAnAccountReportingNoWindowLosesToOneThatReportsRoom(t *testing.T) {
	silent := claudeAccount(1)
	reporting := claudeAccount(2, reported(0, fiveHourWindow, recordedNow.Add(time.Hour)))
	choice, found := Pick([]Candidate{silent, reporting}, ClaudeSub, recordedNow)
	if !found || choice.ID != 2 {
		t.Fatalf("Pick chose %d, want the account whose windows are known", choice.ID)
	}
	if Left(silent.Report, recordedNow).Window != "" {
		t.Fatal("an account that reported no window is bound by one anyway")
	}
}
