package quota

import (
	"cmp"
	"slices"
	"time"
)

type Headroom struct {
	Fraction float64
	Window   string
	ResetsAt time.Time
}

type Candidate struct {
	ID       int64
	Provider Provider
	Report   Report
}

type Choice struct {
	ID       int64
	Headroom Headroom
}

func Left(report Report, spends []string, now time.Time) Headroom {
	bound := report.binding(spends)
	rooms := make([]Headroom, 0, len(bound))
	for _, window := range bound {
		if !window.Used.Reported {
			continue
		}
		room := Headroom{Fraction: 1 - window.Used.Fraction, Window: window.ID, ResetsAt: window.ResetsAt}
		if !window.ResetsAt.IsZero() && !window.ResetsAt.After(now) {
			room.Fraction = 1
		}
		rooms = append(rooms, room)
	}
	if len(rooms) == 0 {
		return Headroom{Fraction: 1}
	}
	return slices.MinFunc(rooms, func(a, b Headroom) int {
		return cmp.Or(cmp.Compare(a.Fraction, b.Fraction), soonerReset(a.ResetsAt, b.ResetsAt))
	})
}

func Spent(report Report, spends []string, now time.Time) bool {
	if report.CreditOverage {
		return false
	}
	return report.LimitReached || Left(report, spends, now).Fraction <= 0
}

func Pick(candidates []Candidate, provider Provider, spends []string, now time.Time) (Choice, bool) {
	ranked := make([]Choice, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Provider != provider {
			continue
		}
		ranked = append(ranked, Choice{ID: candidate.ID, Headroom: Left(candidate.Report, spends, now)})
	}
	if len(ranked) == 0 {
		return Choice{}, false
	}
	return slices.MinFunc(ranked, func(a, b Choice) int {
		return cmp.Or(
			cmp.Compare(b.Headroom.Fraction, a.Headroom.Fraction),
			soonerReset(a.Headroom.ResetsAt, b.Headroom.ResetsAt),
			cmp.Compare(a.ID, b.ID))
	}), true
}

func soonerReset(a, b time.Time) int {
	switch {
	case a.IsZero() && b.IsZero():
		return 0
	case a.IsZero():
		return 1
	case b.IsZero():
		return -1
	}
	return a.Compare(b)
}
