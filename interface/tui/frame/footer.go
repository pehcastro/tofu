package frame

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/internal/widget"
)

const (
	secondSourceColumns = 150
	chatRightColumns    = 82
	footerSeparator     = "  |  "
	subscriptionSuffix  = "-sub"
	rightSeparator      = "  ·  "
)

func Footer(status Status, width int, right string) string {
	switch status.Mode {
	case StatusHidden:
		return draw([]span{fill(width, look.Panel)})
	case "", StatusCompact, StatusDetailed:
	default:
		panic("frame: unknown status mode " + string(status.Mode))
	}
	if width < chatRightColumns {
		right = ""
	}
	rights := []string{right}
	if bare, _ := footerLeft(status, width, noQuota); cells(bare) <= roomBeside(width, right) {
		for shorter := right; shorter != ""; {
			shorter = shorterRight(shorter)
			rights = append(rights, shorter)
		}
	}
	for _, kept := range rights {
		for form := fullLabel; form < noQuota; form++ {
			if head, tail := footerLeft(status, width, form); cells(head) <= roomBeside(width, kept) {
				return footerRow(slices.Concat(head, tail), width, kept)
			}
		}
	}
	head, tail := footerLeft(status, width, noQuota)
	return footerRow(slices.Concat(head, tail), width, right)
}

func roomBeside(width int, right string) int {
	if right == "" {
		return width
	}
	return width - lipgloss.Width(right) - 1
}

func footerRow(spans []span, width int, right string) string {
	left := draw(spans)
	if room := roomBeside(width, right); right != "" && cells(spans) > room {
		left = ansi.Truncate(left, max(0, room), "…")
	}
	return look.ChromeRow(width, look.Panel, left, right)
}

func shorterRight(right string) string {
	plain := ansi.Strip(right)
	at := strings.LastIndex(plain, rightSeparator)
	if at < 0 {
		return ""
	}
	return ansi.Truncate(right, ansi.StringWidth(plain[:at])+1, "")
}

type quotaLabel int

const (
	fullLabel quotaLabel = iota
	sourceLabel
	vendorLabel
	noQuota
)

func (form quotaLabel) of(quota Quota) string {
	switch form {
	case fullLabel:
		return quota.Label
	case sourceLabel:
		return sourceOf(quota)
	case vendorLabel:
		return strings.TrimSuffix(sourceOf(quota), subscriptionSuffix)
	case noQuota:
	}
	panic("frame: unknown quota label " + strconv.Itoa(int(form)))
}

func footerLeft(status Status, width int, form quotaLabel) (head, tail []span) {
	if status.Note != "" {
		return []span{panel(" "+status.Note, look.Text)}, nil
	}
	head = []span{panel(" ctx ", look.Blue)}
	switch {
	case status.Context.Budget <= 0:
		return append(head, panel("unread", look.FaintColor)), nil
	case status.Fresh:
		return append(head, panel("0/"+widget.Count(status.Context.Budget), look.Text), panel(footerSeparator+"new session", look.FaintColor)), nil
	}
	head = append(head, panel(widget.Count(status.Context.Used)+"/"+widget.Count(status.Context.Budget), look.Text))
	shown := fullestPerSource(status.Quotas, status.InUse)
	if len(shown) == 0 && form != noQuota {
		head = append(head, panel(footerSeparator+"quota unread", look.FaintColor))
	}
	if len(shown) > 1 && width < secondSourceColumns {
		form = max(form, vendorLabel)
	}
	for _, quota := range shown {
		if form == noQuota {
			break
		}
		name := form.of(quota)
		if accountsOf(status.Quotas, sourceOf(quota)) > 1 {
			first, rest, _ := strings.Cut(name, " ")
			name = strings.TrimSpace(first + " " + quota.Account + " " + rest)
		}
		head = append(head, panel(footerSeparator+name+" ", look.Text))
		if quota.Reported {
			head = append(head, panel(widget.Percent(quota.Fraction), look.Amber))
		} else {
			head = append(head, panel("not reported", look.FaintColor))
		}
	}
	if status.Mode != StatusDetailed {
		return head, nil
	}
	if reset := nextReset(shown, status.At); reset > 0 {
		tail = append(tail, panel(footerSeparator+"resets in "+widget.Until(reset), look.Amber))
	}
	counters := footerSeparator + widget.Count(status.TokensIn) + " read  " + widget.Count(status.TokensOut) + " write"
	if status.CacheRead > 0 {
		counters += "  " + widget.Count(status.CacheRead) + " cached"
	}
	return head, append(tail, panel(counters+footerSeparator+"jev "+strconv.Itoa(status.Decisions), look.Text))
}

func fullestPerSource(quotas []Quota, inUse []string) []Quota {
	var shown []Quota
	for _, quota := range quotas {
		if !slices.Contains(inUse, sourceOf(quota)) {
			continue
		}
		index := slices.IndexFunc(shown, func(held Quota) bool { return sourceOf(held) == sourceOf(quota) })
		switch {
		case index < 0:
			shown = append(shown, quota)
		case quota.Reported && (!shown[index].Reported || quota.Fraction > shown[index].Fraction):
			shown[index] = quota
		}
	}
	return shown
}

func accountsOf(quotas []Quota, source string) int {
	var accounts []string
	for _, quota := range quotas {
		if sourceOf(quota) == source && !slices.Contains(accounts, quota.Account) {
			accounts = append(accounts, quota.Account)
		}
	}
	return len(accounts)
}

func sourceOf(quota Quota) string {
	source, _, _ := strings.Cut(quota.Label, " ")
	return source
}

func nextReset(quotas []Quota, at time.Time) time.Duration {
	var soonest time.Duration
	for _, quota := range quotas {
		if left := quota.ResetsAt.Sub(at); quota.Reported && left > 0 && (soonest == 0 || left < soonest) {
			soonest = left
		}
	}
	return soonest
}

func ChatRight(model string, effort string) string {
	right := []span{panel(" [&orchestrator] ", look.Mint), panel(" ·  "+model, look.Text)}
	if effort == "" {
		return draw(append(right, panel(" ", look.Text)))
	}
	return draw(append(right, panel("  ·  ", look.Text), panel("reasoning "+effort+" ", effortColor(effort))))
}

func effortColor(effort string) look.Color {
	switch effort {
	case "low":
		return look.Blue
	case "medium":
		return look.Mint
	case "high":
		return look.Amber
	case "xhigh":
		return look.Violet
	case "max":
		return look.Red
	}
	return look.MutedColor
}
