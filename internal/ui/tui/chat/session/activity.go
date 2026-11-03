package session

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	roster "tofu/internal/agent/subagents"
	"tofu/internal/cli/render"
	"tofu/internal/ui/tui/look/look"
	"tofu/internal/ui/tui/panels/progress"
	"tofu/internal/ui/tui/panels/subagent"
	"tofu/internal/ui/tui/panels/trace"
)

const (
	stoppingWord = "stopping"
	spawningWord = "spawning"
	workingWord  = "working"
	reopenedWord = "reopened"

	waitingAnswerWords = "waiting for answer"
	readingWords       = "reading the reports"
	leadIdleHead       = "waiting"
	stalledWord        = "stalled"
	stopAskTail        = ", press Ctrl+C again to confirm"
	wrapIndent         = "  "
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
	switch {
	case m.Awaiting():
		return waitingOnYou
	case m.inFlight() && !m.respondedOnce:
		return requesting
	case m.leadThinks:
		return thinking
	case len(m.Spawning) > 0 && !slices.ContainsFunc(m.entries, Entry.running):
		return spawning
	}
	return working
}

func (m *Model) Thinks() { m.leadThinks = m.Busy }

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
	words := leadIdleHead + requestSeparator + subAgentCount(m.waitingOn)
	for index, count := range m.activityCounts() {
		doing, phrase := Doing(index), Doing(index).words()
		if doing == RunningBash {
			phrase = "in bash"
		}
		if count > 0 && doing != RunningTool {
			words += foldSeparator + strconv.Itoa(count) + " " + phrase
		}
	}
	return words
}

func (m *Model) leadIdle() bool { return !m.Busy && !m.leadIdleSince.IsZero() }

func (m *Model) AskToStop(subAgents int, until time.Time) {
	m.stopAsked, m.stopAskedUntil = subAgents, until
}

func (m *Model) AskedToStop() bool { return m.now().Before(m.stopAskedUntil) }

type Doing int

const (
	WaitingOnLead Doing = iota
	NoProgress
	Building
	RunningBash
	RunningTool
)

type Activity struct {
	Name  string
	Doing Doing
}

func (d Doing) drawn(count int) string {
	agents := "(" + strconv.Itoa(count) + ") agents "
	if count == 1 {
		agents = "(1) agent "
	}
	style := look.Style(look.Violet)
	if d == NoProgress {
		style = look.Style(look.Amber)
	}
	return style.Render(agents + d.words())
}

func (d Doing) words() string {
	switch d {
	case WaitingOnLead:
		return "waiting for an answer"
	case NoProgress:
		return stalledWord
	case Building:
		return "building"
	case RunningBash:
		return "running bash"
	case RunningTool:
		return workingWord
	}
	panic("session: unknown sub-agent activity")
}

func (m *Model) activityCounts() [RunningTool + 1]int {
	var counts [RunningTool + 1]int
	for _, row := range m.Activity {
		counts[row.Doing]++
	}
	return counts
}

func (m *Model) activityLine() string {
	var parts []string
	for doing, count := range m.activityCounts() {
		if count > 0 {
			parts = append(parts, Doing(doing).drawn(count))
		}
	}
	return widget.Fit(margin+strings.Join(parts, look.Muted(foldSeparator)), m.width)
}

func (m *Model) turnLines() []string {
	var lines []string
	if line := m.requestLine(); line != "" || m.Notice != "" {
		lines = append(lines, m.withNotice(cmp.Or(line, margin)))
	}
	if m.AskedToStop() {
		lines = append(lines, margin+look.Style(look.Amber).Render("this will stop "+subAgentCount(m.stopAsked)+stopAskTail))
	}
	if len(m.Activity) > 0 && !m.leadIdle() {
		lines = append(lines, m.activityLine())
	}
	return lines
}

func (m *Model) withNotice(line string) string {
	room := m.width - widget.Cells(line) - widget.Cells(metaGap) - composerPadX
	if m.Notice == "" || room < 1 {
		return line
	}
	notice := widget.Fit(m.Notice, room)
	return line + strings.Repeat(" ", m.width-composerPadX-widget.Cells(line)-widget.Cells(notice)) + notice
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
	line, id := margin, ""
	switch {
	case m.Busy:
		word, style := m.phase.drawn()
		if m.Stopping || m.LettingToolsFinish {
			word, style = stoppingWord, look.Style(look.Amber)
		}
		line += look.Accent(progress.Spin(m.frame)) + " " + style.Render(word) + look.Muted(requestSeparator+widget.Until(m.phaseSince()))
		if m.requestID == "" {
			return line
		}
		line, id = line+look.Muted(metaGap), m.requestID
	case m.leadIdle():
		line += look.Accent(progress.Spin(m.frame)) + " " + look.Style(look.Violet).Render(m.leadIdleWords()) + look.Muted(requestSeparator+widget.Until(max(m.now().Sub(m.leadIdleSince), 0))+metaGap)
	case m.cooked != "":
		line, id = line+look.Muted(m.cooked+metaGap), m.cookedID
	default:
		return ""
	}
	tag := ""
	if short := trace.Short(id); short != "" {
		tag = look.TypedID(requestKind, short)
	}
	return line + tag
}

