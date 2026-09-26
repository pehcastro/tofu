package session

import (
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/markdown"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/progress"
	"tofu/interface/tui/subagent"
	"tofu/interface/tui/trace"
	"tofu/internal/widget"
)

const TickInterval = progress.TickInterval

const PhaseDwell = 400 * time.Millisecond

const (
	entryWindow      = 500
	noResult         = "no result"
	shellTool        = "bash"
	minimumColumns   = 20
	requestSeparator = "  |  "
	Placeholder      = "Ask tofu to build, inspect, or delegate"
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
	Promoted  bool
	Chips     []Chip
	Started   time.Time
	Ended     time.Time
	Decision  *Decision
	turn      int
	intoTurn  time.Duration
	streaming bool
	waiting   bool
	rendered  []string
	tail      []string
	stable    int
	width     int
	drawn     drawn
}

type Result struct {
	Status string
	Bytes  int
	Failed bool
}

func (e Entry) assistant() bool { return e.Kind == Assistant }

func (e Entry) message() bool { return e.Kind == User || e.Kind == Assistant }

func (e Entry) displayLines() []string {
	if !e.streaming || e.tail == nil {
		return e.rendered
	}
	return append(append([]string{}, e.rendered...), e.tail...)
}

func (e Entry) returned() bool { return e.Status != "" }

func (e Entry) running() bool { return e.Kind == Tool && e.ID != "" && !e.returned() }

func (e Entry) sticky() bool {
	return e.Failed || e.Promoted || (e.Decision != nil && e.Decision.Verdict != Allow)
}

func (e Entry) label() string { return strings.TrimSpace(e.Head + " " + e.Body) }

type Prose func(source string, width int) []string

type Model struct {
	Busy               bool
	Stopping           bool
	LettingToolsFinish bool
	Commands           []Command
	Paths              []string
	Children           []subagent.Child
	now                func() time.Time
	waiting            time.Time
	requested          time.Time
	answered           time.Time
	respondedOnce      bool
	waited             time.Duration
	phase              phase
	shown              time.Time
	prose              Prose
	plan               []PlanItem
	entries            []Entry
	composer           textarea.Model
	width              int
	height             int
	top                anchor
	following          bool
	began              time.Time
	entered            time.Time
	turns              int
	turnID             string
	cooked             string
	cookedID           string
	minted             int
	started            bool
	attached           []paste.Outcome
	pastes             int
	picked             int
	closed             bool
	queue              []pending
	pick               int
	sent               []string
	histAt             int
	draft              string
	chips              []Chip
	pending            []pendingPaste
	frame              int
	ChatShowsTools     bool
	FoldHidesShell     bool
}

func New(now func() time.Time, prose Prose) Model {
	composer := textarea.New()
	composer.Placeholder = Placeholder
	composer.ShowLineNumbers = false
	composer.Prompt = ""
	composer.DynamicHeight = true
	composer.MinHeight = composerMinRows
	composer.MaxHeight = composerMaxRows
	composer.CharLimit = 0
	composer.SetVirtualCursor(false)
	composer.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	styles := look.ComposerStyles()
	styles.Cursor.Shape = tea.CursorBar
	composer.SetStyles(styles)
	return Model{now: now, prose: prose, composer: composer, following: true, began: now()}
}

func (m *Model) Focus() tea.Cmd { return m.composer.Focus() }

func (m *Model) Blur() { m.composer.Blur() }

func (m *Model) SetFrame(frame int) { m.frame = frame }

func (m *Model) pulse() time.Duration { return time.Duration(m.frame) * TickInterval }

func (m *Model) Insert(text string) { m.composer.InsertString(text) }

func (m *Model) SetSize(width, height int) {
	columns := max(width, minimumColumns)
	rewrap := columns != m.width
	m.width, m.height = columns, height
	m.composer.SetWidth(max(m.width-composerSideCells, 1))
	if !rewrap {
		return
	}
	for index := range m.entries {
		m.rerender(&m.entries[index])
	}
}

func (m *Model) textWidth() int { return max(m.width-2*messageInset, 1) }

func (m *Model) rerender(entry *Entry) {
	if !entry.assistant() {
		return
	}
	room := m.textWidth()
	if !entry.streaming {
		entry.rendered, entry.stable, entry.width, entry.tail = m.prose(entry.Body, room), len(entry.Body), room, nil
		return
	}
	boundary := max(markdown.Boundary(entry.Body), entry.stable)
	if boundary != entry.stable || room != entry.width {
		entry.width = room
		if boundary == 0 {
			entry.rendered, entry.stable = nil, 0
		} else {
			entry.rendered, entry.stable = m.prose(entry.Body[:boundary], room), boundary
		}
	}
	if trailing := entry.Body[entry.stable:]; trailing != "" {
		entry.tail = m.prose(trailing, room)
	} else {
		entry.tail = nil
	}
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

func (m *Model) mint() string {
	m.minted++
	sum := fnv.New64a()
	_, _ = sum.Write([]byte(m.now().String() + strconv.Itoa(m.minted)))
	return strconv.FormatUint(sum.Sum64(), 16)
}

func (m *Model) Append(entry Entry) {
	m.seal()
	entry.Started, entry.turn, entry.intoTurn = m.now(), m.turns, m.elapsed(m.began)
	if entry.Kind == User && entry.ID == "" {
		entry.ID = m.mint()
	}
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
	m.seal()
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
		if entry.Kind == Tool && entry.Decision == nil && entry.Head == decision.Tool && !entry.returned() {
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
	if strings.HasPrefix(rest, commandPrefix) || strings.HasPrefix(rest, atSigil) {
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
	m.entered, m.turns, m.turnID = at, m.turns+1, m.mint()
	m.Busy, m.Stopping, m.LettingToolsFinish, m.began, m.plan = true, false, false, at, nil
	m.waited, m.phase, m.shown = 0, requesting, at
	m.requested, m.answered, m.respondedOnce = at, time.Time{}, false
}

func (m *Model) dropGreeting() {
	if len(m.entries) > 0 && m.entries[0].Kind == Note {
		m.entries = m.entries[1:]
	}
}

func (m *Model) Close(words, id string) {
	m.cooked = words + " " + widget.Until(m.elapsed(m.began)) + requestSeparator + "waited " + widget.Until(m.waited)
	m.cookedID = m.turnID
	if id != "" {
		m.cookedID = id
	}
	m.Append(Entry{Kind: Note, Body: m.cooked + "  [request" + trace.Short(m.cookedID) + "]"})
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
	interrupted := m.Stopping || m.LettingToolsFinish
	m.Busy, m.Stopping, m.LettingToolsFinish = false, false, false
	m.requested, m.answered = time.Time{}, time.Time{}
	m.seal()
	for index := range m.entries {
		entry := &m.entries[index]
		if entry.running() {
			entry.Status, entry.Ended = noResult, m.now()
		}
		if interrupted && entry.Kind == Tool && entry.turn == m.turns {
			entry.Promoted = true
		}
	}
}
