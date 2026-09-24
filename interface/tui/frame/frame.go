package frame

import (
	"strconv"
	"strings"
	"time"

	"tofu/interface/tui/theme"
	"tofu/interface/tui/trace"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const (
	modelSeparator    = "/"
	pathPrefix        = "./"
	separator         = "  ·  "
	contextUnread     = "context unread"
	quotaUnread       = "quota unread"
	agentMark         = "●"
	ForkNotice        = "⟳ forking"
	develSuffix       = "+dev"
	dirtyMark         = "-dirty"
	quotaPercentCells = 4
)

type headDropped int

const (
	headNothing headDropped = iota
	headClock
	headSession
	headModel
	headBranch
)

type rowOneDrop int

const (
	rowOneFull rowOneDrop = iota
	rowOneNoQuotaBar
	rowOneNoReset
	rowOneLabelOnly
	rowOneNoContextBar
)

func RowOneDropOrder() []string {
	return []string{"quota bar", "quota reset", "quota label", "context bar"}
}

type rowTwoDrop int

const (
	rowTwoFull rowTwoDrop = iota
	rowTwoNoRelease
	rowTwoNoNote
	rowTwoNoJev
	rowTwoNoAgent
	rowTwoNoTokens
)

func Resolved(provider, model string) string {
	if provider == "" || model == "" {
		return provider + model
	}
	return provider + modelSeparator + model
}

type Head struct {
	Path        string
	Branch      string
	Provider    string
	Model       string
	SessionName string
	SessionID   string
	At          time.Time
	Started     time.Time
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
	CacheRead int
	Decisions int
	Quotas    []Quota
	Agents    int
	At        time.Time
	Note      string
	Release   string
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
	for drop := headNothing; drop < headBranch; drop++ {
		if text := join(headFields(head, drop)); widget.Cells(text) <= width {
			return text
		}
	}
	return join(headFields(head, headBranch))
}

func headFields(head Head, drop headDropped) []string {
	fields := []string{pathPrefix + head.Path}
	if drop < headBranch {
		fields = append(fields, head.Branch)
	}
	if drop < headModel {
		fields = append(fields, Resolved(head.Provider, head.Model))
	}
	if drop < headSession {
		fields = append(fields, sessionText(head))
	}
	if drop < headClock && !head.Started.IsZero() {
		fields = append(fields, widget.Until(head.At.Sub(head.Started)))
	}
	return fields
}

func sessionText(head Head) string {
	if head.SessionID == "" {
		return ""
	}
	return strings.TrimSpace(head.SessionName + " " + trace.Short(head.SessionID))
}

func Bar(status Status, width int) string {
	row1, row2 := barLines(status, width)
	style := theme.Bar().Width(width)
	return style.Render(widget.Fit(row1, width)) + "\n" + style.Render(widget.Fit(row2, width))
}

func barLines(status Status, width int) (string, string) {
	return row1Text(status, width), row2Text(status, width)
}

func row1Text(status Status, width int) string {
	for drop := rowOneFull; drop <= rowOneNoContextBar; drop++ {
		if text := join(row1Fields(status, drop)); widget.Cells(text) <= width {
			return text
		}
	}
	return contextText(status.Context, rowOneNoContextBar)
}

func row1Fields(status Status, drop rowOneDrop) []string {
	fields := []string{contextText(status.Context, drop)}
	if len(status.Quotas) == 0 {
		return append(fields, quotaUnread)
	}
	for _, quota := range status.Quotas {
		fields = append(fields, quotaText(quota, status.At, drop))
	}
	return fields
}

func row2Text(status Status, width int) string {
	for drop := rowTwoFull; drop <= rowTwoNoTokens; drop++ {
		if text := join(row2Fields(status, drop)); widget.Cells(text) <= width {
			return text
		}
	}
	return ""
}

func row2Fields(status Status, drop rowTwoDrop) []string {
	var fields []string
	if drop < rowTwoNoTokens {
		fields = append(fields, tokensText(status))
	}
	if drop < rowTwoNoJev {
		fields = append(fields, "jev "+strconv.Itoa(status.Decisions))
	}
	if drop < rowTwoNoAgent && status.Agents > 0 {
		fields = append(fields, agentMark+strconv.Itoa(status.Agents))
	}
	if drop < rowTwoNoNote {
		fields = append(fields, status.Note)
	}
	if drop < rowTwoNoRelease {
		fields = append(fields, releaseLabel(status.Release))
	}
	return fields
}

func tokensText(status Status) string {
	text := widget.Count(status.TokensIn) + " read  " + widget.Count(status.TokensOut) + " write"
	if status.CacheRead > 0 {
		text += "  " + widget.Count(status.CacheRead) + " cached"
	}
	return text
}

func releaseLabel(release string) string {
	if release == "" {
		return ""
	}
	return "tofu " + release
}

func contextText(carried Context, drop rowOneDrop) string {
	if carried.Budget <= 0 {
		return contextUnread
	}
	numbers := widget.Count(carried.Used) + "/" + widget.Count(carried.Budget)
	if drop >= rowOneNoContextBar {
		return numbers
	}
	return numbers + " " + widget.Bar(float64(carried.Used)/float64(carried.Budget), konst.MeterBarWidthChars)
}

func quotaText(quota Quota, at time.Time, drop rowOneDrop) string {
	if quota.Label == "" {
		return quotaUnread
	}
	if !quota.Reported {
		return quota.Label + " quota not reported"
	}
	if drop >= rowOneLabelOnly {
		return quota.Label
	}
	reading := widget.Lead(widget.Percent(quota.Fraction), quotaPercentCells)
	if drop < rowOneNoQuotaBar {
		reading = widget.Bar(quota.Fraction, konst.MeterBarWidthChars) + " " + reading
	}
	text := quota.Label + " " + reading
	if drop < rowOneNoReset {
		text += resetClause(quota.ResetsAt, at)
	}
	return text
}

func resetClause(resetsAt, at time.Time) string {
	if resetsAt.IsZero() {
		return ""
	}
	if left := resetsAt.Sub(at); left > 0 {
		return "  resets in " + widget.Until(left)
	}
	return "  resets now"
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
