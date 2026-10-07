package session

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/progress"
	"tofu/interface/tui/trace"
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
	requestKind      = "request"
	toolMarker       = "⟩ "
	noteMarker       = "· "
	failureMarker    = "! "
	continuation     = "    "
	statusShare      = 2
	foldSeparator    = " · "
	wholeErrorInWork = "the whole error is in work "
)

type foldCounts struct {
	tools, jev, shell int
	since             time.Duration
	id                string
}

type drawKey struct {
	width, lead, body                                    int
	status, verdict, spawn                               string
	latest, picked, streaming, waiting, failed, promoted bool
	fold                                                 foldCounts
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

func (m *Model) blockLines(start, end int) []string {
	key, live := m.drawKey(start, end)
	cached := &m.entries[start].drawn
	if !live && cached.lines != nil && cached.key == key {
		return cached.lines
	}
	lines := make([]string, key.lead, key.lead+1)
	if m.folds(start) {
		fold, progress := m.foldShows(start, end)
		if fold {
			lines = append(lines, m.foldLine(key.fold))
		}
		if progress {
			lines = append(lines, m.progressLine(m.entries[start:end]))
		}
	} else {
		lines = append(lines, m.render(start)...)
	}
	for index, line := range lines {
		lines[index] = widget.Pad(line, m.width-trackCells)
	}
	if !live {
		*cached = drawn{key: key, lines: lines}
	}
	return lines
}

func (m *Model) drawKey(start, end int) (drawKey, bool) {
	entry := &m.entries[start]
	key := drawKey{
		width: m.width, lead: m.lead(start), body: len(entry.Body), status: entry.Status, verdict: entry.verdictShown(),
		streaming: entry.streaming, waiting: entry.waiting, failed: entry.Failed, promoted: entry.Promoted,
	}
	if m.folds(start) {
		key.fold = m.foldCounts(start, end)
		return key, m.liveFold(start, end)
	}
	key.latest = m.latestUser(start)
	key.picked = entry.waiting && entry.ID == m.pickedQueue()
	if len(entry.SubAgents) == 0 {
		return key, entry.running()
	}
	batch := m.batchOf(entry.SubAgents)
	for _, row := range batch.settled {
		key.spawn += row.Name + " " + row.State.String() + " "
	}
	return key, len(batch.running) > 0
}

func (m *Model) liveFold(start, end int) bool { return end == len(m.entries) || m.stillRunning(start) }

func (m *Model) foldShows(start, end int) (bool, bool) {
	return !m.stillRunning(start), end == len(m.entries) && m.Busy && m.entries[end-1].label() != ""
}

func (m *Model) blockRows(start, end int) int {
	if !m.folds(start) || !m.liveFold(start, end) {
		return len(m.blockLines(start, end))
	}
	rows := m.lead(start)
	fold, progress := m.foldShows(start, end)
	if fold {
		rows++
	}
	if progress {
		rows++
	}
	return rows
}

func (m *Model) lead(start int) int {
	switch {
	case start == 0:
		return 0
	case m.entries[start].Kind == User:
		return turnGapRows
	case m.entries[start].message() || m.entries[start-1].message():
		return 1
	}
	return 0
}

func (m *Model) latestUser(index int) bool {
	if m.entries[index].Kind != User || m.entries[index].waiting {
		return false
	}
	for later := index + 1; later < len(m.entries); later++ {
		if m.entries[later].Kind == User && !m.entries[later].waiting {
			return false
		}
	}
	return true
}

func (m *Model) foldCounts(start, end int) foldCounts {
	fold := foldCounts{tools: end - start, since: m.foldSince(end), id: m.entries[end-1].ID}
	for index := start; index < end; index++ {
		if m.entries[index].Head == shellTool && !m.FoldHidesShell {
			fold.shell++
		}
		if m.entries[index].Decision != nil {
			fold.jev++
		}
	}
	return fold
}

func (m *Model) foldLine(fold foldCounts) string {
	fields := []string{"(" + strconv.Itoa(fold.tools) + ") tools"}
	if fold.jev > 0 {
		fields = append(fields, "jev "+strconv.Itoa(fold.jev))
	}
	if fold.shell > 0 {
		fields = append(fields, "shell ("+strconv.Itoa(fold.shell)+")")
	}
	fields = append(fields, widget.Until(fold.since))
	tail := expandTail(fold.id)
	room := max(m.textWidth()-widget.Cells(tail), 1)
	return continuation + look.Faint(widget.Pad(widget.Fit(noteMarker+strings.Join(fields, foldSeparator), room), room)) + tail
}

func (m *Model) foldSince(end int) time.Duration {
	if end >= len(m.entries) {
		return m.elapsed(m.began)
	}
	return m.entries[end].intoTurn
}

func (m *Model) progressLine(fold []Entry) string {
	latest := fold[len(fold)-1]
	tail := expandTail(latest.ID)
	room := max(m.textWidth()-widget.Cells(tail), 1)
	return continuation + widget.Pad(progress.Line{Label: oneLine(latest.label()), Frame: m.frame, Live: true}.View(room), room) + tail
}

func expandTail(id string) string {
	if short := trace.Short(id); short != "" {
		return gap + look.TypedID(toolKind, short) + expandMark(id)
	}
	return ""
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
		if entry.waiting {
			lines = []string{look.Muted(widget.Fit(oneLine(entry.Body), m.textWidth()))}
		}
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
		if entry.Head != "" && entry.ID != "" {
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
		return indented([]string{m.failureLine(entry)})
	}
	panic("session: unknown entry kind")
}

func (m *Model) message(entry Entry, label string, body []string, tinted bool) []string {
	width, clock := m.textWidth(), entry.Started.Format(clockLayout)
	meta := look.Faint(clock)
	if short := trace.Short(entry.ID); short != "" {
		meta = look.Faint(clock+metaGap) + look.TypedID(messageKind, short)
	}
	if entry.waiting {
		meta = look.Faint(waitingWord)
		if entry.ID == m.pickedQueue() {
			meta = look.Accent(pickedMarker + waitingWord)
		}
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

func indented(lines []string) []string {
	for index, line := range lines {
		lines[index] = continuation + line
	}
	return lines
}

func (m *Model) failureLine(entry Entry) string {
	tail := ""
	if short := trace.Short(entry.ID); short != "" {
		tail = gap + look.Faint(wholeErrorInWork) + look.TypedID(toolKind, short)
	}
	room := max(m.textWidth()-widget.Cells(failureMarker+tail), 1)
	return look.Style(look.Red).Render(failureMarker+widget.Fit(oneLine(entry.Body), room)) + tail
}

func (m *Model) toolLine(entry Entry) string {
	style := look.Style(look.Blue)
	if entry.Head == shellTool {
		style = look.Style(look.Amber)
	}
	status, statusStyle := m.callStatus(entry)
	marker := toolMarker
	if entry.running() {
		marker = progress.Work(m.frame) + " "
	}
	right := expandMark(entry.ID)
	if status = widget.Fit(oneLine(status), max(m.textWidth()/statusShare-widget.Cells(right), 0)); status != "" {
		right = gap + statusStyle.Render(status) + right
	}
	if word := entry.verdictShown(); word != "" {
		right = gap + verdictStyle(entry.Decision.Verdict).Render(word) + right
	}
	room := max(m.textWidth()-widget.Cells(right), 1)
	return style.Render(widget.Pad(widget.Fit(marker+oneLine(entry.label()), room), room)) + right
}
