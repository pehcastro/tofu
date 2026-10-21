package session

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/progress"
	"tofu/interface/tui/trace"
	"tofu/internal/transport"
	"tofu/internal/widget"
)

const (
	margin           = "  "
	messageMargin    = len(margin)
	messageInset     = len(continuation)
	trackCells       = 1
	turnGapRows      = 2
	metaGap          = "  "
	expandLabel      = "[expand]"
	clockLayout      = "15:04"
	you              = "You"
	orchestrator     = "orchestrator"
	messageKind      = "message"
	toolKind         = "tool"
	spawnKind        = "spawn"
	requestKind      = "request"
	toolMarker       = "⟩ "
	noteMarker       = "· "
	failureMarker    = "! "
	continuation     = "    "
	statusShare      = 2
	foldSeparator    = " · "
	wholeErrorInWork = "the whole error is in work "
)

type drawKey struct {
	width, lead, body                   int
	status, verdict, spawn              string
	latest, streaming, failed, promoted bool
}

type drawn struct {
	key   drawKey
	lines []string
}

func (m *Model) View() string {
	m.settle()
	return strings.Join(append(m.transcript(m.transcriptRows()), m.footer()...), "\n")
}

func (m *Model) transcript(rows int) []string {
	total, fromTop, from := m.scrollMetrics(rows)
	lines := m.linesFrom(from, rows)
	if len(m.entries) == 0 && m.Welcome != nil {
		lines = m.Welcome(m.width-trackCells, rows)
	}
	blank := strings.Repeat(" ", m.width-trackCells)
	for len(lines) < rows {
		lines = append(lines, blank)
	}
	track := strings.Split(look.ScrollTrack(rows, total, rows, fromTop), "\n")
	for index := range lines {
		cell := " "
		if len(track) == rows {
			cell = track[index]
		}
		lines[index] += cell
	}
	return lines
}

func (m *Model) scrollMetrics(rows int) (int, int, anchor) {
	from, scrollable := m.tailAnchor(rows)
	if scrollable && !m.following {
		from = m.top
	}
	return m.offset(anchor{entry: len(m.entries)}), m.offset(from), from
}

func (m *Model) blockLines(start int) []string {
	if m.folds(start) {
		return nil
	}
	key, live := m.drawKey(start)
	cached := &m.entries[start].drawn
	if !live && cached.lines != nil && cached.key == key {
		return cached.lines
	}
	lines := append(make([]string, key.lead, key.lead+1), m.render(start)...)
	for index, line := range lines {
		lines[index] = widget.Pad(line, m.width-trackCells)
	}
	if !live {
		*cached = drawn{key: key, lines: lines}
	}
	return lines
}

func (m *Model) drawKey(start int) (drawKey, bool) {
	entry := &m.entries[start]
	key := drawKey{
		width: m.width, lead: m.lead(start), body: len(entry.Body), status: entry.Status, verdict: entry.verdictShown(),
		streaming: entry.streaming, failed: entry.Failed, promoted: entry.Promoted, latest: m.latestUser(start),
	}
	if len(entry.SubAgents) == 0 {
		return key, entry.running()
	}
	batch := m.batchOf(entry.SubAgents)
	for _, row := range batch.settled {
		key.spawn += row.Name + " " + row.State.String() + " "
	}
	return key, len(batch.running) > 0
}

func (m *Model) lead(start int) int {
	before := start - 1
	for before >= 0 && m.folds(before) {
		before--
	}
	switch {
	case before < 0:
		return 0
	case m.entries[start].Kind == User:
		return turnGapRows
	case m.entries[start].message() || m.entries[before].message():
		return 1
	}
	return 0
}

func (m *Model) latestUser(index int) bool {
	if m.entries[index].Kind != User {
		return false
	}
	for later := index + 1; later < len(m.entries); later++ {
		if m.entries[later].Kind == User {
			return false
		}
	}
	return true
}

func (m *Model) turnWork() (string, string) {
	var tools, jev, shell int
	var named *Entry
	for index := len(m.entries) - 1; index >= 0 && m.entries[index].turn == m.turns; index-- {
		entry := &m.entries[index]
		if !m.folds(index) {
			continue
		}
		if named == nil && cmp.Or(entry.Ended, m.now()).Sub(entry.Started) >= PhaseDwell {
			named = entry
		}
		tools++
		if entry.Head == shellTool && !m.FoldHidesShell {
			shell++
		}
		if entry.Decision != nil {
			jev++
		}
	}
	kind, spawn := toolKind, &m.latestSpawn
	waitsOnSpawn := slices.Contains(m.Spawning, spawn.ID) && (named == nil || !named.running())
	if spawn.ID != "" && spawn.turn == m.turns && (named == nil || waitsOnSpawn || spawn.Started.After(named.Started)) {
		named, kind = spawn, spawnKind
	}
	if tools == 0 && named == nil {
		return "", ""
	}
	var fields []string
	tag := ""
	if named != nil {
		fields = []string{oneLine(named.label())}
		if short := m.FeedID(named.ID); short != "" {
			tag = look.TypedID(kind, short)
		}
	}
	if tools > 0 {
		fields = append(fields, "("+strconv.Itoa(tools)+") tools")
	}
	if jev > 0 {
		fields = append(fields, "jev "+strconv.Itoa(jev))
	}
	if shell > 0 {
		fields = append(fields, "shell ("+strconv.Itoa(shell)+")")
	}
	return strings.Join(fields, foldSeparator), tag
}

