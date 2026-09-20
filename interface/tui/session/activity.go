package session

import (
	"slices"
	"strings"
	"time"

	"boji/interface/tui/crew"
	"boji/interface/tui/theme"
	"boji/internal/widget"
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

type activity struct {
	since  time.Duration
	name   string
	owns   string
	intent string
	tokens int
}

func (m Model) activityRows() []activity {
	rows := make([]activity, 0, len(m.Children)+1)
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
		})
	}
	if !m.Busy {
		return rows
	}
	state, what := thinking, ""
	for index := len(m.entries) - 1; index >= 0; index-- {
		if entry := m.entries[index]; entry.running() {
			state, what = working, entry.label()
			break
		}
	}
	if m.Awaiting() {
		state = waitingOnYou
	}
	return append(rows, activity{since: m.elapsed(m.began), name: state, intent: what})
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
	clock := spin(row.since) + " " + widget.Pad(widget.Elapsed(row.since), elapsedColumn)
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
	line := theme.Accent().Render(clock) + gap + theme.Text().Render(who) +
		theme.Tool().Render(widget.Pad(widget.Fit(row.intent, room), room))
	if spent != "" {
		line += theme.Text().Render(spent)
	}
	return line
}

func spin(since time.Duration) string {
	frames := []rune(spinFrames)
	return string(frames[int(since/TickInterval)%len(frames)])
}

func (m Model) elapsed(at time.Time) time.Duration {
	now := m.now()
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
