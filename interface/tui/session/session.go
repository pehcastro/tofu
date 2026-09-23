package session

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/markdown"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/progress"
	"tofu/interface/tui/subagent"
	"tofu/interface/tui/theme"
	"tofu/interface/tui/trace"
	"tofu/internal/widget"
)

const TickInterval = progress.TickInterval

const PhaseDwell = 400 * time.Millisecond

const (
	entryWindow      = 500
	composerRows     = 3
	tintLeadRows     = 1
	tintPadRows      = 2 * tintLeadRows
	footerRows       = composerRows + tintPadRows + 1
	keyHints         = "⏎ send   ⇧⏎ newline   / commands"
	queueHints       = "⏎ queues   alt+↑↓ picks   ctrl+x unqueues"
	hintGap          = "   "
	quitHint         = "ctrl+c quit"
	stopHint         = "ctrl+c stops the turn"
	stoppingHint     = "stopping the turn, ctrl+c will not quit until it ends"
	noResult         = "no result"
	continuation     = "    "
	composerInset    = "  "
	toolMarker       = "⟩ "
	shellTool        = "bash"
	assistantMark    = "▌ "
	userMarker       = "» "
	noteMarker       = "· "
	failureMarker    = "! "
	treeBranch       = "├─ "
	treeLast         = "└─ "
	minimumColumns   = 20
	statusShare      = 2
	foldSeparator    = " · "
	wheelLines       = 3
	wholeErrorInWork = "the whole error is in work"
	followingState   = "following"
	scrolledState    = "scrolled back   end returns"
)

var PlaceholderExamples = [3]string{
	"hey tofu, can you explain this repository to me?",
	"what should we do about the failing test in internal/turn?",
	"tofu, find where the gate reads its thresholds",
}

var composerGap = regexp.MustCompile(` +(?:\x1b\[[0-9;]*m)*$`)

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
	Promoted  bool
	Chips     []Chip
	Started   time.Time
	Ended     time.Time
	Decision  *Decision
	turn      int
	streaming bool
	waiting   bool
	rendered  []string
	stable    int
	width     int
}

type Result struct {
	Status string
	Bytes  int
	Failed bool
}

func (e Entry) assistant() bool { return e.Kind == Assistant }

func (e Entry) displayLines(room int) []string {
	if !e.streaming {
		return e.rendered
	}
	trailing := e.Body[e.stable:]
	if trailing == "" {
		return e.rendered
	}
	return append(append([]string{}, e.rendered...), widget.Wrap(trailing, room)...)
}

func (e Entry) running() bool { return e.Kind == Tool && e.ID != "" && e.Status == "" }

func (e Entry) sticky() bool {
	return e.Failed || e.Promoted || (e.Decision != nil && e.Decision.Verdict != Allow)
}

func (e Entry) label() string { return strings.TrimSpace(e.Head + " " + e.Body) }

type Prose func(source string, width int) []string

type Model struct {
	Busy           bool
	Stopping       bool
	Commands       []Command
	Paths          []string
	Children       []subagent.Child
	now            func() time.Time
	waiting        time.Time
	requested      time.Time
	answered       time.Time
	respondedOnce  bool
	waited         time.Duration
	phase          phase
	intent         string
	shown          time.Time
	prose          Prose
	plan           []PlanItem
	entries        []Entry
	composer       textarea.Model
	width          int
	height         int
	top            anchor
	following      bool
	began          time.Time
	entered        time.Time
	turns          int
	started        bool
	attached       []paste.Outcome
	pastes         int
	picked         int
	closed         bool
	queue          []pending
	queues         int
	pick           int
	sent           []string
	histAt         int
	draft          string
	chips          []Chip
	pending        []pendingPaste
	ChatShowsTools bool
	FoldHidesShell bool
}

func New(now func() time.Time, prose Prose) Model {
	composer := textarea.New()
	composer.Placeholder = PlaceholderExamples[pickPlaceholder(now())]
	composer.ShowLineNumbers = false
	composer.Prompt = composerInset
	composer.SetHeight(composerRows)
	composer.CharLimit = 0
	composer.SetVirtualCursor(false)
	composer.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	composer.SetStyles(tintedComposerStyles())
	return Model{now: now, prose: prose, composer: composer, following: true, began: now()}
}

func pickPlaceholder(at time.Time) int {
	offset := at.UnixNano() % int64(len(PlaceholderExamples))
	if offset < 0 {
		offset += int64(len(PlaceholderExamples))
	}
	return int(offset)
}

