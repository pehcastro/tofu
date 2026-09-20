package session

import (
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/crew"
	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	spinFrames    = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	elapsedColumn = 5
	nameColumn    = 10
	ownsColumn    = 20
	tokensColumn  = 5
	ownsFrom      = 72
	intentLeast   = 12
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
	if m.inFlight() {
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
		if child.State != crew.Running {
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
	if m.phase == requesting {
		return max(m.now().Sub(m.requested), 0)
	}
	return m.elapsed(m.began)
}

func (m Model) activityLines() []string {
	rows := m.activityRows()
	held := m.width >= ownsFrom &&
		slices.ContainsFunc(rows, func(row activity) bool { return row.owns != "" })
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, m.activityLine(row, held))
	}
	return lines
}

func (m Model) activityLine(row activity, held bool) string {
	clock := spin(row.since) + " " + widget.Pad(widget.Until(row.since), elapsedColumn)
	name := widget.Pad(widget.Fit(row.name, nameColumn), nameColumn)
	who, spent := name, ""
	if held {
		who += widget.Pad(widget.Fit(row.owns, ownsColumn), ownsColumn)
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

func spin(since time.Duration) string {
	frames := []rune(spinFrames)
	return string(frames[int(since/TickInterval)%len(frames)])
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
	waited := m.now().Sub(m.waiting)
	m.waiting = time.Time{}
	m.hold(waited)
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
	waited := m.answered.Sub(m.requested)
	m.waited += waited
	m.hold(waited)
}
