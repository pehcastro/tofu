package quota

import "time"

type Provider string

const (
	Anthropic Provider = "anthropic"
	Codex     Provider = "codex"
)

type Source string

const (
	SourceHeaders   Source = "response headers"
	SourceEndpoint  Source = "usage endpoint"
	SourceRejection Source = "quota rejection"
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
	Source        Source
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

func ContextWindowTokens(catalogTokens, reportedTokens int) int {
	if reportedTokens > catalogTokens {
		return reportedTokens
	}
	return catalogTokens
}