func tintedComposerStyles() textarea.Styles {
	styles := textarea.DefaultDarkStyles()
	styles.Focused = tintedState(styles.Focused)
	styles.Blurred = tintedState(styles.Blurred)
	styles.Cursor.Shape = tea.CursorBar
	return styles
}

func tintedState(state textarea.StyleState) textarea.StyleState {
	tint := theme.ComposerColor()
	state.Base = state.Base.Background(tint)
	state.Text = state.Text.Background(tint)
	state.LineNumber = state.LineNumber.Background(tint)
	state.CursorLineNumber = state.CursorLineNumber.Background(tint)
	state.CursorLine = state.CursorLine.Background(tint)
	state.EndOfBuffer = state.EndOfBuffer.Background(tint)
	state.Placeholder = state.Placeholder.Background(tint)
	state.Prompt = state.Prompt.Background(tint)
	return state
}

func (m *Model) Focus() tea.Cmd { return m.composer.Focus() }

func (m *Model) Insert(text string) { m.composer.InsertString(text) }

func (m Model) Cursor() *tea.Cursor {
	caret := m.composer.Cursor()
	if caret == nil {
		return nil
	}
	caret.Y += m.transcriptRows() + m.activityBlockRows() + len(m.attached) + tintLeadRows
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
	if !entry.assistant() {
		return
	}
	room := max(m.width-widget.Cells(assistantMark), 1)
	if !entry.streaming {
		entry.rendered, entry.stable, entry.width = m.prose(entry.Body, room), len(entry.Body), room
		return
	}
	boundary := max(markdown.Boundary(entry.Body), entry.stable)
	if boundary == entry.stable && room == entry.width {
		return
	}
	entry.width = room
	if boundary == 0 {
		entry.rendered, entry.stable = nil, 0
		return
	}
	entry.rendered, entry.stable = m.prose(entry.Body[:boundary], room), boundary
}

