package frame

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"tofu/interface/tui/theme"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const (
	clockFormat   = "15:04"
	separator     = "  ·  "
	wireArrow     = " → "
	contextUnread = "context unread"
	agentMark     = "●"
	ForkNotice    = "⟳ forking the session in the background"
	develSuffix   = "+dev"
	dirtyMark     = "-dirty"
)

type headDropped int

const (
	headNothing headDropped = iota
	headElapsed
	headClock
	headModel
	headRepo
	headBranch
	headWire
)

type dropped int

const (
	nothing dropped = iota
	sessionNote
	jevCount
	resetClause
	tokenCounts
	agentCount
	quotaMeter
	contextBar
)

func Resolved(wire, model string) string {
	if wire == "" || model == "" {
		return wire + model
	}
	return wire + wireArrow + model
}

type Head struct {
	Release string
	Repo    string
	Branch  string
	Wire    string
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

type Context struct {
	Used   int
	Budget int
}

type Status struct {
	Context   Context
	TokensIn  int
	TokensOut int
	Decisions int
	Quota     Quota
	Agents    int
	At        time.Time
	Note      string
}

func Release(buildVersion, buildRevision string) string {
	if buildVersion == sys.DevelVersion || strings.HasSuffix(buildRevision, dirtyMark) {
		return konst.Version + develSuffix
	}
	return konst.Version
}

func Header(head Head, width int) string {
	return theme.Bar().Width(width).Render(widget.Fit(headerText(head, width), width))
}

func headerText(head Head, width int) string {
	for drop := headNothing; drop < headWire; drop++ {
		if text := join(headFields(head, drop)); widget.Cells(text) <= width {
			return text
		}
	}
	return join(headFields(head, headWire))
}

func headFields(head Head, drop headDropped) []string {
	fields := []string{"tofu " + head.Release}
	if drop < headRepo {
		fields = append(fields, head.Repo)
	}
	if drop < headBranch {
		fields = append(fields, head.Branch)
	}
	if drop < headWire {
		model := head.Model
		if drop >= headModel {
			model = ""
		}
		fields = append(fields, Resolved(head.Wire, model))
	}
	if drop >= headClock || head.At.IsZero() {
		return fields
	}
	clock := head.At.Format(clockFormat)
	if drop < headElapsed {
		clock += " " + short(head.Elapsed)
	}
	return append(fields, clock)
}

func Bar(status Status, width int) string {
	return theme.Bar().Width(width).Render(widget.Fit(barText(status, width), width))
}

func barText(status Status, width int) string {
	for drop := nothing; drop <= contextBar; drop++ {
		if text := join(fieldsWithout(status, drop)); widget.Cells(text) <= width {
			return text
		}
	}
	return contextText(status.Context, contextBar)
}

func fieldsWithout(status Status, drop dropped) []string {
	fields := []string{contextText(status.Context, drop), quotaText(status.Quota, status.At, drop)}
	if drop < tokenCounts {
		fields = append(fields, "⇅ "+widget.Count(status.TokensIn)+"/"+widget.Count(status.TokensOut))
	}
	if drop < jevCount {
		fields = append(fields, "jev "+strconv.Itoa(status.Decisions))
	}
	if drop < agentCount && status.Agents > 0 {
		fields = append(fields, agentMark+strconv.Itoa(status.Agents))
	}
	if drop < sessionNote {
		fields = append(fields, status.Note)
	}
	return fields
}

func contextText(carried Context, drop dropped) string {
	if carried.Budget <= 0 {
		return contextUnread
	}
	numbers := widget.Count(carried.Used) + "/" + widget.Count(carried.Budget)
	if drop >= contextBar {
		return numbers
	}
	return numbers + " " + widget.Bar(float64(carried.Used)/float64(carried.Budget), konst.MeterBarWidthChars)
}

func quotaText(quota Quota, at time.Time, drop dropped) string {
	if quota.Label == "" {
		return "quota unread"
	}
	if !quota.Reported {
		return quota.Label + " quota not reported"
	}
	if drop >= quotaMeter {
		return quota.Label
	}
	resetsAt := quota.ResetsAt
	if drop >= resetClause {
		resetsAt = time.Time{}
	}
	return quota.Label + " " + widget.Quota(quota.Fraction, resetsAt, at)
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

func short(elapsed time.Duration) string {
	seconds := int(elapsed.Seconds())
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
