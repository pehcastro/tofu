package session

import (
	"strconv"
	"strings"
	"time"

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
	width, lead, body                                           int
	status                                                      string
	latest, picked, streaming, waiting, failed, promoted, gated bool
	fold                                                        foldCounts
}

type drawn struct {
	key   drawKey
	lines []string
}

func (m *Model) View() string {
	m.settle()
	plan, rows := m.feed()
	lines := m.transcript(rows)
	for _, line := range plan {
		lines = append(lines, widget.Pad(continuation+line, m.width))
	}
	return strings.Join(append(lines, m.footer()...), "\n")
}

func (m *Model) transcript(rows int) []string {
	total, fromTop, from := m.scrollMetrics(rows)
	lines := m.linesFrom(from, rows)
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
			lines = append(lines, m.progressLine(m.entries[end-1]))
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
		width: m.width, lead: m.lead(start), body: len(entry.Body), status: entry.Status,
		streaming: entry.streaming, waiting: entry.waiting, failed: entry.Failed,
		promoted: entry.Promoted, gated: entry.Decision != nil,
	}
	if m.folds(start) {
		key.fold = m.foldCounts(start, end)
		return key, m.liveFold(start, end)
	}
	key.latest = m.latestUser(start)
	key.picked = entry.waiting && entry.ID == m.pickedQueue()
	return key, entry.running()
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
	case m.entries[start].message():
		return turnGapRows
	case m.entries[start-1].message():
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
	text := noteMarker + strings.Join(fields, foldSeparator)
	if fold.id == "" {
		return continuation + look.Faint(widget.Fit(text, m.textWidth()))
	}
	id := look.TypedID(toolKind, trace.Short(fold.id))
	room := max(m.textWidth()-widget.Cells(foldSeparator)-widget.Cells(id), 1)
	return continuation + look.Faint(widget.Fit(text, room)+foldSeparator) + id
}

func (m *Model) foldSince(end int) time.Duration {
	if end >= len(m.entries) {
		return m.elapsed(m.began)
	}
	return m.entries[end].intoTurn
}

func (m *Model) progressLine(entry Entry) string {
	line := progress.Line{Label: entry.label(), Frame: m.frame, Live: entry.running()}
	id := trace.Short(entry.ID)
	if id == "" {
		return continuation + line.View(m.textWidth())
	}
	typed := look.TypedID(toolKind, id)
	room := max(m.textWidth()-widget.Cells(typed)-widget.Cells(gap), 1)
	return continuation + widget.Pad(line.View(room), room) + gap + typed
}

func (m *Model) render(index int) []string {
	entry := m.entries[index]
	switch entry.Kind {
	case User:
		lines := strings.Split(look.Style(look.Text).Render(strings.Join(widget.Wrap(entry.Body, m.textWidth()), "\n")), "\n")
		if entry.waiting {
			lines = []string{look.Muted(widget.Fit(strings.Join(strings.Fields(entry.Body), " "), m.textWidth()))}
		}
		return m.message(entry, look.Title(you), append(lines, m.chipLines(entry.Chips)...), m.latestUser(index))
	case Assistant:
		return m.message(entry, look.AgentRef(orchestrator), entry.displayLines(), false)
	case Tool:
		return indented(m.toolLines(entry))
	case Note:
		lines := widget.Wrap(entry.Body, max(m.textWidth()-widget.Cells(noteMarker), 1))
		for index, line := range lines {
			marker := noteMarker
			if index > 0 {
				marker = strings.Repeat(" ", widget.Cells(noteMarker))
			}
			lines[index] = look.Faint(marker + line)
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
	return look.Style(look.Red).Render(failureMarker+widget.Fit(strings.Join(strings.Fields(entry.Body), " "), room)) + tail
}

func (m *Model) toolLines(entry Entry) []string {
	width := m.textWidth()
	style := look.Style(look.Blue)
	if entry.Head == shellTool {
		style = look.Style(look.Amber)
	}
	verdict, verdictStyle := "", style
	if entry.Decision != nil {
		verdict, verdictStyle = entry.Decision.Verdict.String(), entry.Decision.Verdict.style()
	}
	status, statusStyle := entry.Status, style
	switch {
	case entry.running():
		status, statusStyle = widget.Until(m.elapsed(entry.Started)), look.Style(look.Mint)
	case entry.Failed:
		statusStyle = look.Style(look.Red)
	case status != "":
		statusStyle = look.Style(look.Mint)
	}
	status = widget.Fit(status, max(width/statusShare-widget.Cells(verdict)-widget.Cells(gap), 0))
	right, columns := "", 0
	if verdict != "" {
		right, columns = verdictStyle.Render(verdict), widget.Cells(verdict)
	}
	if status != "" {
		if right != "" {
			right, columns = right+gap, columns+widget.Cells(gap)
		}
		right, columns = right+statusStyle.Render(status), columns+widget.Cells(status)
	}
	room := max(width-columns-widget.Cells(gap), 1)
	lines := []string{style.Render(widget.Pad(widget.Fit(toolMarker+entry.label(), room), room)) + gap + right}
	if entry.Detail != "" {
		for _, line := range widget.Wrap(entry.Detail, max(width-widget.Cells(continuation), 1)) {
			lines = append(lines, look.Faint(continuation+line))
		}
	}
	if entry.Decision != nil {
		lines = append(lines, entry.Decision.lines(width)...)
	}
	if short := trace.Short(entry.ID); short != "" {
		lines = append(lines, continuation+look.TypedID(toolKind, short))
	}
	return lines
}