func (m *Model) Stream(text string) {
	last := len(m.entries) - 1
	if last >= 0 && m.entries[last].streaming {
		m.entries[last].Body += text
		m.rerender(&m.entries[last])
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
	entry.Started, entry.turn = m.now(), m.turns
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
		m.histAt, m.draft = len(m.sent), ""
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
	m.attached, m.chips, m.pending = nil, nil, nil
	m.closed, m.picked = false, 0
}

func (m *Model) Follow() { m.following = true }

func (m *Model) Start() {
	if !m.started {
		m.dropGreeting()
		m.started = true
	}
	at := m.now()
	m.entered, m.turns = at, m.turns+1
	m.Busy, m.Stopping, m.began, m.plan = true, false, at, nil
	m.waited, m.phase, m.intent, m.shown = 0, requesting, "", at
	m.requested, m.answered, m.respondedOnce = at, time.Time{}, false
}

func (m *Model) dropGreeting() {
	if len(m.entries) > 0 && m.entries[0].Kind == Note {
		m.entries = m.entries[1:]
	}
}

func (m *Model) Close(words, id string) {
	work := m.elapsed(m.began)
	line := words + " " + widget.Until(work)
	if m.waited > work {
		line += foldSeparator + "waited " + widget.Until(m.waited)
	}
	if short := trace.Short(id); short != "" {
		line += foldSeparator + "[" + short + "]"
	}
	m.Append(Entry{Kind: Note, Body: line})
}

func (m *Model) TakePartial() (string, bool) {
	last := len(m.entries) - 1
	if last < 0 || !m.entries[last].streaming || m.entries[last].Body == "" {
		return "", false
	}
	partial := m.entries[last].Body
	m.entries = m.entries[:last]
	return partial, true
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
	content := m.linesFrom(from, rows)
	for len(content) < rows {
		content = append(content, "")
	}
	lines := slices.Concat(content, plan)
	footer := []string{strings.Join(lines, "\n")}
	if block := append(m.activityLines(), m.askLines()...); len(block) > 0 {
		footer = append(footer, "")
		footer = append(footer, block...)
	}
	for _, attached := range m.attached {
		footer = append(footer, attached.Render(m.width))
	}
	footer = append(footer, m.composerView())
	footer = append(footer, m.commandLines()...)
	return lipgloss.JoinVertical(lipgloss.Left, append(footer, m.hint(scrollable))...)
}

func (m Model) composerView() string {
	rows := strings.Split(m.composer.View(), "\n")
	for index, row := range rows {
		rows[index] = tintRow(row)
	}
	pad := tintRow(strings.Repeat(" ", m.width))
	return strings.Join(append(append([]string{pad}, rows...), pad), "\n")
}

func tintRow(row string) string {
	loc := composerGap.FindStringIndex(row)
	if loc == nil {
		return row
	}
	plain := ansi.Strip(row[loc[0]:loc[1]])
	return row[:loc[0]] + lipgloss.NewStyle().Background(theme.ComposerColor()).Render(plain)
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
	return max(m.height-footerRows-len(m.attached)-m.activityBlockRows()-len(rows), 1)
}

func (m Model) activityBlockRows() int {
	rows := len(m.activityRows())
	if _, open := m.openAsk(); open {
		rows += askBlockRows
	}
	if rows > 0 {
		rows++
	}
	return rows
}

func (m Model) foldLine(start, end int) string {
	shell, decisions := 0, 0
	for _, entry := range m.entries[start:end] {
		if entry.Head == shellTool {
			shell++
		}
		if entry.Decision != nil {
			decisions++
		}
	}
	fields := []string{"(" + strconv.Itoa(end-start) + ") tools"}
	if decisions > 0 {
		fields = append(fields, "jev "+strconv.Itoa(decisions))
	}
	if shell > 0 && !m.FoldHidesShell {
		fields = append(fields, "shell ("+strconv.Itoa(shell)+")")
	}
	fields = append(fields, widget.Until(m.foldSince(end)))
	if id := trace.Short(m.entries[end-1].ID); id != "" {
		fields = append(fields, "["+id+"]")
	}
	return theme.Faint().Render(widget.Fit(noteMarker+strings.Join(fields, foldSeparator), m.width))
}

func (m Model) foldSince(end int) time.Duration {
	if end >= len(m.entries) {
		return m.elapsed(m.began)
	}
	return max(m.entries[end].Started.Sub(m.began), 0)
}

func (m Model) render(entry Entry) []string {
	if entry.waiting {
		return m.queuedLines(entry)
	}
	if entry.Kind == Tool {
		return m.toolLines(entry)
	}
	if entry.Kind == Failure {
		return []string{m.failureLine(entry)}
	}
	marker, style := markerOf(entry.Kind)
	indent := strings.Repeat(" ", widget.Cells(marker))
	room := max(m.width-widget.Cells(marker), 1)
	var lines []string
	if entry.Kind == Assistant || entry.Kind == User {
		lines = append(lines, "")
	}
	if entry.assistant() {
		for index, line := range entry.displayLines(room) {
			prefix := style.Render(marker)
			if index > 0 {
				prefix = indent
			}
			lines = append(lines, prefix+line)
		}
		if !entry.streaming {
			if id := idLine(entry, indent); id != "" {
				lines = append(lines, id)
			}
			lines = append(lines, "")
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
	if entry.Kind == User {
		lines = append(lines, m.chipLines(entry.Chips)...)
	}
	if id := idLine(entry, indent); id != "" {
		lines = append(lines, id)
	}
	return lines
}

func (m Model) failureLine(entry Entry) string {
	marker, style := markerOf(Failure)
	tail := ""
	if short := trace.Short(entry.ID); short != "" {
		tail = gap + theme.Faint().Render(wholeErrorInWork+" ["+short+"]")
	}
	room := max(m.width-widget.Cells(marker+tail), 1)
	return style.Render(marker+widget.Fit(strings.Join(strings.Fields(entry.Body), " "), room)) + tail
}

func idLine(entry Entry, indent string) string {
	id := trace.Short(entry.ID)
	if id == "" {
		return ""
	}
	return theme.ID().Render(indent + id)
}

func (m Model) toolLines(entry Entry) []string {
	marker, style := markerOf(entry.Kind)
	if entry.Head == shellTool {
		style = theme.Tool()
	}
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
	case status != "":
		statusStyle = theme.Added()
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
	if entry.Detail != "" {
		for _, line := range widget.Wrap(entry.Detail, max(m.width-widget.Cells(continuation), 1)) {
			lines = append(lines, theme.Faint().Render(continuation+line))
		}
	}
	if entry.Decision != nil {
		lines = append(lines, entry.Decision.lines(m.width)...)
	}
	if id := idLine(entry, continuation); id != "" {
		lines = append(lines, id)
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
		return toolMarker, theme.Call()
	case Note:
		return noteMarker, theme.Faint()
	case Failure:
		return failureMarker, theme.Fail()
	}
	panic("session: unknown entry kind")
}
