package session

import (
	"slices"
	"strconv"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/progress"
	"tofu/interface/tui/subagent"
	"tofu/interface/tui/trace"
	"tofu/internal/konst"
	roster "tofu/internal/subagent"
	"tofu/internal/widget"
)

const (
	stoppingWord = "stopping"
	spawningWord = "spawning"
	runningWord  = "running"
	reportHead   = "report"
	readingWords = "reading the reports"
	stalledWord  = "stalled"
	stopAskTail  = ", press Ctrl+C again to confirm"
	doneMark     = "✓ "
	failedMark   = "✗ "
	stoppedMark  = "○ "
	wrapIndent   = "  "
)

type phase int

const (
	requesting phase = iota
	thinking
	working
	spawning
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
	case spawning:
		return spawningWord, look.Style(look.Violet)
	case waitingOnYou:
		return "waiting", look.Style(look.Amber)
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
	if m.Spawns > 0 {
		return spawning
	}
	return thinking
}

func (m *Model) WaitOn(working int) {
	if m.Busy || m.leadIdleSince.IsZero() && working == 0 {
		return
	}
	if m.leadIdleSince.IsZero() {
		m.leadIdleSince = m.now()
	}
	m.waitingOn = working
}

func subAgentCount(count int) string {
	if count == 1 {
		return "1 sub-agent"
	}
	return strconv.Itoa(count) + " sub-agents"
}

func (m *Model) leadIdleWords() string {
	if m.waitingOn == 0 {
		return readingWords
	}
	return "waiting on " + subAgentCount(m.waitingOn)
}

func (m *Model) AskToStop(subAgents int, until time.Time) {
	m.stopAsked, m.stopAskedUntil = subAgents, until
}

func (m *Model) AskedToStop() bool { return m.now().Before(m.stopAskedUntil) }

func Stalled(call subagent.Call, now time.Time) bool {
	return call.Result == "" && !call.At.IsZero() && now.Sub(call.At) >= konst.SubAgentStallMillis*time.Millisecond
}

func (m *Model) turnLines() []string {
	var lines []string
	if line := m.requestLine(); line != "" {
		lines = append(lines, line)
	}
	if m.AskedToStop() {
		lines = append(lines, margin+look.Style(look.Amber).Render("this will stop "+subAgentCount(m.stopAsked)+stopAskTail))
	}
	for _, row := range m.SubAgents {
		at := slices.IndexFunc(row.Calls, func(call subagent.Call) bool { return row.State == roster.Working && Stalled(call, m.now()) })
		if at < 0 {
			continue
		}
		call := row.Calls[at]
		open := requestSeparator + "open " + widget.Until(m.now().Sub(call.At))
		lines = append(lines, widget.Fit(margin+look.AgentRef(row.Name)+" "+look.Style(look.Amber).Render(stalledWord)+" "+look.Muted(oneLine(call.Tool+" "+call.Text)+open), m.width))
	}
	return lines
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
		line += look.Accent(progress.Spin(m.frame)) + " " + style.Render(word) + look.Muted(requestSeparator+widget.Until(m.phaseSince())+metaGap)
		id = m.turnID
	case !m.leadIdleSince.IsZero():
		line += look.Accent(progress.Spin(m.frame)) + " " + look.Style(look.Violet).Render(m.leadIdleWords()) + look.Muted(requestSeparator+widget.Until(max(m.now().Sub(m.leadIdleSince), 0))+metaGap)
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

func (m *Model) Spawned(name string) {
	for index := len(m.entries) - 1; index >= 0 && !m.entries[index].message(); index-- {
		if batch := &m.entries[index]; len(batch.SubAgents) > 0 {
			batch.SubAgents = append(batch.SubAgents, name)
			m.revision++
			return
		}
	}
	m.Append(Entry{Kind: Note, SubAgents: []string{name}})
}

func (m *Model) Reported(id string, row subagent.Row) {
	m.Append(Entry{
		Kind: Tool, ID: id, Head: reportHead, Body: "[&" + row.Name + "]", Status: row.State.String(), Detail: row.Report,
		Failed: row.State == roster.Errored, Promoted: true,
	})
}

type spawnBatch struct {
	running, settled []subagent.Row
}

func (m *Model) batchOf(names []string) spawnBatch {
	var batch spawnBatch
	for _, name := range names {
		at := slices.IndexFunc(m.SubAgents, func(row subagent.Row) bool { return row.Name == name })
		switch {
		case at < 0:
		case settledMark(m.SubAgents[at].State) == "":
			batch.running = append(batch.running, m.SubAgents[at])
		default:
			batch.settled = append(batch.settled, m.SubAgents[at])
		}
	}
	return batch
}

func settledMark(state roster.State) string {
	switch state {
	case roster.Working, roster.Reopened, roster.WaitingAnswer:
		return ""
	case roster.InReview, roster.Finished:
		return doneMark
	case roster.Errored:
		return failedMark
	case roster.Parked:
		return stoppedMark
	}
	panic("session: unknown sub-agent state")
}

func (m *Model) batchLines(batch spawnBatch) []string {
	var lines []string
	if len(batch.running) > 0 {
		word, since, names := runningWord, time.Duration(0), []string(nil)
		for _, row := range batch.running {
			if len(row.Calls) == 0 {
				word = spawningWord
			}
			since = max(since, row.Since)
			names = append(names, look.AgentRef(row.Name))
			if row.State == roster.Working && row.Report != "" {
				names = append(names, look.Muted(row.Report))
			}
		}
		head := look.Style(look.Violet).Render(progress.Work(m.frame) + " " + word)
		lines = m.wordRows(slices.Concat([]string{head}, names, []string{" " + look.Muted(widget.Until(since))}))
	}
	var words []string
	for _, mark := range []string{doneMark, failedMark, stoppedMark} {
		colour, named := look.FaintColor, look.Mint
		switch mark {
		case doneMark:
			named = look.MintMuted
		case failedMark:
			colour = look.Red
		}
		lead := look.Style(colour).Render(mark)
		if words != nil {
			lead = " " + lead
		}
		for _, row := range batch.settled {
			if settledMark(row.State) == mark {
				words = append(words, lead+look.AgentRefIn(named, row.Name))
				lead = ""
			}
		}
	}
	if words != nil {
		lines = append(lines, m.wordRows(words)...)
	}
	return lines
}

func (m *Model) wordRows(words []string) []string {
	var rows []string
	row := ""
	for _, word := range words {
		switch {
		case row == "":
			row = word
		case widget.Cells(row)+1+widget.Cells(word) > m.textWidth():
			rows = append(rows, row)
			row = wrapIndent + word
		default:
			row += " " + word
		}
	}
	return append(rows, row)
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
		if entry := &m.entries[index]; entry.Decision != nil && entry.Decision.Verdict == Ask {
			entry.asking, entry.asked = awaiting, entry.asked || awaiting
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
