package quota

import (
	"strconv"
	"strings"
	"time"

	"boji/internal/transport"
)

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

func DoctorLine(report Report, err error, now time.Time) string {
	head := string(report.Provider) + ": "
	switch Diagnose(report, err) {
	case ConditionCredentialBroken:
		return head + "the credential is broken, run boji login " + string(report.Provider)
	case ConditionWindowSpent:
		if until, ok := report.WaitUntil(now); ok {
			return head + "the credential is fine, a window is spent, back at " + until.UTC().Format(time.RFC3339) + " " + within(until, now)
		}
		return head + "the credential is fine, a window is spent and no reset time was reported, so waiting is not authorized"
	case ConditionServing:
		used := make([]string, 0, len(report.Windows))
		for _, window := range report.Windows {
			used = append(used, window.ID+" "+window.usedText())
		}
		return head + "serving, " + strings.Join(used, ", ")
	case ConditionUnknown:
		if err != nil {
			return head + "the windows are unknown, the usage endpoint did not answer"
		}
		return head + "the windows are unknown, nothing was reported"
	}
	panic("quota: unknown condition")
}

func SpendLimitLine() string {
	return "spend limit: boji sets none, an api key's spending limit is the provider's, " +
		"set on the account that issued the key"
}

func (r Report) Lines(now time.Time) []string {
	head := string(r.Provider) + ", read from the " + string(r.Source)
	if r.Plan != "" {
		head += ", plan " + r.Plan
	}
	lines := []string{head}
	if len(r.Windows) == 0 {
		return append(lines, "  no window was reported")
	}
	for _, window := range r.Windows {
		lines = append(lines, "  "+window.line(now))
	}
	if r.CreditOverage {
		lines = append(lines, "  the plan allowance is spent and credits still fund it, so requests are served")
	}
	return lines
}

func (w Window) line(now time.Time) string {
	line := w.ID + " " + w.usedText()
	if w.Duration > 0 {
		line += " of a " + windowID(w.Duration) + " window"
	}
	if w.ResetsAt.IsZero() {
		return line + ", reset unknown"
	}
	return line + ", resets " + w.ResetsAt.UTC().Format(time.RFC3339) + " " + within(w.ResetsAt, now)
}

func (w Window) usedText() string {
	if !w.Used.Reported {
		return "used not reported"
	}
	return strconv.FormatFloat(w.Used.Fraction*percentFull, 'f', 1, 64) + "% used"
}

func within(at, now time.Time) string {
	remaining := at.Sub(now)
	if remaining <= 0 {
		return "(now)"
	}
	return "(in " + remaining.Truncate(time.Minute).String() + ")"
}
