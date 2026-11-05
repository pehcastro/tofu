package session

import (
	"cmp"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/internal/cli/render"
	"tofu/internal/host"
	"tofu/internal/konst"
	isession "tofu/internal/store/session"
	"tofu/internal/tools/history/quote"
	"tofu/internal/tools/human/plan"
	"tofu/internal/ui/tui/chat/markdown"
	"tofu/internal/ui/tui/composer/paste"
	"tofu/internal/ui/tui/look/look"
	"tofu/internal/ui/tui/panels/progress"
	"tofu/internal/ui/tui/panels/subagent"
)

const TickInterval = progress.TickInterval

const PhaseDwell = 400 * time.Millisecond

const (
	entryWindow      = 500
	noResult         = "no result"
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
	Output    string
	Bytes     int
	Failed    bool
	Promoted  bool
	Chips     []Chip
	Started   time.Time
	Ended     time.Time
	Decision  *Decision
	SubAgents []string
	Card      *host.ToolCard
	Group     string
	recorded  string
	askID     string
	turn      int
	streaming bool
	taken     bool
	asking    bool
	asked     bool
	rendered  []string
	tail      []string
	stable    int
	boundary  int
	undrawn   bool
	width     int
	wraps     map[int][]string
	drawn     drawn
}

type Result struct {
	Status string
	Output string
	Bytes  int
	Failed bool
	At     time.Time
	Card   *host.ToolCard
}

func (e Entry) assistant() bool { return e.Kind == Assistant }

func (e Entry) message() bool { return e.Kind == User || e.Kind == Assistant }

