package session

import (
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/progress"
	"tofu/interface/tui/theme"
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
		return "requesting", theme.Dim()
	case thinking:
		return "thinking", theme.Accent()
	case working:
		return "working", theme.Tool()
	case waitingOnYou:
		return "waiting", theme.Warn()
	}
	panic("session: unknown phase")
}

type activity struct {
	since  time.Duration
	name   string
	owns   string
	intent string
	tokens int
	style  lipgloss.Style
}

func (m Model) reached() (phase, string) {
	if m.Awaiting() {
		return waitingOnYou, ""
	}
	if m.inFlight() && !m.respondedOnce {
		return requesting, ""
	}
	for index := len(m.entries) - 1; index >= 0; index-- {
		if entry := m.entries[index]; entry.running() {
			return working, entry.label()
		}
	}
	return thinking, ""
}

func (m *Model) settle() {
	if !m.Busy {
		return
	}
	at, intent := m.reached()
	if at == m.phase && intent == m.intent {
		return
	}
	if m.now().Sub(m.shown) < PhaseDwell {
		return
	}
	m.phase, m.intent, m.shown = at, intent, m.now()
}

func (m Model) activityRows() []activity {
	rows := make([]activity, 0, len(m.Children)+1)
	_, busyStyle := working.drawn()
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
			style:  busyStyle,
		})
	}
	if !m.Busy {
		return rows
	}
	name, style := m.phase.drawn()
	return append(rows, activity{since: m.phaseSince(), name: name, intent: m.intent, style: style})
}

func (m Model) phaseSince() time.Duration {
	now := m.now()
	if !m.waiting.IsZero() {
		now = m.waiting
	}
	return max(now.Sub(m.entered), 0)
}

func (m Model) activityLines() []string {
	rows := m.activityRows()
	held := m.width >= ownsFrom &&
		slices.ContainsFunc(rows, func(row activity) bool { return row.owns != "" })
	elapsed := widget.Column(rows, func(row activity) string { return widget.Until(row.since) }, elapsedColumn)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, m.activityLine(row, held, elapsed))
	}
	return lines
}

func column(text string, width int) string {
	if widget.Cells(text) > width {
		text = widget.Fit(text, width-gapCells)
	}
	return widget.Pad(text, width)
}

func (m Model) activityLine(row activity, held bool, elapsed int) string {
	clock := progress.Spin(row.since, TickInterval) + " " + widget.Pad(widget.Until(row.since), elapsed)
	name := column(row.name, nameColumn)
	who, spent := name, ""
	if held {
		who += column(row.owns, ownsColumn)
	}
	if row.tokens > 0 {
		spent = gap + widget.Lead(widget.Count(row.tokens), tokensColumn)
	}
	room := m.width - widget.Cells(clock+gap+who+spent)
	if room < intentLeast {
		who, spent = name, ""
		room = m.width - widget.Cells(clock+gap+who)
	}
	room = max(room, 1)
	return row.style.Render(clock + gap + who + widget.Pad(widget.Fit(row.intent, room), room) + spent)
}

func (m Model) progressLine(end int) string {
	if end != len(m.entries) || !m.Busy {
		return ""
	}
	entry := m.entries[end-1]
	if entry.label() == "" {
		return ""
	}
	line := progress.Line{Label: entry.label(), Live: entry.running()}
	if line.Live {
		line.Since, line.Tick = m.elapsed(entry.Started), TickInterval
	}
	id := trace.Short(entry.ID)
	if id == "" {
		return line.View(m.width)
	}
	room := max(m.width-widget.Cells(id+gap), 1)
	return widget.Pad(line.View(room), room) + gap + theme.ID().Render(id)
}

func (m Model) inFlight() bool { return !m.requested.IsZero() && m.answered.IsZero() }

func (m Model) elapsed(at time.Time) time.Duration {
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