func (m *Model) Requested(id string) {
	m.requestID = id
}

func (m *Model) Spawn(id string) {
	m.Spawning = append(m.Spawning, id)
}

func (m *Model) Spawned(name string) {
	for index := len(m.entries) - 1; index >= 0 && !m.entries[index].message(); index-- {
		if batch := &m.entries[index]; len(batch.SubAgents) > 0 {
			batch.SubAgents = append(batch.SubAgents, name)
			m.revision++
			return
		}
	}
	at := len(m.entries)
	for at > 0 && m.folds(at-1) {
		at--
	}
	m.entries = slices.Insert(m.entries, at, Entry{Kind: Note, SubAgents: []string{name}, Started: m.now(), turn: m.turns})
	if !m.following && m.top.entry >= at {
		m.top.entry++
	}
	m.revision++
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
		case settled(m.SubAgents[at].State):
			batch.settled = append(batch.settled, m.SubAgents[at])
		default:
			batch.running = append(batch.running, m.SubAgents[at])
		}
	}
	return batch
}

func settled(state roster.State) bool {
	switch state {
	case roster.Working, roster.Reopened, roster.WaitingAnswer:
		return false
	case roster.InReview, roster.Finished, roster.Errored, roster.Parked:
		return true
	}
	panic("session: unknown sub-agent state")
}

func (m *Model) batchLines(batch spawnBatch) []string {
	var lines, statuses []string
	named := map[string][]string{}
	for _, row := range batch.running {
		status := subAgentStatus(row)
		if named[status] == nil {
			statuses = append(statuses, status)
		} else {
			last := len(named[status]) - 1
			named[status][last] += look.Muted(",")
		}
		named[status] = append(named[status], look.AgentRef(row.Name))
	}
	for _, status := range statuses {
		head := look.Style(look.Violet).Render(progress.Work(m.frame)+" "+status) + look.Muted(" ·")
		lines = append(lines, m.wordRows(append([]string{head}, named[status]...))...)
	}
	return lines
}

func subAgentStatus(row subagent.Row) string {
	switch row.State {
	case roster.WaitingAnswer:
		return waitingAnswerWords
	case roster.Reopened:
		return reopenedWord
	}
	return workingWord
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

func (m *Model) AskedID() string {
	if len(m.asks) == 0 {
		return ""
	}
	return m.asks[0]
}

func (m *Model) markAsked() {
	m.revision++
	for index := range m.entries {
		entry := &m.entries[index]
		entry.asking = entry.askID != "" && entry.askID == m.AskedID()
		entry.asked = entry.asked || entry.asking
	}
}

func (m *Model) Await(id, tool, place string, decision *Decision) {
	if slices.Contains(m.asks, id) {
		return
	}
	if id == "" {
		id = m.mint()
		if at := slices.IndexFunc(m.entries, func(entry Entry) bool {
			return entry.askID == "" && entry.Decision != nil && entry.Decision.Verdict == Ask && !entry.returned()
		}); at >= 0 {
			m.entries[at].askID = id
		}
	}
	if !slices.ContainsFunc(m.entries, func(entry Entry) bool { return entry.askID == id }) {
		asked := Decision{Tool: tool, Verdict: Ask}
		if decision != nil {
			asked = *decision
		}
		m.Append(Entry{Kind: Tool, Head: tool, Body: strings.TrimPrefix(place, tool+" "), Decision: &asked, askID: id})
	}
	if m.asks = append(m.asks, id); len(m.asks) == 1 {
		m.waiting = m.now()
	}
	m.markAsked()
}

func (m *Model) Resume(id string) {
	at := slices.Index(m.asks, cmp.Or(id, m.AskedID()))
	if at < 0 {
		return
	}
	m.asks = slices.Delete(m.asks, at, at+1)
	m.markAsked()
	if len(m.asks) > 0 {
		return
	}
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
	m.seal()
	if !m.Busy {
		return
	}
	m.requested, m.answered = m.now(), time.Time{}
}

func (m *Model) Returned() {
	m.leadThinks = false
	if !m.inFlight() {
		return
	}
	m.answered = m.now()
	m.respondedOnce = true
	waited := m.answered.Sub(m.requested)
	m.waited += waited
	m.hold(waited)
}
