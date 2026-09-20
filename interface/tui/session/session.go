package session

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tofu/interface/tui/crew"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const TickInterval = 250 * time.Millisecond

const PhaseDwell = 400 * time.Millisecond

const (
	entryWindow    = 500
	composerRows   = 3
	ruleRows       = 1
	footerRows     = composerRows + 2
	placeholder    = "what should tofu do here?"
	keyHints       = "⏎ send   ⇧⏎ newline   / commands"
	queueHints     = "⏎ queues   alt+↑↓ picks   ctrl+x unqueues"
	hintGap        = "   "
	quitHint       = "ctrl+c quit"
	stopHint       = "ctrl+c stops the turn"
	stoppingHint   = "stopping the turn, ctrl+c will not quit until it ends"
	noResult       = "no result"
	continuation   = "    "
	toolMarker     = "⟩ "
	assistantMark  = "▌ "
	userMarker     = "» "
	noteMarker     = "· "
	failureMarker  = "! "
	minimumColumns = 20
	statusShare    = 2
	foldFrom       = 2
	foldSeparator  = " · "
	countSeparator = ", "
	wheelLines     = 3
	followingState = "following"
	scrolledState  = "scrolled back   end returns"
)

type Kind int

const (
	User Kind = iota
	Assistant
	Tool
	Note
	Failure
)

type Entry struct {
	Kind      Kind
	ID        string
	Head      string
	Body      string
	Detail    string
	Status    string
	Bytes     int
	Failed    bool
	Started   time.Time
	Ended     time.Time
	Decision  *Decision
	streaming bool
	waiting   bool
	rendered  []string
}

type Result struct {
	Status string
	Bytes  int
	Failed bool
}

func (e Entry) markdown() bool { return e.Kind == Assistant && !e.streaming }

func (e Entry) running() bool { return e.Kind == Tool && e.ID != "" && e.Status == "" }

func (e Entry) sticky() bool {
	return e.Failed || (e.Decision != nil && e.Decision.Verdict != Allow)
}

func (e Entry) label() string { return strings.TrimSpace(e.Head + " " + e.Body) }

type Prose func(source string, width int) []string

type Model struct {
	Busy      bool
	Stopping  bool
	Commands  []Command
	Paths     []string
	Children  []crew.Child
	now       func() time.Time
	waiting   time.Time
	requested time.Time
	answered  time.Time
	waited    time.Duration
	phase     phase
	intent    string
	shown     time.Time
	prose     Prose
	plan      []PlanItem
	entries   []Entry
	composer  textarea.Model
	width     int
	height    int
	top       anchor
	following bool
	open      bool
	began     time.Time
	attached  []paste.Outcome
	pastes    int
	picked    int
	closed    bool
	queue     []pending
	queues    int
	pick      int
}

func (m *Model) Paste(board paste.Board) tea.Cmd {
	m.pastes++
	m.attached = append(m.attached, paste.Outcome{Index: m.pastes, State: paste.Working})
	return board.Attach(m.pastes)
}

func (m *Model) Attached(outcome paste.Outcome) {
	at := slices.IndexFunc(m.attached, func(row paste.Outcome) bool { return row.Index == outcome.Index })
	if at < 0 {
		return
	}
	if outcome.State == paste.Textual {
		m.attached = slices.Delete(m.attached, at, at+1)
		m.composer.InsertString(outcome.Text)
		return
	}
	m.attached[at] = outcome
}

func New(now func() time.Time, prose Prose) Model {
	composer := textarea.New()
	composer.Placeholder = placeholder
	composer.ShowLineNumbers = false
	composer.Prompt = "▏ "
	composer.SetHeight(composerRows)
	composer.CharLimit = 0
	composer.SetVirtualCursor(false)
	composer.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	return Model{now: now, prose: prose, composer: composer, following: true, began: now()}
}

func (m *Model) Focus() tea.Cmd { return m.composer.Focus() }

func (m Model) Cursor() *tea.Cursor {
	caret := m.composer.Cursor()
	if caret == nil {
		return nil
	}
	caret.Y += m.transcriptRows() + len(m.activityRows()) + ruleRows + len(m.attached)
	return caret
}

func (m *Model) SetSize(width, height int) {
	columns := max(width, minimumColumns)
	rewrap := columns != m.width
	m.width, m.height = columns, height
	m.composer.SetWidth(m.width)
	m.composer.SetHeight(composerRows)
	if !rewrap {
		return
	}
	for index := range m.entries {
		m.rerender(&m.entries[index])
	}
}

func (m Model) rerender(entry *Entry) {
	if entry.markdown() {
		entry.rendered = m.prose(entry.Body, max(m.width-widget.Cells(assistantMark), 1))
	}
}

func (m *Model) Stream(text string) {
	last := len(m.entries) - 1
	if last >= 0 && m.entries[last].streaming {
		m.entries[last].Body += text
		return
	}
	m.Append(Entry{Kind: Assistant, Body: text, streaming: true})
}

