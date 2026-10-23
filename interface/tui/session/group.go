package session

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/internal/host"
	"tofu/internal/toolcard"
	"tofu/internal/widget"
)

const (
	callIndent   = "  "
	openIndent   = "   "
	highlightGap = "   "
	runningDot   = "●"
	breathFrames = 4
	collapseWord = "collapse"
	failedWord   = "failed"
)

type group struct {
	calls   []Entry
	summary *host.GroupSummary
	running int
	total   int
	headed  bool
	open    bool
}

func (e Entry) standsAlone() bool {
	return e.Failed || e.verdictShown() != "" || e.Card != nil && e.Card.Family == host.CardFamily(toolcard.Asked)
}

func (m *Model) groupOf(start int) group {
	_, end := m.blockAt(start)
	g := group{calls: m.entries[start:end], running: -1, open: m.opened[m.entries[start].ID]}
	var settled time.Time
	for at, call := range g.calls {
		if call.running() {
			g.running = at
		}
		if call.Summary != nil && !call.Ended.Before(settled) {
			g.summary, settled = call.Summary, call.Ended
		}
		g.headed = g.headed || !call.standsAlone()
	}
	g.total = len(g.calls)
	if g.summary != nil {
		g.total = max(g.total, g.summary.Calls)
	}
	return g
}

func (m *Model) groupKey(key drawKey, start int) (drawKey, bool) {
	g := m.groupOf(start)
	key.calls, key.open, key.summary = len(g.calls), g.open, g.summary
	for _, call := range g.calls {
		if call.standsAlone() {
			key.verdict += call.ID + call.verdictShown() + " "
		}
	}
	return key, g.running >= 0
}

func (m *Model) groupLines(start int) []string {
	g := m.groupOf(start)
	var lines []string
	if g.headed {
		lines = append(lines, continuation+m.groupHead(g))
		if g.open && g.summary != nil {
			lines = append(lines, m.openLines(g.summary)...)
		}
	}
	room, failed := m.textWidth()-widget.Cells(callIndent), 0
	for _, call := range g.calls {
		switch {
		case !call.standsAlone():
		case call.verdictShown() != "":
			lines = append(lines, continuation+m.toolLine(call))
		case call.Failed:
			failed++
			lines = append(lines, continuation+callIndent+ansi.Truncate(failedCall(call)+highlightGap+look.Faint(strconv.Itoa(failed)+" of "+strconv.Itoa(g.total)), room, "…"))
		default:
			lines = append(lines, continuation+callIndent+look.Sides(look.Style(look.Text).Render(cardWords(call, ""))+" "+look.Muted(call.Card.Meta), "", room))
		}
	}
	return lines
}

func (m *Model) groupHead(g group) string {
	room := m.textWidth() - widget.Cells(callIndent)
	lead, left := callIndent, look.Muted(callCount(g.total))
	var right string
	switch first := g.calls[0]; {
	case g.running >= 0:
		dot, running, doing := look.Mint, g.calls[g.running], ""
		if m.frame/breathFrames%2 == 1 {
			dot = look.MintMuted
		}
		if running.Card != nil {
			doing = running.Card.Doing
		}
		lead = look.Style(dot).Render(runningDot) + " "
		left += highlightGap + look.Style(look.Text).Render(cardWords(running, doing))
		right = look.Faint(widget.Until(m.elapsed(first.Started)))
	case g.open:
		right = look.Faint(collapseWord)
	default:
		var ended time.Time
		for _, call := range g.calls {
			if call.Ended.After(ended) {
				ended = call.Ended
			}
		}
		right = look.Faint(widget.Until(ended.Sub(first.Started)))
	}
	if g.running < 0 && g.summary != nil {
		left = fittedHead(g.summary.Head, room-widget.Cells(right)-len(highlightGap))
	}
	return lead + look.Sides(left, right, room)
}

func (m *Model) openLines(summary *host.GroupSummary) []string {
	labels := widget.Column(summary.Open, func(line host.CardLine) string { return string(line.Family) }, 0)
	room := m.textWidth() - widget.Cells(callIndent+openIndent+highlightGap) - labels
	lines := make([]string, 0, len(summary.Open))
	for _, line := range summary.Open {
		label := look.Muted(widget.Pad(string(line.Family), labels))
		lines = append(lines, continuation+callIndent+openIndent+label+highlightGap+ansi.Truncate(painted(line.Detail), max(room, 1), "…"))
	}
	return lines
}

func (m *Model) Toggle(id string) bool {
	for at, entry := range m.entries {
		if entry.ID != id || id == "" {
			continue
		}
		if start, _ := m.blockAt(at); !m.folds(at) || start != at || !m.groupOf(at).headed {
			return false
		}
		m.opened[id] = !m.opened[id]
		m.revision++
		return true
	}
	return false
}

func failedCall(call Entry) string {
	why := ""
	if call.Card != nil {
		why = strings.TrimSpace(strings.TrimPrefix(call.Card.Meta, failedWord))
	}
	words := look.Style(look.Text).Render(cardWords(call, "")) + highlightGap + look.Style(look.Red).Render(failedWord)
	if why != "" {
		words += " " + look.Muted(why)
	}
	return words
}

func cardWords(call Entry, verb string) string {
	if call.Card == nil {
		return call.Head
	}
	if verb == "" {
		verb = call.Card.Verb
	}
	return strings.TrimSpace(verb + " " + call.Card.Object)
}

func callCount(calls int) string {
	if calls == 1 {
		return "1 call"
	}
	return strconv.Itoa(calls) + " calls"
}

func fittedHead(head []host.CardPiece, room int) string {
	fitted, cells, from := "", 0, 0
	for at := 1; at <= len(head); at++ {
		if at < len(head) && head[at].Text != highlightGap {
			continue
		}
		chunk := head[from:at]
		if width := phraseCells(chunk); from == 0 || cells+width <= room {
			fitted, cells = fitted+painted(chunk), cells+width
		}
		from = at
	}
	return fitted
}

func phraseCells(pieces []host.CardPiece) int {
	cells := 0
	for _, piece := range pieces {
		cells += widget.Cells(piece.Text)
	}
	return cells
}

func painted(pieces []host.CardPiece) string {
	var said strings.Builder
	for _, piece := range pieces {
		said.WriteString(inked(piece))
	}
	return said.String()
}

func inked(piece host.CardPiece) string {
	switch toolcard.Ink(piece.Ink) {
	case toolcard.Plain:
		return look.Style(look.Text).Render(piece.Text)
	case toolcard.Muted:
		return look.Muted(piece.Text)
	case toolcard.Ref:
		return look.Style(look.ReferenceID).Render(piece.Text)
	case toolcard.Added:
		return look.Style(look.Mint).Faint(true).Render(piece.Text)
	case toolcard.Removed:
		return look.Style(look.Red).Faint(true).Render(piece.Text)
	case toolcard.Failed:
		return look.Style(look.Red).Render(piece.Text)
	}
	panic("session: unknown card ink " + string(piece.Ink))
}
