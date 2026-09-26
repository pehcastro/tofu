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
	secondAccountColumns = 150
	chatRightColumns     = 82
	footerSeparator      = "  |  "
)

func Footer(status Status, width int, right string) string {
	switch status.Mode {
	case StatusHidden:
		return draw([]span{fill(width, look.Panel)})
	case "", StatusCompact, StatusDetailed:
	default:
		panic("frame: unknown status mode " + string(status.Mode))
	}
	spans := footerLeft(status, width)
	left := draw(spans)
	if width < chatRightColumns {
		right = ""
	}
	if room := width - lipgloss.Width(right) - 1; right != "" && cells(spans) > room {
		left = ansi.Truncate(left, max(0, room), "…")
	}
	return look.ChromeRow(width, look.Panel, left, right)
}

func footerLeft(status Status, width int) []span {
	if status.Note != "" {
		return []span{panel(" "+status.Note, look.Text)}
	}
	left := []span{panel(" ctx ", look.Blue)}
	switch {
	case status.Context.Budget <= 0:
		return append(left, panel("unread", look.FaintColor))
	case status.Fresh:
		return append(left, panel("0/"+widget.Count(status.Context.Budget), look.Text), panel(footerSeparator+"new session", look.FaintColor))
	}
	left = append(left, panel(widget.Count(status.Context.Used)+"/"+widget.Count(status.Context.Budget), look.Text))
	shown := fullestPerAccount(status.Quotas)
	if len(shown) == 0 {
		left = append(left, panel(footerSeparator+"quota unread", look.FaintColor))
	}
	for index, quota := range shown {
		if index > 0 && width < secondAccountColumns {
			break
		}
		left = append(left, panel(footerSeparator+quota.Label+" ", look.Text))
		if quota.Reported {
			left = append(left, panel(widget.Percent(quota.Fraction), look.Amber))
		} else {
			left = append(left, panel("not reported", look.FaintColor))
		}
	}
	if status.Mode != StatusDetailed {
		return left
	}
	if reset := nextReset(shown, status.At); reset > 0 {
		left = append(left, panel(footerSeparator+"resets in "+widget.Until(reset), look.Amber))
	}
	counters := footerSeparator + widget.Count(status.TokensIn) + " read  " + widget.Count(status.TokensOut) + " write"
	if status.CacheRead > 0 {
		counters += "  " + widget.Count(status.CacheRead) + " cached"
	}
	return append(left, panel(counters+footerSeparator+"jev "+strconv.Itoa(status.Decisions), look.Text))
}

func fullestPerAccount(quotas []Quota) []Quota {
	var shown []Quota
	for _, quota := range quotas {
		index := slices.IndexFunc(shown, func(held Quota) bool { return accountOf(held) == accountOf(quota) })
		switch {
		case index < 0:
			shown = append(shown, quota)
		case quota.Reported && (!shown[index].Reported || quota.Fraction > shown[index].Fraction):
			shown[index] = quota
		}
	}
	return shown
}

func accountOf(quota Quota) string {
	account, _, _ := strings.Cut(quota.Label, " ")
	return account
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
