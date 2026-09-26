package frame

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/trace"
	"tofu/internal/widget"
)

const (
	fullSessionColumns = 145
	sessionRefColumns  = 120
	workspaceColumns   = 105
	pathColumns        = 77
	sideGap            = 2
	settingsLink       = "[settings]"
	settingsIndex      = -1
)

type Tab struct {
	Label string
	Count int
}

type Hit struct {
	Start int
	End   int
	Index int
}

type rightDrop int

const (
	dropNothing rightDrop = iota
	dropName
	dropSession
	dropClock
)

func Top(head Head, tabs []Tab, current int, width int) (string, []Hit) {
	left := topLeft(head, width)
	nav, hits := chips(tabLabels(tabs, width), current)
	leftWidth, navWidth := cells(left), cells(nav)
	right := topRight(head, width, dropNothing)
	for drop := dropName; drop <= dropClock && leftWidth+navWidth+cells(right)+2*sideGap > width; drop++ {
		right = topRight(head, width, drop)
	}
	rightWidth := cells(right)
	start := max(leftWidth+sideGap, (width-navWidth)/2)
	if start+navWidth+sideGap+rightWidth > width {
		start = max(leftWidth+sideGap, width-navWidth-sideGap-rightWidth)
	}
	navRow := draw(nav)
	if room := max(0, width-start-sideGap-rightWidth); navWidth > room {
		navRow, navWidth = ansi.Truncate(navRow, room, ""), room
		for len(hits) > 0 && hits[len(hits)-1].End > room {
			hits = hits[:len(hits)-1]
		}
	}
	for index := range hits {
		hits[index].Start += start
		hits[index].End += start
	}
	row := draw(append(left, fill(start-leftWidth, look.Background))) + navRow
	gap := width - start - navWidth - rightWidth
	if gap < 0 {
		return look.ChromeRow(width, look.Background, row, ""), hits
	}
	linkEnd := width - 1
	return row + draw(append([]span{fill(gap, look.Background)}, right...)), append(hits, Hit{Start: linkEnd - len(settingsLink), End: linkEnd, Index: settingsIndex})
}

func topLeft(head Head, width int) []span {
	chip := span{text: " tofu ", fg: look.Background, bg: look.Mint}
	switch {
	case width < pathColumns:
		return []span{chip}
	case width < workspaceColumns:
		return []span{chip, {text: " ./" + head.Path + "/", fg: look.Text, bg: look.Background}}
	}
	left := []span{chip, {text: "  |  ./" + head.Path + "/", fg: look.Text, bg: look.Background}}
	if head.Fresh || head.Branch == "" {
		return left
	}
	return append(left,
		span{text: " (", fg: look.MutedColor, bg: look.Background},
		span{text: "git:" + head.Branch, fg: look.Amber, bg: look.Background},
		span{text: ")", fg: look.MutedColor, bg: look.Background})
}

func topRight(head Head, width int, drop rightDrop) []span {
	var right []span
	if drop < dropSession {
		right = session(head, width, drop)
	}
	if drop < dropClock && !head.Started.IsZero() {
		clock := widget.Until(head.At.Sub(head.Started))
		switch {
		case len(right) == 0:
		case width >= sessionRefColumns:
			clock = " | " + clock
		default:
			clock = "  |  " + clock
		}
		right = append(right, span{text: clock, fg: look.MutedColor, bg: look.Background})
	}
	if len(right) > 0 {
		right = append(right, fill(1, look.Background))
	}
	return append(right, span{text: settingsLink + " ", fg: look.Blue, bg: look.Background})
}

func session(head Head, width int, drop rightDrop) []span {
	if head.Notice != "" {
		return []span{{text: head.Notice, fg: look.Amber, bg: look.Background}}
	}
	if width < workspaceColumns || head.SessionID == "" {
		return nil
	}
	name := head.SessionName
	if width >= sessionRefColumns && width < fullSessionColumns {
		name, _, _ = strings.Cut(name, "-")
	}
	var parts []span
	if name != "" && drop < dropName {
		parts = append(parts, span{text: name, fg: look.Text, bg: look.Background})
	}
	if width < sessionRefColumns {
		return parts
	}
	if len(parts) > 0 {
		parts = append(parts, fill(1, look.Background))
	}
	short := trace.Short(head.SessionID)
	return append(parts, span{text: "[session" + short + "]", session: short})
}

func tabLabels(tabs []Tab, width int) []string {
	labels := make([]string, len(tabs))
	for index, tab := range tabs {
		label := tab.Label
		if width < workspaceColumns {
			label = label[strings.LastIndexAny(label, " -")+1:]
		}
		if tab.Count > 0 && width >= pathColumns {
			label += " (" + strconv.Itoa(tab.Count) + ")"
		}
		labels[index] = label
	}
	return labels
}

func chips(labels []string, current int) ([]span, []Hit) {
	nav := make([]span, 0, 2*len(labels))
	hits := make([]Hit, 0, len(labels)+1)
	column := 0
	for index, label := range labels {
		if index > 0 {
			nav = append(nav, fill(1, look.Background))
			column++
		}
		chip := span{text: " " + label + " ", fg: look.MutedColor, bg: look.Panel}
		if index == current {
			chip.fg, chip.bg = look.Background, look.Mint
		}
		end := column + widget.Cells(chip.text)
		nav = append(nav, chip)
		hits = append(hits, Hit{Start: column, End: end, Index: index})
		column = end
	}
	return nav, hits
}
