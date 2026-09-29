package quota

import (
	"slices"
	"strings"
	"time"

	"tofu/internal/transport"
)

type Provider string

const (
	ClaudeSub Provider = "claude-sub"
	CodexSub  Provider = "codex-sub"
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

const windowModelMark = ":"

func (w Window) Binds(spends []string) bool {
	period, scope, scoped := strings.Cut(w.ID, windowModelMark)
	if !scoped {
		return true
	}
	return slices.ContainsFunc(spends, func(spend string) bool {
		spendPeriod, spendScope, ok := strings.Cut(spend, windowModelMark)
		return ok && spendScope != "" && spendPeriod == period &&
			strings.Contains(strings.ToLower(scope), strings.ToLower(spendScope))
	})
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

func (r Report) binding(spends []string) []Window {
	bound := make([]Window, 0, len(r.Windows))
	for _, window := range r.Windows {
		if window.Binds(spends) {
			bound = append(bound, window)
		}
	}
	return bound
}

func (r Report) Exhausted() bool {
	if r.CreditOverage {
		return false
	}
	if r.LimitReached {
		return true
	}
	for _, window := range r.binding(nil) {
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
	for _, window := range r.binding(nil) {
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
	case len(report.binding(nil)) == 0:
		return ConditionUnknown
	}
	return ConditionServing
}

func SpendLimitLine() string {
	return "spend limit: tofu sets none, an api key's spending limit is the provider's, " +
		"set on the account that issued the key"
}
