package session

import (
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/progress"
	"tofu/interface/tui/subagent"
	"tofu/interface/tui/trace"
	roster "tofu/internal/subagent"
	"tofu/internal/widget"
)

const stoppingWord = "stopping"

type phase int

const (
	requesting phase = iota
	thinking
	working
	waitingOnYou
	waitingOnSubAgent
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
	case waitingOnSubAgent:
		return "waiting on", look.Style(look.Mint)
	}
	panic("session: unknown phase")
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
	if !m.inFlight() && len(m.workingSubAgents()) > 0 {
		return waitingOnSubAgent
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

func (m *Model) workingSubAgents() []string {
	var names []string
	for _, subAgent := range m.SubAgents {
		if subAgent.State == roster.Working {
			names = append(names, subAgent.Name)
		}
	}
	return names
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
		word = style.Render(word)
		switch {
		case m.Stopping || m.LettingToolsFinish:
			word = look.Style(look.Amber).Render(stoppingWord)
		case m.phase == waitingOnSubAgent:
			for _, name := range m.workingSubAgents() {
				word += " " + look.AgentRef(name)
			}
		}
		line += look.Accent(progress.Spin(m.frame)) + " " + word + look.Muted(requestSeparator+widget.Until(m.phaseSince())+metaGap)
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

func (m *Model) spawnedBy(entry Entry) (subagent.Row, bool) {
	at := slices.IndexFunc(m.SubAgents, func(row subagent.Row) bool { return row.Name == entry.SubAgent })
	if entry.SubAgent == "" || at < 0 {
		return subagent.Row{}, false
	}
	return m.SubAgents[at], true
}

func settledMark(state roster.State) (string, look.Color) {
	switch state {
	case roster.Working, roster.Reopened, roster.WaitingAnswer:
		return "", look.Violet
	case roster.InReview, roster.Finished:
		return "✓ ", look.FaintColor
	case roster.Errored:
		return "✗ ", look.Red
	case roster.Parked:
		return "○ ", look.FaintColor
	}
	panic("session: unknown sub-agent state")
}

func (m *Model) spawnLine(entry Entry, row subagent.Row) string {
	mark, colour := settledMark(row.State)
	body := oneLine(entry.Body)
	if mark != "" {
		return look.Style(colour).Render(widget.Fit(mark+body, m.textWidth()))
	}
	onNow := strings.Join(row.Owns, " ")
	if len(row.Calls) > 0 {
		last := row.Calls[len(row.Calls)-1]
		onNow = oneLine(last.Tool + " " + last.Text)
	}
	if onNow != "" {
		onNow = gap + widget.Fit(onNow, m.textWidth()/statusShare)
	}
	room := max(m.textWidth()-widget.Cells(onNow), 1)
	left := progress.Work(m.frame) + " " + widget.Until(row.Since) + gap + body
	return look.Style(colour).Render(widget.Pad(widget.Fit(left, room), room) + onNow)
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

func (m *Model) Awaiting() bool { return !m.waiting.IsZero() }

func (m *Model) TakesAnswerDigits() bool { return m.Awaiting() && m.composer.Value() == "" }

func (m *Model) markAsked(awaiting bool) {
	for index := len(m.entries) - 1; index >= 0; index-- {
		if decision := m.entries[index].Decision; decision != nil && decision.Verdict == Ask {
			decision.Awaiting, decision.asked = awaiting, decision.asked || awaiting
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