func expandMark(id string) string {
	if trace.Short(id) == "" {
		return ""
	}
	return gap + look.Style(look.Violet).Render(expandLabel)
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

func (m *Model) callStatus(entry Entry) (string, lipgloss.Style) {
	switch {
	case entry.running():
		return widget.Until(m.elapsed(entry.Started)), look.Style(look.Mint)
	case entry.Failed:
		return entry.Status, look.Style(look.Red)
	}
	return entry.Status, look.Style(look.Mint)
}

func (m *Model) render(index int) []string {
	if !m.wrapped(&m.entries[index]) {
		m.rerender(&m.entries[index])
	}
	entry := m.entries[index]
	switch entry.Kind {
	case User:
		lines := strings.Split(look.Style(look.Text).Render(strings.Join(widget.Wrap(entry.Body, m.textWidth()), "\n")), "\n")
		return m.message(entry, look.Title(you), append(lines, m.chipLines(entry.Chips)...), m.latestUser(index))
	case Assistant:
		return m.message(entry, look.AgentRef(orchestrator), entry.displayLines(), false)
	case Tool:
		return []string{continuation + m.toolLine(entry)}
	case Note:
		if len(entry.SubAgents) > 0 {
			return indented(m.batchLines(m.batchOf(entry.SubAgents)))
		}
		reference := ""
		switch {
		case entry.Head == FiredHead:
			reference = expandMark(entry.ID)
		case entry.Head != "" && entry.ID != "":
			reference = gap + look.TypedID(entry.Head, entry.ID)
		}
		lines := widget.Wrap(entry.Body, max(m.textWidth()-widget.Cells(noteMarker)-widget.Cells(reference), 1))
		for index, line := range lines {
			marker := noteMarker
			if index > 0 {
				marker = strings.Repeat(" ", widget.Cells(noteMarker))
			}
			lines[index] = look.Faint(marker + line)
		}
		if reference != "" && len(lines) > 0 {
			lines[0] += reference
		}
		return indented(lines)
	case Failure:
		return indented(m.failureLines(entry))
	}
	panic("session: unknown entry kind")
}

func (m *Model) message(entry Entry, label string, body []string, tinted bool) []string {
	width, clock := m.textWidth(), entry.Started.Format(clockLayout)
	meta := look.Faint(clock)
	if short := trace.Short(entry.shownID()); short != "" {
		meta = look.Faint(clock+metaGap) + look.TypedID(messageKind, short)
	}
	lines := []string{label, meta}
	if widget.Cells(label)+widget.Cells(meta)+len(metaGap) <= width {
		lines = []string{look.Sides(label, meta, width)}
	}
	lines = append(lines, body...)
	if !tinted {
		return indented(lines)
	}
	surface := strings.Split(look.TintedSurface(m.width-2*messageMargin, look.Panel, strings.Join(lines, "\n")), "\n")
	for index, line := range surface {
		surface[index] = margin + line
	}
	return surface
}

func (e Entry) shownID() string {
	if e.Kind == Assistant {
		return cmp.Or(e.recorded, e.ID)
	}
	return e.recorded
}

func indented(lines []string) []string {
	for index, line := range lines {
		lines[index] = continuation + line
	}
	return lines
}

func (m *Model) failureLines(entry Entry) []string {
	tail := ""
	if short := m.FeedID(entry.ID); short != "" {
		tail = gap + look.Faint(wholeErrorInWork) + look.TypedID(toolKind, short)
	}
	said, sentenced := transport.Sentenced(entry.Body)
	if !sentenced {
		room := max(m.textWidth()-widget.Cells(failureMarker+tail), 1)
		return []string{look.Style(look.Red).Render(failureMarker+widget.Fit(oneLine(entry.Body), room)) + tail}
	}
	room := max(m.textWidth()-widget.Cells(failureMarker), 1)
	lines := widget.Wrap(said, room)
	last := len(lines) - 1
	for index, line := range lines {
		marker := failureMarker
		if index > 0 {
			marker = strings.Repeat(" ", widget.Cells(failureMarker))
		}
		lines[index] = look.Style(look.Red).Render(marker + line)
	}
	if widget.Cells(lines[last])+widget.Cells(tail) > room+widget.Cells(failureMarker) {
		return append(lines, strings.Repeat(" ", widget.Cells(failureMarker))+strings.TrimPrefix(tail, gap))
	}
	lines[last] += tail
	return lines
}

func (m *Model) toolLine(entry Entry) string {
	style := look.Style(look.Blue)
	if entry.Head == shellTool {
		style = look.Style(look.Amber)
	}
	status, statusStyle := m.callStatus(entry)
	label := entry.label()
	if entry.Head == reportHead {
		label, status = entry.Body, ""
	}
	marker := toolMarker
	if entry.running() {
		marker = progress.Work(m.frame) + " "
	}
	right := expandMark(entry.ID)
	if short := trace.Short(entry.recorded); short != "" {
		right = gap + look.TypedID(messageKind, short) + right
	}
	if status = widget.Fit(oneLine(status), max(m.textWidth()/statusShare-widget.Cells(right), 0)); status != "" {
		right = gap + statusStyle.Render(status) + right
	}
	if word := entry.verdictShown(); word != "" {
		right = gap + verdictStyle(entry.Decision.Verdict).Render(word) + right
	}
	room := max(m.textWidth()-widget.Cells(right), 1)
	return style.Render(widget.Pad(widget.Fit(marker+oneLine(label), room), room)) + right
}