func (m *Model) seal() {
	last := len(m.entries) - 1
	if last < 0 || !m.entries[last].streaming {
		return
	}
	m.entries[last].streaming = false
	m.rerender(&m.entries[last])
}

func (m *Model) Append(entry Entry) {
	m.seal()
	entry.Started = m.now()
	m.rerender(&entry)
	m.entries = append(m.entries, entry)
	if len(m.entries) <= entryWindow {
		return
	}
	dropped := len(m.entries) - entryWindow
	m.entries = m.entries[dropped:]
	m.top.entry -= dropped
	if m.top.entry < 0 {
		m.top = anchor{}
	}
}

func (m *Model) Finish(id string, result Result) {
	ended := m.now()
	for index := len(m.entries) - 1; index >= 0; index-- {
		entry := &m.entries[index]
		if entry.running() && entry.ID == id {
			entry.Status, entry.Bytes, entry.Failed, entry.Ended = result.Status, result.Bytes, result.Failed, ended
			return
		}
	}
	m.Append(Entry{Kind: Note, Body: result.Status})
}

func (m Model) LastAnswer() (string, bool) {
	for index := len(m.entries) - 1; index >= 0; index-- {
		if entry := m.entries[index]; entry.Kind == Assistant {
			return entry.Body, true
		}
	}
	return "", false
}

func (m Model) Intent(id string) string {
	for index := len(m.entries) - 1; index >= 0; index-- {
		if entry := m.entries[index]; entry.Kind == Tool && entry.ID == id {
			return entry.Body
		}
	}
	return ""
}

func (m Model) LastCall() (string, bool) {
	for index := len(m.entries) - 1; index >= 0; index-- {
		entry := m.entries[index]
		if entry.Kind != Tool {
			continue
		}
		lines := []string{entry.label()}
		if entry.Detail != "" {
			lines = append(lines, entry.Detail)
		}
		if entry.Status != "" {
			lines = append(lines, entry.Status)
		}
		return strings.Join(lines, "\n"), true
	}
	return "", false
}

func (m *Model) Decide(decision Decision) {
	for index := range m.entries {
		entry := &m.entries[index]
		if entry.Kind == Tool && entry.Decision == nil && entry.Head == decision.Tool {
			entry.Decision = &decision
			return
		}
	}
	m.Append(Entry{Kind: Tool, Head: decision.Tool, Decision: &decision})
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	before := m.composer.Value()
	composer, cmd := m.composer.Update(msg)
	m.composer = composer
	if m.composer.Value() != before {
		m.closed, m.picked = false, 0
	}
	return cmd
}

func (m Model) Value() string { return unescaped(strings.TrimSpace(m.composer.Value())) }

func unescaped(typed string) string {
	var built strings.Builder
	atWordStart := true
	for index := range len(typed) {
		char := typed[index]
		if atWordStart && char == escapePrefix[0] && escapesASigil(typed[index+1:]) {
			atWordStart = false
			continue
		}
		built.WriteByte(char)
		atWordStart = strings.IndexByte(wordBreaks, char) >= 0
	}
	return built.String()
}

func escapesASigil(rest string) bool {
	if strings.HasPrefix(rest, commandPrefix) {
		return true
	}
	for _, sigil := range pathSigils() {
		if strings.HasPrefix(rest, sigil) {
			return true
		}
	}
	return false
}

func (m *Model) Reset() {
	m.composer.Reset()
	m.attached = nil
	m.closed, m.picked = false, 0
}

func (m *Model) Follow() { m.following = true }

func (m *Model) ToggleOpen() { m.open = !m.open }

func (m *Model) Start() {
	at := m.now()
	m.Busy, m.Stopping, m.began, m.plan = true, false, at, nil
	m.waited, m.phase, m.intent, m.shown = 0, requesting, "", at
	m.requested, m.answered = at, time.Time{}
}

func (m *Model) Close(words string) {
	work := m.elapsed(m.began)
	line := words + " " + widget.Until(work)
	if m.waited > work {
		line += foldSeparator + "waited " + widget.Until(m.waited)
	}
	m.Append(Entry{Kind: Note, Body: line})
}

func (m *Model) Stop() {
	m.Busy, m.Stopping = false, false
	m.requested, m.answered = time.Time{}, time.Time{}
	m.seal()
	for index := range m.entries {
		if m.entries[index].running() {
			m.entries[index].Status, m.entries[index].Ended = noResult, m.now()
		}
	}
}

