package quota

import (
	"testing"
	"time"
)

const (
	fiveHourWindow = "5h"
	sevenDayWindow = "7d"
)

func TestAnAccountWhoseOwnWindowIsSpentStaysSpent(t *testing.T) {
	scoped := Window{ID: sevenDayWindow + WindowModelMark + "any", Used: Used{Fraction: 1, Reported: true}}
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
		{ID: sevenDayWindow + WindowModelMark + "any", Used: Used{Fraction: 0.2, Reported: true}},
	}}
	if got := Diagnose(report, nil); got != ConditionUnknown {
		t.Fatalf("Diagnose read %v with no window of its own reported, want %v", got, ConditionUnknown)
	}
}

func TestHeadroomIgnoresAWindowThatBindsNothing(t *testing.T) {
	room := claudeAccount(1,
		reported(0.09, fiveHourWindow, recordedNow.Add(time.Hour)),
		reported(0.68, sevenDayWindow, recordedNow.Add(96*time.Hour)),
		reported(1, sevenDayWindow+WindowModelMark+"any", recordedNow.Add(96*time.Hour)))
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

func TestFreshnessIsJitteredAndShorterNearALimitOnlyFromTheEndpoint(t *testing.T) {
	poller := &Poller{spread: func() float64 { return 0 }}
	near := Report{Source: SourceEndpoint, Windows: []Window{reported(0.92, fiveHourWindow, time.Time{})}}
	for _, check := range []struct {
		report Report
		spread float64
		want   time.Duration
	}{
		{Report{Source: SourceEndpoint}, 0, freshFor * 3 / 4},
		{Report{Source: SourceEndpoint}, 1, freshFor * 5 / 4},
		{near, 0.5, nearLimitCloseFor},
		{Report{Source: SourceHeaders, Windows: near.Windows}, 0.5, freshFor},
	} {
		poller.spread = func() float64 { return check.spread }
		if got := poller.freshFor(check.report); got != check.want {
			t.Errorf("freshness of %+v at spread %v is %v, want %v", check.report, check.spread, got, check.want)
		}
	}
}

func (p *Poller) PinForTest(dir string) {
	p.dir, p.spread = dir, func() float64 { return 0.5 }
}

var recordedNow = time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

func reported(fraction float64, id string, resetsAt time.Time) Window {
	return Window{ID: id, Used: Used{Fraction: fraction, Reported: true}, ResetsAt: resetsAt}
}

func claudeAccount(id int64, windows ...Window) Candidate {
	return Candidate{ID: id, Provider: ClaudeSub, Report: Report{Provider: ClaudeSub, Windows: windows}}
}
