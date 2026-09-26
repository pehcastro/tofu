package quota

import (
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/frame"
	"tofu/interface/tui/look"
	"tofu/internal/widget"
)

const (
	compactWidth   = 78
	compactHeight  = 24
	dialogMargin   = 4
	dialogPadding  = 2
	fullModalWidth = 68
	fullMargin     = 6
	fullMaxHeight  = 18
	compactMaxW    = 54
	compactMaxH    = 14
	fullMeter      = 18
	compactMeter   = 12
	cardWidth      = 60
	readingWidth   = 58
	windowColumn   = 8
	percentCells   = 4
	warnFraction   = 0.7
	fullFraction   = 0.9
	fillGlyph      = "━"
	trackGlyph     = "─"
	heading        = "Subscription status"
	closeHint      = "esc"
	resetsIn       = " · resets in "
	compactResets  = "  ·  "
	notReported    = "not reported"
)

type account struct {
	name    string
	windows []frame.Quota
}

func byAccount(quotas []frame.Quota) []account {
	var accounts []account
	for _, q := range quotas {
		name, _, _ := strings.Cut(q.Label, " ")
		at := len(accounts)
		for i, known := range accounts {
			if known.name == name {
				at = i
			}
		}
		if at == len(accounts) {
			accounts = append(accounts, account{name: name})
		}
		accounts[at].windows = append(accounts[at].windows, q)
	}
	return accounts
}

func meter(width int, q frame.Quota) string {
	colour := look.Mint
	if q.Fraction >= warnFraction {
		colour = look.Amber
	}
	if q.Fraction >= fullFraction {
		colour = look.Red
	}
	filled := int(math.Round(max(0, min(1, q.Fraction)) * float64(width)))
	return look.Style(colour).Render(strings.Repeat(fillGlyph, filled)) + look.Faint(strings.Repeat(trackGlyph, width-filled))
}

func window(q frame.Quota) string {
	_, name, _ := strings.Cut(q.Label, " ")
	return look.Muted(widget.Pad(name, windowColumn))
}

func reading(q frame.Quota, now time.Time, resets string) string {
	if !q.Reported {
		return look.Faint(notReported)
	}
	said := widget.Lead(widget.Percent(q.Fraction), percentCells)
	if !q.ResetsAt.IsZero() {
		said += resets + widget.Until(q.ResetsAt.Sub(now))
	}
	return look.Title(said)
}

func Dialog(base string, width, height int, quotas []frame.Quota, now time.Time) string {
	accountStyle := look.Style(look.Amber).Bold(true)
	var modal string
	if width < compactWidth || height < compactHeight {
		modalWidth := min(compactMaxW, width-dialogMargin)
		content := look.Sides(look.Title(heading), look.Faint(closeHint), modalWidth-2*dialogPadding)
		for _, one := range byAccount(quotas) {
			content += "\n\n" + accountStyle.Render(one.name)
			for _, q := range one.windows {
				content += "\n" + window(q) + meter(compactMeter, q) + " " + reading(q, now, compactResets)
			}
		}
		modal = look.ModalPane(modalWidth, min(compactMaxH, height-dialogMargin), look.Panel, dialogPadding, content)
	} else {
		card := lipgloss.NewStyle().Width(cardWidth).Padding(0, 1).Background(look.Style(look.PanelLight).GetForeground())
		content := look.Sides(look.Title(heading), look.Faint(closeHint), cardWidth)
		for _, one := range byAccount(quotas) {
			lines := []string{accountStyle.Render(one.name)}
			for _, q := range one.windows {
				lines = append(lines, look.Sides(window(q)+"  "+meter(fullMeter, q), reading(q, now, resetsIn), readingWidth))
			}
			content += "\n\n" + card.Render(strings.Join(lines, "\n"))
		}
		modal = look.ModalPane(min(fullModalWidth, width-fullMargin), min(fullMaxHeight, height-dialogMargin), look.Panel, dialogPadding, content)
	}
	left, top := max(1, (width-lipgloss.Width(modal))/2), max(1, (height-lipgloss.Height(modal))/2)
	return look.Over(look.Dim(base), modal, left, top)
}