func (m *Model) View() string {
	m.settle()
	plan, rows := m.feed()
	tail, scrollable := m.tailAnchor(rows)
	from := tail
	if scrollable && !m.following {
		from = m.top
	}
	lines := slices.Concat(m.linesFrom(from, rows), plan)
	for len(lines) < rows+len(plan) {
		lines = append([]string{""}, lines...)
	}
	footer := append([]string{strings.Join(lines, "\n")}, m.activityLines()...)
	footer = append(footer, theme.Rule().Render(strings.Repeat("─", m.width)))
	for _, attached := range m.attached {
		footer = append(footer, attached.Render(m.width))
	}
	footer = append(footer, m.composer.View())
	footer = append(footer, m.commandLines()...)
	return lipgloss.JoinVertical(lipgloss.Left, append(footer, m.hint(scrollable))...)
}

func (m Model) hint(scrollable bool) string {
	line := quitHint + hintGap + keyHints
	switch {
	case m.Stopping:
		line = stoppingHint
	case m.Busy && len(m.queue) > 0:
		line = stopHint + hintGap + queueHints
	case m.Busy:
		line = stopHint + hintGap + keyHints
	}
	if scrollable {
		state := followingState
		if !m.following {
			state = scrolledState
		}
		room := max(m.width-widget.Cells(state), 0)
		line = widget.Pad(widget.Fit(line, room), room) + state
	}
	return theme.Faint().Render(widget.Fit(line, m.width))
}

func (m Model) transcriptRows() int {
	rows, _ := m.menuRows()
	return max(m.height-footerRows-len(m.attached)-len(m.activityRows())-len(rows), 1)
}

func (m Model) foldLine(start, end int) string {
	kinds := make([]string, 0, end-start)
	counted := make(map[string]int, end-start)
	bytes := 0
	for _, entry := range m.entries[start:end] {
		if counted[entry.Head] == 0 {
			kinds = append(kinds, entry.Head)
		}
		counted[entry.Head]++
		bytes += entry.Bytes
	}
	last := m.entries[end-1]
	since := max(last.Ended.Sub(m.entries[start].Started), 0)
	separator, style := countSeparator, theme.Faint()
	fields := []string{tools(end - start)}
	if last.running() {
		since = m.elapsed(m.entries[start].Started)
		separator, style = foldSeparator, theme.Accent()
		for _, kind := range kinds {
			fields = append(fields, kind+" "+strconv.Itoa(counted[kind]))
		}
	}
	if bytes > 0 {
		fields = append(fields, widget.Size(bytes))
	}
	fields = append(fields, widget.Until(since))
	return style.Render(widget.Fit(noteMarker+strings.Join(fields, separator), m.width))
}

func tools(count int) string {
	if count == 1 {
		return "1 tool"
	}
	return strconv.Itoa(count) + " tools"
}

func (m Model) render(entry Entry) []string {
	if entry.waiting {
		return m.queuedLines(entry)
	}
	if entry.Kind == Tool {
		return m.toolLines(entry)
	}
	marker, style := markerOf(entry.Kind)
	indent := strings.Repeat(" ", widget.Cells(marker))
	room := max(m.width-widget.Cells(marker), 1)
	var lines []string
	if entry.Kind == Assistant {
		lines = append(lines, "")
	}
	if entry.markdown() {
		for index, line := range entry.rendered {
			prefix := style.Render(marker)
			if index > 0 {
				prefix = indent
			}
			lines = append(lines, prefix+line)
		}
		return lines
	}
	for index, line := range widget.Wrap(entry.Body, room) {
		prefix := marker
		if index > 0 {
			prefix = indent
		}
		lines = append(lines, style.Render(prefix+line))
	}
	return lines
}

func (m Model) toolLines(entry Entry) []string {
	marker, style := markerOf(entry.Kind)
	verdict, verdictStyle := "", style
	if entry.Decision != nil {
		verdict, verdictStyle = entry.Decision.Verdict.String(), entry.Decision.Verdict.style()
	}
	status, statusStyle := entry.Status, style
	switch {
	case entry.running():
		status, statusStyle = widget.Until(m.elapsed(entry.Started)), theme.Accent()
	case entry.Failed:
		statusStyle = theme.Fail()
	}
	status = widget.Fit(status, max(m.width/statusShare-widget.Cells(verdict)-widget.Cells(gap), 0))
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
	room := max(m.width-columns-widget.Cells(gap), minimumColumns)
	lines := []string{style.Render(widget.Pad(widget.Fit(marker+entry.label(), room), room)) + gap + right}
	if m.open && entry.Detail != "" {
		for _, line := range widget.Wrap(entry.Detail, max(m.width-widget.Cells(continuation), 1)) {
			lines = append(lines, theme.Faint().Render(continuation+line))
		}
	}
	if entry.Decision != nil {
		lines = append(lines, entry.Decision.lines(m.width)...)
	}
	return lines
}

func markerOf(kind Kind) (string, lipgloss.Style) {
	switch kind {
	case User:
		return userMarker, theme.Accent()
	case Assistant:
		return assistantMark, theme.Speech()
	case Tool:
		return toolMarker, theme.Dim()
	case Note:
		return noteMarker, theme.Faint()
	case Failure:
		return failureMarker, theme.Fail()
	}
	panic("session: unknown entry kind")
}
