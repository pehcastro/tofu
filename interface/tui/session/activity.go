package session

import (
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/progress"
	"tofu/interface/tui/trace"
	roster "tofu/internal/subagent"
	"tofu/internal/widget"
)

const (
	elapsedColumn = 5
	nameColumn    = 10
	ownsColumn    = 20
	tokensColumn  = 5
	ownsFrom      = 72
	intentLeast   = 12
	gapCells      = len(gap)
	stoppingWord  = "stopping"
)

type phase int

const (
	requesting phase = iota
	thinking
	working
	waitingOnYou
)

func (p phase) drawn() (string, lipgloss.Style) {
	switch p {
	case requesting:
		return "requesting", look.Style(look.MutedColor)
	case thinking:
		return "thinking", look.Style(look.Mint)
	case working:
		return "working", look.Style(look.Mint)
	case waitingOnYou:
		return "waiting", look.Style(look.Amber)
	}
	panic("session: unknown phase")
}

type activity struct {
	since  time.Duration
	name   string
	owns   string
	intent string
	tokens int
}

func (m *Model) reached() phase {
	if m.Awaiting() {
		return waitingOnYou
	}
	if m.inFlight() && !m.respondedOnce {
		return requesting
	}
	if slices.ContainsFunc(m.entries, Entry.running) {
		return working
	}
	return thinking
}

func (m *Model) settle() {
	if !m.Busy {
		return
	}
	at := m.reached()
	if at == m.phase || m.now().Sub(m.shown) < PhaseDwell {
		return
	}
	m.phase, m.shown = at, m.now()
}

func (m *Model) activityRows() []activity {
	rows := make([]activity, 0, len(m.Children))
	for _, child := range m.Children {
		if child.State != roster.Working {
			continue
		}
		rows = append(rows, activity{
			since:  child.Since,
			name:   child.Name,
			owns:   strings.Join(child.Owns, " "),
			intent: child.Doing,
			tokens: child.Tokens,
		})
	}
	return rows
}

func (m *Model) phaseSince() time.Duration {
	now := m.now()
	if !m.waiting.IsZero() {
		now = m.waiting
	}
	return max(now.Sub(m.entered), 0)
}

func (m *Model) requestLine() string {
	line, id := margin, m.cookedID
	switch {
	case m.Busy:
		word, style := m.phase.drawn()
		if m.Stopping || m.LettingToolsFinish {
			word, style = stoppingWord, look.Style(look.Amber)
		}
		since := m.phaseSince()
		line += look.Accent(progress.Spin(since, TickInterval)) + " " + style.Render(word) + look.Muted(requestSeparator+widget.Until(since)+metaGap)
		id = m.turnID
	case m.cooked != "":
		line += look.Muted(m.cooked + metaGap)
	default:
		return ""
	}
	if short := trace.Short(id); short != "" {
		line += look.TypedID(requestKind, short)
	}
	return line
}

func (m *Model) activityLines() []string {
	rows := m.activityRows()
	width := m.width - messageMargin
	held := width >= ownsFrom && slices.ContainsFunc(rows, func(row activity) bool { return row.owns != "" })
	elapsed := widget.Column(rows, func(row activity) string { return widget.Until(row.since) }, elapsedColumn)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		clock := progress.Spin(row.since, TickInterval) + " " + widget.Pad(widget.Until(row.since), elapsed)
		name := column(row.name, nameColumn)
		who, spent := name, ""
		if held {
			who += column(row.owns, ownsColumn)
		}
		if row.tokens > 0 {
			spent = gap + widget.Lead(widget.Count(row.tokens), tokensColumn)
		}
		room := width - widget.Cells(clock+gap+who+spent)
		if room < intentLeast {
			who, spent = name, ""
			room = width - widget.Cells(clock+gap+who)
		}
		room = max(room, 1)
		line := look.Accent(clock) + gap + look.Style(look.Text).Render(who) + look.Muted(widget.Pad(widget.Fit(row.intent, room), room)+spent)
		lines = append(lines, margin+line)
	}
	return lines
}

func column(text string, width int) string {
	if widget.Cells(text) > width {
		text = widget.Fit(text, width-gapCells)
	}
	return widget.Pad(text, width)
}

func (m *Model) inFlight() bool { return !m.requested.IsZero() && m.answered.IsZero() }

func (m *Model) elapsed(at time.Time) time.Duration {
	now := m.now()
	if m.inFlight() {
		now = m.requested
	}
	if !m.waiting.IsZero() {
		now = m.waiting
	}
	return max(now.Sub(at), 0)
}

func (m Model) Awaiting() bool { return !m.waiting.IsZero() }

func (m Model) TakesAnswerDigits() bool { return m.Awaiting() && m.composer.Value() == "" }

func (m *Model) markAsked(awaiting bool) {
	for index := len(m.entries) - 1; index >= 0; index-- {
		if decision := m.entries[index].Decision; decision != nil && decision.Verdict == Ask {
			decision.Awaiting = awaiting
			return
		}
	}
}

func (m *Model) Await() {
	if !m.Busy || m.Awaiting() {
		return
	}
	m.waiting = m.now()
	m.markAsked(true)
}

func (m *Model) Resume() {
	if !m.Awaiting() {
		return
	}
	m.markAsked(false)
	waited := max(m.now().Sub(m.waiting), 0)
	m.waiting = time.Time{}
	m.hold(waited)
	m.entered = m.entered.Add(waited)
}

func (m *Model) hold(waited time.Duration) {
	if waited <= 0 {
		return
	}
	m.began = m.began.Add(waited)
	for index := range m.entries {
		if m.entries[index].running() {
			m.entries[index].Started = m.entries[index].Started.Add(waited)
		}
	}
}

func (m *Model) Requesting() {
	if !m.Busy {
		return
	}
	m.requested, m.answered = m.now(), time.Time{}
}

func (m *Model) Returned() {
	if !m.inFlight() {
		return
	}
	m.answered = m.now()
	m.respondedOnce = true
	waited := m.answered.Sub(m.requested)
	m.waited += waited
	m.hold(waited)
}