func (e Entry) displayLines() []string {
	lines := e.rendered
	if e.streaming && e.tail != nil {
		lines = append(append([]string{}, e.rendered...), e.tail...)
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	return lines
}

func blank(line string) bool { return strings.TrimSpace(ansi.Strip(line)) == "" }

func (e Entry) returned() bool { return e.Status != "" }

func (e Entry) running() bool { return e.Kind == Tool && e.ID != "" && !e.returned() }

func (e Entry) label() string { return strings.TrimSpace(e.Head + " " + e.Body) }

type Prose func(source string, width int) []string

type Model struct {
	Busy               bool
	Stopping           bool
	LettingToolsFinish bool
	Commands           []Command
	Paths              []string
	SubAgents          []subagent.Row
	Activity           []Activity
	Spawning           []string
	Question           []string
	now                func() time.Time
	leadIdleSince      time.Time
	waitingOn          int
	stopAsked          int
	stopAskedUntil     time.Time
	waiting            time.Time
	asks               []string
	requested          time.Time
	answered           time.Time
	respondedOnce      bool
	leadThinks         bool
	waited             time.Duration
	phase              phase
	shown              time.Time
	prose              Prose
	unshown            map[string]bool
	entries            []Entry
	earlier            int
	lo                 int
	composer           textarea.Model
	bar                []string
	barKey             composerKey
	width              int
	height             int
	top                anchor
	following          bool
	began              time.Time
	entered            time.Time
	turns              int
	requestID          string
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
	sent               []sentEntry
	histAt             int
	draft              sentEntry
	chips              []Chip
	pending            []pendingPaste
	frame              int
	revision           int
	rows               rowTable
	opened             map[string]bool
	ChatShowsTools     bool
	Welcome            func(width, rows int) []string
	Notice             string
	FeedID             func(full string) string
}

func New(now func() time.Time, prose Prose) Model {
	composer := textarea.New()
	composer.Placeholder = Placeholder
	composer.ShowLineNumbers = false
	composer.Prompt = ""
	composer.DynamicHeight = true
	composer.MinHeight = composerMinRows
	composer.MaxHeight = composerMaxRows
	composer.MaxContentHeight = konst.ComposerContentRows
	composer.CharLimit = 0
	composer.SetVirtualCursor(false)
	composer.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	styles := look.ComposerStyles()
	styles.Cursor.Shape = tea.CursorBar
	composer.SetStyles(styles)
	return Model{now: now, prose: prose, composer: composer, following: true, began: now(), unshown: map[string]bool{}, opened: map[string]bool{}, FeedID: quote.ShortID}
}

func (m *Model) Focus() tea.Cmd { return m.composer.Focus() }

func (m *Model) Blur() { m.composer.Blur() }

func (m *Model) SetFrame(frame int) { m.frame = frame }

func (m *Model) Insert(text string) { m.composer.InsertString(text) }

func (m *Model) SetSize(width, height int) {
	if last := len(m.entries) - 1; last >= 0 && m.entries[last].undrawn {
		m.rerender(&m.entries[last])
	}
	m.width, m.height = max(width, minimumColumns), height
	m.composer.SetWidth(max(m.width-composerSideCells, 1))
}

func (m *Model) textWidth() int { return max(m.width-2*messageInset, 1) }

func (m *Model) wrapped(entry *Entry) bool {
	room := m.textWidth()
	if entry.undrawn && entry.streaming {
		m.rerender(entry)
	}
	if !entry.assistant() || entry.width == room {
		return true
	}
	if _, known := entry.wraps[room]; !known || entry.streaming {
		return false
	}
	m.rerender(entry)
	return true
}

func (m *Model) Fill() bool {
	left := konst.RewrapFillEntries
	lo, hi := m.window()
	for index := hi - 1; index >= lo; index-- {
		if entry := &m.entries[index]; !m.wrapped(entry) {
			if left == 0 {
				return true
			}
			m.rerender(entry)
			left--
		}
	}
	return false
}

func (m *Model) rerender(entry *Entry) {
	if !entry.assistant() {
		return
	}
	m.revision++
	room := m.textWidth()
	entry.undrawn = false
	if !entry.streaming {
		lines, known := entry.wraps[room]
		if !known {
			lines = m.prose(entry.Body, room)
			if entry.wraps == nil || len(entry.wraps) >= konst.RewrapKeptWidths {
				entry.wraps = map[int][]string{}
			}
			entry.wraps[room] = lines
		}
		entry.rendered, entry.stable, entry.width, entry.tail = lines, len(entry.Body), room, nil
		return
	}
	switch {
	case room == entry.width && entry.stable > 0 && entry.boundary > entry.stable:
		grown := m.prose(entry.Body[entry.stable:entry.boundary], room)
		if len(grown) > 0 && len(entry.rendered) > 0 {
			grown = append([]string{""}, grown...)
		}
		entry.rendered, entry.stable = append(slices.Clip(entry.rendered), grown...), entry.boundary
	case entry.boundary != entry.stable || room != entry.width:
		entry.width, entry.stable, entry.rendered = room, entry.boundary, nil
		if entry.boundary > 0 {
			entry.rendered = m.prose(entry.Body[:entry.boundary], room)
		}
	}
	if trailing := entry.Body[entry.stable:]; trailing != "" {
		entry.tail = m.prose(trailing, room)
	} else {
		entry.tail = nil
	}
}

func (m *Model) Stream(text string) {
	at := m.streamingAt()
	if at < 0 {
		m.Append(Entry{Kind: Assistant, Body: text, streaming: true, boundary: markdown.Boundary(text)})
		return
	}
	entry := &m.entries[at]
	entry.Body += text
	entry.boundary = max(markdown.Boundary(entry.Body), entry.boundary)
	entry.undrawn = true
	m.revision++
}

func (m *Model) streamingAt() int {
	for at := len(m.entries) - 1; at >= 0; at-- {
		if m.entries[at].streaming {
			return at
		}
		if m.entries[at].message() {
			return -1
		}
	}
	return -1
}

func (m *Model) seal() {
	at := m.streamingAt()
	if at < 0 {
		return
	}
	m.entries[at].streaming = false
	m.rerender(&m.entries[at])
}

func (m *Model) mint() string {
	m.minted++
	sum := fnv.New64a()
	_, _ = sum.Write([]byte(m.now().String() + strconv.Itoa(m.minted)))
	return strconv.FormatUint(sum.Sum64(), 16)
}

func (m *Model) Append(entry Entry) {
	if entry.Kind == Tool && entry.Head == plan.PlanToolName {
		if entry.ID != "" {
			m.unshown[entry.ID] = true
		}
		return
	}
	if entry.message() {
		m.seal()
	}
	m.revision++
	entry.Started, entry.turn = cmp.Or(entry.Started, m.now()), m.turns
	if entry.Kind == User && entry.ID == "" {
		entry.ID = m.mint()
	}
	if entry.streaming {
		m.rerender(&entry)
	} else if entry.assistant() {
		entry.undrawn, entry.rendered = true, render.Wrap(entry.Body, m.textWidth())
	}
	m.entries = append(m.entries, entry)
	m.frameAround(m.top.entry)
}

func (m *Model) window() (int, int) {
	lo := min(m.lo, len(m.entries))
	return lo, min(lo+entryWindow, len(m.entries))
}

func (m *Model) frameAround(entry int) {
	last := max(len(m.entries)-entryWindow, 0)
	switch lo, hi := m.window(); {
	case m.following:
		m.lo = last
	case entry < lo || entry >= hi:
		m.lo = min(max(entry-entryWindow/2, 0), last)
	}
	if m.lo < len(m.entries) {
		m.lo, _ = m.blockAt(m.lo)
	}
}

func (m *Model) Older() *Model {
	return &Model{now: m.now, prose: m.prose, width: m.width, height: m.height, following: true, unshown: map[string]bool{}, opened: map[string]bool{}, FeedID: m.FeedID, ChatShowsTools: m.ChatShowsTools}
}

func (m *Model) MarkEarlier() { m.earlier = len(m.entries) }

func (m *Model) Prepend(older *Model) {
	for index := range older.entries {
		older.entries[index].turn = -1
	}
	m.entries = slices.Insert(m.entries, m.earlier, older.entries...)
	if !m.following && m.top.entry >= m.earlier {
		m.top.entry += len(older.entries)
	}
	if m.lo >= m.earlier {
		m.lo += len(older.entries)
	}
	m.revision++
	m.frameAround(m.top.entry)
}

func (m *Model) Finish(id string, result Result) {
	if m.unshown[id] {
		delete(m.unshown, id)
		return
	}
	ended := m.now()
	for index := len(m.entries) - 1; index >= 0; index-- {
		entry := &m.entries[index]
		if entry.running() && entry.ID == id {
			entry.Status, entry.Output, entry.Bytes, entry.Failed, entry.Ended = result.Status, result.Output, result.Bytes, result.Failed, cmp.Or(result.At, ended)
			entry.Card = cmp.Or(result.Card, entry.Card)
			entry.Promoted = entry.Promoted || m.Stopping
			m.revision++
			return
		}
	}
	m.Append(Entry{Kind: Note, Body: result.Status})
}

func (m *Model) LastAnswer() (string, bool) {
	for index := len(m.entries) - 1; index >= 0; index-- {
		if entry := m.entries[index]; entry.Kind == Assistant {
			return entry.Body, true
		}
	}
	return "", false
}

func (m *Model) Intent(id string) string {
	for index := len(m.entries) - 1; index >= 0; index-- {
		if entry := m.entries[index]; entry.Kind == Tool && entry.ID == id {
			return entry.Body
		}
	}
	return ""
}

func (m *Model) LastCall() (string, bool) {
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

func (m *Model) Decide(id string, decision Decision) {
	for index := range m.entries {
		entry := &m.entries[index]
		if entry.Kind == Tool && entry.Decision == nil && entry.Head == decision.Tool && !entry.returned() {
			entry.Decision, entry.askID = &decision, id
			m.revision++
			return
		}
	}
	m.Append(Entry{Kind: Tool, Head: decision.Tool, Decision: &decision, askID: id})
}

func (m *Model) Recorded(role, content, id string, at time.Time) {
	if strings.TrimSpace(content) == "" {
		return
	}
	switch role {
	case isession.RoleAssistant:
		m.link(func(entry Entry) bool {
			return entry.Kind == Assistant && strings.TrimSpace(entry.Body) == strings.TrimSpace(content)
		}, id, at)
	case isession.RoleUser:
		m.link(func(entry Entry) bool {
			return entry.Kind == User && strings.Contains(content, Expand(entry.Body, entry.Chips))
		}, id, at)
	}
}

func (m *Model) link(matches func(Entry) bool, id string, at time.Time) {
	for index := range m.entries {
		entry := &m.entries[index]
		if entry.recorded != "" || !matches(*entry) {
			continue
		}
		entry.recorded, entry.drawn = id, drawn{}
		if !at.IsZero() {
			entry.Started = at
		}
		m.revision++
		return
	}
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	before, row, col := m.composer.Value(), m.composer.Line(), m.composer.Column()
	composer, cmd := m.composer.Update(msg)
	m.composer = composer
	if m.composer.Value() != before {
		m.deleteWhole(before)
		m.edited()
	}
	if m.composer.Line() != row || m.composer.Column() != col {
		m.stepOver(row, col)
	}
	return cmd
}

func (m *Model) edited() {
	m.chips = slices.DeleteFunc(m.chips, func(chip Chip) bool { return !strings.Contains(m.composer.Value(), chip.Token) })
	m.closed, m.picked = false, 0
	m.histAt, m.draft = len(m.sent), sentEntry{}
}

func (m *Model) Value() string { return unescaped(strings.TrimSpace(m.composer.Value())) }

func (m *Model) Draft() string { return m.composer.Value() }

func (m *Model) Redraft(text string) {
	m.composer.SetValue(text)
	m.edited()
}

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

func (m *Model) Follow() {
	m.following = true
	m.frameAround(m.top.entry)
}

func (m *Model) Start() {
	if !m.started {
		m.dropGreeting()
		m.started = true
	}
	at := m.now()
	m.entered, m.turns, m.requestID = at, m.turns+1, ""
	m.Busy, m.Stopping, m.LettingToolsFinish, m.began = true, false, false, at
	m.leadIdleSince, m.waitingOn = time.Time{}, 0
	m.waited, m.phase, m.shown = 0, requesting, at
	m.requested, m.answered, m.respondedOnce, m.leadThinks = at, time.Time{}, false, false
}

func (m *Model) dropGreeting() {
	if len(m.entries) > 0 && m.entries[0].Kind == Note {
		m.entries = m.entries[1:]
		m.earlier = max(m.earlier-1, 0)
		m.revision++
	}
}

func (m *Model) Close(words, id string) {
	m.cooked = words + " " + render.Until(m.elapsed(m.began))
	if !m.Stopping {
		m.cooked += requestSeparator + "waited " + render.Until(m.waited)
	}
	m.cookedID = id
	m.seal()
}

func (m *Model) TakePartial() (string, bool) {
	at := m.streamingAt()
	if at < 0 || m.entries[at].Body == "" || slices.ContainsFunc(m.entries[at+1:], func(after Entry) bool { return after.Kind == Tool }) {
		return "", false
	}
	partial := m.entries[at].Body
	m.entries = slices.Delete(m.entries, at, at+1)
	m.revision++
	return partial, true
}

func (m *Model) Stop() {
	interrupted := m.Stopping || m.LettingToolsFinish
	m.Busy, m.Stopping, m.LettingToolsFinish = false, false, false
	m.requested, m.answered = time.Time{}, time.Time{}
	m.leadIdleSince, m.waitingOn, m.stopAskedUntil = time.Time{}, 0, time.Time{}
	m.seal()
	m.revision++
	for index := range m.entries {
		entry := &m.entries[index]
		if entry.running() {
			entry.Status, entry.Ended, entry.Promoted = noResult, m.now(), entry.Promoted || interrupted
		}
		if interrupted && entry.Kind == Tool && entry.Group == "" && entry.turn == m.turns {
			entry.Promoted = true
		}
	}
}
