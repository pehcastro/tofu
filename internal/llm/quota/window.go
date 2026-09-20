package quota

import (
	"time"

	"boji/internal/transport"
)

type Provider string

const (
	Anthropic Provider = "anthropic"
	Codex     Provider = "codex"
)

type State int

const (
	StateUnknown State = iota
	StateServing
	StateExhausted
)

type Used struct {
	Fraction float64
	Reported bool
}

type Window struct {
	ID       string
	Used     Used
	Duration time.Duration
	ResetsAt time.Time
}

func (w Window) State() State {
	switch {
	case !w.Used.Reported:
		return StateUnknown
	case w.Used.Fraction >= 1:
		return StateExhausted
	}
	return StateServing
}

type Report struct {
	Provider      Provider
	Plan          string
	Windows       []Window
	LimitReached  bool
	CreditOverage bool
	FetchedAt     time.Time
}

func (r Report) Exhausted() bool {
	if r.CreditOverage {
		return false
	}
	if r.LimitReached {
		return true
	}
	for _, window := range r.Windows {
		if window.State() == StateExhausted {
			return true
		}
	}
	return false
}

func (r Report) WaitUntil(now time.Time) (time.Time, bool) {
	if !r.Exhausted() {
		return time.Time{}, false
	}
	var latest time.Time
	for _, window := range r.Windows {
		if window.State() != StateExhausted {
			continue
		}
		if !window.ResetsAt.After(now) {
			return time.Time{}, false
		}
		if window.ResetsAt.After(latest) {
			latest = window.ResetsAt
		}
	}
	if latest.IsZero() {
		return time.Time{}, false
	}
	return latest, true
}

type Condition int

const (
	ConditionUnknown Condition = iota
	ConditionServing
	ConditionWindowSpent
	ConditionCredentialBroken
)

func Diagnose(report Report, err error) Condition {
	if err != nil {
		kind := transport.KindOf(err)
		if kind == transport.KindAuth || kind == transport.KindMissingCredential {
			return ConditionCredentialBroken
		}
		return ConditionUnknown
	}
	switch {
	case report.Exhausted():
		return ConditionWindowSpent
	case len(report.Windows) == 0:
		return ConditionUnknown
	}
	return ConditionServing
}

func SpendLimitLine() string {
	return "spend limit: boji sets none, an api key's spending limit is the provider's, " +
		"set on the account that issued the key"
}
