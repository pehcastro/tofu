package frame

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"boji/interface/tui/theme"
	"boji/interface/tui/widget"
)

const (
	quotaBarWidth = 12
	clockFormat   = "15:04"
	separator     = "  ·  "
)

type Head struct {
	Repo    string
	Branch  string
	Model   string
	At      time.Time
	Elapsed time.Duration
}

type Quota struct {
	Label    string
	Fraction float64
	Reported bool
	ResetsAt time.Time
}

type Status struct {
	TokensIn  int
	TokensOut int
	Decisions int
	Quota     Quota
	At        time.Time
	Note      string
}

func Header(head Head, width int) string {
	fields := []string{"boji", head.Repo, head.Branch, head.Model}
	if !head.At.IsZero() {
		fields = append(fields, head.At.Format(clockFormat)+" "+short(head.Elapsed))
	}
	return theme.Bar().Width(width).Render(widget.Fit(join(fields), width))
}

func Bar(status Status, width int) string {
	fields := []string{
		quotaText(status.Quota, status.At),
		"⇅ " + count(status.TokensIn) + "/" + count(status.TokensOut),
		"jev " + strconv.Itoa(status.Decisions),
	}
	if status.Note != "" {
		fields = append(fields, status.Note)
	}
	return theme.Bar().Width(width).Render(widget.Fit(join(fields), width))
}

func quotaText(quota Quota, at time.Time) string {
	if quota.Label == "" {
		return "quota unread"
	}
	if !quota.Reported {
		return quota.Label + " quota not reported"
	}
	text := quota.Label + " " + widget.Bar(quota.Fraction, quotaBarWidth) + " " + widget.Percent(quota.Fraction)
	if quota.ResetsAt.IsZero() {
		return text
	}
	return text + " resets " + quota.ResetsAt.Local().Format(clockFormat) + " (" + remaining(quota.ResetsAt.Sub(at)) + ")"
}

func remaining(until time.Duration) string {
	switch {
	case until <= 0:
		return "now"
	case until >= time.Hour:
		return "in " + strconv.Itoa(int(until.Hours())) + "h"
	}
	return "in " + strconv.Itoa(int(until.Minutes())) + "m"
}

func join(fields []string) string {
	kept := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != "" {
			kept = append(kept, field)
		}
	}
	return strings.Join(kept, separator)
}

func count(tokens int) string {
	if tokens < 1000 {
		return strconv.Itoa(tokens)
	}
	return strconv.Itoa(tokens/1000) + "k"
}

func short(elapsed time.Duration) string {
	seconds := int(elapsed.Seconds())
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
