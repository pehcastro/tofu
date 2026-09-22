package main

import (
	"strconv"
	"strings"
	"time"

	"tofu/internal/widget"
)

const (
	accountNumberColumn = 4
	factGap             = 2
	factTextFloor       = 24
	cardTop             = "┌ "
	cardSide            = "│ "
	cardFoot            = "└"
	cardRule            = "─"
	factState           = "state"
	factLogin           = "login"
	factPlan            = "plan"
	factWindows         = "windows"
)

type layout struct {
	shade  palette
	now    time.Time
	column int
	width  int
}

func statusText(report statusReport, shade palette, now time.Time, width int) string {
	page := layout{shade: shade, now: now, column: factColumn(report), width: width}
	lines := headlineLines(report, page)
	for _, source := range report.Sources {
		lines = append(lines, "", source.Subscription)
		for _, account := range source.Accounts {
			lines = append(lines, "")
			lines = append(lines, accountCard(account, page)...)
		}
	}
	lines = append(lines, "", jevName)
	for _, line := range wrapHard(report.Gate, max(width-len(reportIndent), factTextFloor)) {
		lines = append(lines, reportIndent+line)
	}
	return strings.Join(lines, "\n") + "\n"
}

func headlineLines(report statusReport, page layout) []string {
	painted := page.shade.settled(report.State)
	if report.State != statusServing {
		painted = page.shade.unsettled(report.State)
	}
	if gap := page.width - widget.Cells(report.Headline) - len(report.State); gap >= factGap {
		return []string{report.Headline + strings.Repeat(" ", gap) + painted}
	}
	return []string{report.Headline, painted}
}

func accountCard(account accountReport, page layout) []string {
	paint := page.shade.settled
	if account.Attention {
		paint = page.shade.unsettled
	}
	body := factLines(factState, account.State, page, paint)
	body = append(body, factLines(factLogin, account.Login, page, nil)...)
	body = append(body, factLines(factPlan, account.Plan, page, nil)...)
	reported := false
	for _, window := range account.Windows {
		if !window.Reported {
			continue
		}
		reported = true
		body = append(body, widget.Pad(window.ID, page.column)+
			page.shade.full(window.Used, widget.Quota(window.Used, window.ResetsAt, page.now)))
	}
	if !reported {
		body = append(body, factLines(factWindows, usageNoWindowReported, page, nil)...)
	}
	head := widget.Pad("#"+strconv.FormatInt(account.ID, 10), accountNumberColumn) + account.Account
	return card(head, body, page)
}

func card(head string, body []string, page layout) []string {
	lines := []string{reportIndent + page.shade.rule(cardTop) + head}
	widest := widget.Cells(cardTop) + widget.Cells(head)
	for _, line := range body {
		lines = append(lines, reportIndent+page.shade.rule(cardSide)+line)
		widest = max(widest, widget.Cells(cardSide)+widget.Cells(line))
	}
	foot := min(widest, max(page.width-len(reportIndent), 1))
	return append(lines, reportIndent+page.shade.rule(cardFoot+strings.Repeat(cardRule, foot-1)))
}

func factColumn(report statusReport) int {
	widest := max(len(factState), len(factLogin), len(factPlan), len(factWindows))
	for _, source := range report.Sources {
		for _, account := range source.Accounts {
			for _, window := range account.Windows {
				if window.Reported {
					widest = max(widest, len(window.ID))
				}
			}
		}
	}
	return widest + factGap
}

func factLines(label, text string, page layout, paint func(string) string) []string {
	if text == "" {
		return nil
	}
	room := max(page.width-len(reportIndent)-widget.Cells(cardSide)-page.column, factTextFloor)
	var lines []string
	for _, line := range wrapHard(text, room) {
		if paint != nil {
			line = paint(line)
		}
		lines = append(lines, widget.Pad(label, page.column)+line)
		label = ""
	}
	return lines
}

func wrapHard(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		for widget.Cells(word) > width {
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			runes := []rune(word)
			lines = append(lines, string(runes[:width]))
			word = string(runes[width:])
		}
		switch {
		case line == "":
			line = word
		case widget.Cells(line)+1+widget.Cells(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	return append(lines, line)
}
