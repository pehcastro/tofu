package feed

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
	"tofu/interface/tui/progress"
	"tofu/internal/konst"
	roster "tofu/internal/subagent"
	"tofu/internal/widget"
)

type Kind int

const (
	KindMessage Kind = iota
	KindTool
	KindEdit
	KindRequest
	KindFailure
	KindNote
	KindDecision
	KindSpawn
	KindThinking
)

func (k Kind) String() string {
	return [...]string{"message", "tool", "edit", "request", "failure", "note", "decision", "spawn", "thinking"}[k]
}

type State int

const (
	StateRunning State = iota
	StateComplete
	StateFailed
	StateStopped
)

func (s State) String() string { return [...]string{"running", "complete", "failed", "stopped"}[s] }

type Op int

const (
	OpModified Op = iota
	OpAdded
	OpDeleted
)

type Event struct {
	ID, Actor, Target string
	Instance          int
	Kind              Kind
	State             State
	Title, Body       string
	Detail            []string
	Path              string
	Op                Op
	Added, Removed    int
	At                time.Time
	Elapsed           time.Duration
}

type Agent struct {
	Name       string
	Definition string
	Model      string
	Instance   int
	State      roster.State
	Doing      string
	Since      time.Duration
	Owns       []string
	Report     string
}

const (
	narrowWidth    = 86
	railMaxWidth   = 31
	railShare      = 4
	panePadding    = 2
	trackGutter    = 1
	minFeedWidth   = 24
	headerRows     = 3
	wheelRows      = 3
	railCacheCap   = 1024
	spinnerCell    = "\xe2\xa0\x80"
	measureBatch   = 32
	compactGap     = 0
	comfortableGap = 1
	spaciousGap    = 2
	allActivity    = "All activity"
	orchestrator   = "orchestrator"
	askTool        = "ask"
	messageTool    = "message"
	askedWord      = " asked: "
	toldWord       = ": "
)

type Retention int

const (
	KeepAll Retention = iota
	KeepRecent
	RailOnly
)

type identity struct {
	name     string
	instance int
}

func (i identity) label() string {
	if i.instance > 0 {
		return "[&" + i.name + " " + strconv.Itoa(i.instance) + "]"
	}
	return "[&" + i.name + "]"
}

func actor(e Event) identity { return identity{e.Actor, e.Instance} }

type Model struct {
	now                func() time.Time
	width, height      int
	events             []Event
	agents             []Agent
	orchestratorBusy   bool
	plan               string
	railFocused        bool
	filter             identity
	selected           string
	expanded           map[string]bool
	scroll, frame, gap int
	version            int
	retention          Retention
	thinkingHidden     bool
	cards              *cardCache
	rail, main         *look.PaneCache
}

func New(now func() time.Time) Model {
	return Model{now: now, railFocused: true, expanded: map[string]bool{}, gap: comfortableGap, cards: &cardCache{}, rail: &look.PaneCache{}, main: &look.PaneCache{}}
}

func (m *Model) SetSize(width, height int) { m.width, m.height = width, height }
func (m *Model) SetFocus(sidebar bool)     { m.railFocused = sidebar }
func (m *Model) SetFrame(frame int)        { m.frame = frame }

func (m *Model) SetAgents(orchestratorBusy bool, agents []Agent) {
	m.orchestratorBusy, m.agents = orchestratorBusy, agents
}

func (m *Model) SetPlan(line string) { m.plan = line }

func (m *Model) SetDensity(density string) {
	switch density {
	case "compact":
		m.gap = compactGap
	case "spacious":
		m.gap = spaciousGap
	default:
		m.gap = comfortableGap
	}
}

func (m *Model) SetRetention(retention Retention) { m.retention = retention }

func (m *Model) SetThinking(shown bool) { m.thinkingHidden = !shown }

func (m Model) retained() []Event {
	events := m.events
	if m.thinkingHidden {
		events = slices.DeleteFunc(slices.Clone(events), func(e Event) bool { return e.Kind == KindThinking })
	}
	switch m.retention {
	case KeepAll:
		return events
	case KeepRecent:
		return events[max(0, len(events)-konst.FeedRecentEvents):]
	case RailOnly:
		return nil
	}
	panic("feed: unknown retention " + strconv.Itoa(int(m.retention)))
}

func (m *Model) SetEvents(events []Event) {
	if m.scroll == 0 {
		m.events, m.version = events, m.version+1
		return
	}
	s := m.laid(m.depth(), "")
	top, anchor := fromTop(s.rows, m.pageHeight(), m.scroll), len(s.drafts)-1
	for anchor > 0 && s.starts[anchor] > top {
		anchor--
	}
	m.events, m.version = events, m.version+1
	if anchor < 0 {
		return
	}
	id, into := s.drafts[anchor].event.ID, top-s.starts[anchor]
	s = m.laid(m.depth(), id)
	if at := s.index(id); at >= 0 {
		m.scroll = max(0, s.rows-m.pageHeight()-s.starts[at]-into)
	}
}

func (m Model) Selected() string { return m.selected }

func (m Model) Split() int {
	if m.retention == RailOnly {
		return m.width
	}
	if m.width < narrowWidth {
		return 0
	}
	return min(railMaxWidth, m.width/railShare)
}

func (m Model) feedWidth() int {
	return max(minFeedWidth, m.width-m.Split()-2*panePadding-trackGutter)
}

func (m Model) pageHeight() int { return max(1, m.height-headerRows) }

func (m Model) Track() pointer.Track {
	rows, height := m.laid(m.depth(), "").rows, m.pageHeight()
	return pointer.Track{Total: rows, Visible: height, FromTop: fromTop(rows, height, m.scroll)}
}

func (m Model) View() string {
	if m.retention == RailOnly {
		return m.railPane(m.width)
	}
	width := m.feedWidth()
	p := m.page()
	heading, hint := allActivity, "newest"
	if m.filter != (identity{}) {
		heading = m.filter.label()
	}
	if p.first > 1 {
		hint = "wheel ↑ older"
	}
	main := look.Sides(look.PaneTitle(heading, !m.railFocused), look.Faint(fmt.Sprintf("%d-%d/%d · %s", p.first, p.last, p.total, hint)), width) + "\n\n" + p.view
	split := m.Split()
	feedPane := m.main.Surface(m.width-split, m.height, "", panePadding, "\n"+main)
	if split == 0 {
		return feedPane
	}
	return look.JoinFixedPanes(m.railPane(split), feedPane)
}

func (m Model) railPane(width int) string {
	rows, _ := m.railView()
	shown := strings.SplitAfterN(rows, "\n", m.height+1)
	rail := strings.Join(shown[:min(len(shown), m.height)], "")
	return strings.ReplaceAll(m.rail.Surface(width, m.height, look.Panel, panePadding, rail), spinnerCell, progress.Work(m.frame))
}

func (m Model) visible() []Event {
	if m.filter == (identity{}) {
		return m.retained()
	}
	var events []Event
	for _, e := range m.retained() {
		if m.shows(e) {
			events = append(events, e)
		}
	}
	return events
}

func (m Model) shows(e Event) bool {
	if m.filter == (identity{}) || actor(e) == m.filter {
		return true
	}
	with, _, talking := talk(e)
	return talking && with == m.filter
}

func talk(e Event) (with identity, said []string, talking bool) {
	if e.Kind != KindTool {
		return identity{}, nil, false
	}
	switch e.Title {
	case askTool:
		return identity{name: orchestrator}, append([]string{e.Actor + askedWord + e.Body}, e.Detail...), true
	case messageTool:
		to, text, _ := strings.Cut(e.Body, toldWord)
		return identity{name: to}, append([]string{e.Actor + toldWord + text}, e.Detail...), true
	}
	return identity{}, nil, false
}

type hit struct {
	id         string
	start, end int
}

type page struct {
	view               string
	hits               []hit
	first, last, total int
}

func fromTop(rows, height, scroll int) int {
	return max(0, rows-height-min(scroll, max(0, rows-height)))
}

func (m Model) page() page {
	s, height := m.laid(m.depth(), ""), m.pageHeight()
	p := page{total: len(s.drafts)}
	if len(s.drafts) == 0 {
		p.view = look.Muted("No events in this feed")
		return p
	}
	top := fromTop(s.rows, height, m.scroll)
	bottom := top + height
	shown := make([]string, 0, height)
	after, _ := slices.BinarySearch(s.starts, top+1)
	for i := max(0, after-1); i < len(s.drafts) && s.starts[i] < bottom; i++ {
		end := s.starts[i] + s.heights[i]
		if end > top {
			c := m.card(s.key.width, s.drafts[i])
			if p.first == 0 {
				p.first = i + 1
			}
			p.last = i + 1
			p.hits = append(p.hits, hit{c.id, max(0, s.starts[i]-top), min(height, end-top)})
			lines := strings.Split(c.view, "\n")
			shown = append(shown, lines[max(0, top-s.starts[i]):min(len(lines), bottom-s.starts[i])]...)
		}
		if i+1 < len(s.drafts) {
			for row := max(end, top); row < min(s.starts[i+1], bottom); row++ {
				shown = append(shown, "")
			}
		}
	}
	p.view = strings.Join(shown, "\n")
	return p
}

type group int

const (
	active group = iota
	waiting
	dead
)

func groupOf(state roster.State) group {
	switch state {
	case roster.Working, roster.Reopened:
		return active
	case roster.WaitingAnswer, roster.InReview, roster.Parked:
		return waiting
	case roster.Errored, roster.Finished:
		return dead
	}
	panic("feed: unknown sub-agent state " + state.String())
}

func stateWord(state roster.State) string {
	switch state {
	case roster.Working, roster.Reopened:
		return ""
	case roster.WaitingAnswer:
		return "waiting on you"
	case roster.InReview:
		return "in review"
	case roster.Parked:
		return "stopped"
	case roster.Errored:
		return "failed"
	case roster.Finished:
		return doneWord
	}
	panic("feed: unknown sub-agent state " + state.String())
}

type entry struct {
	who          identity
	group        group
	glyph, doing string
}

func (m Model) glyph(g group) string {
	switch g {
	case active:
		return look.Accent(spinnerCell)
	case waiting:
		return "."
	}
	return look.Accent("✓")
}

func (m Model) entries() []entry {
	lead := entry{who: identity{name: orchestrator}, group: waiting, doing: "Waiting for request"}
	if m.orchestratorBusy {
		lead.group, lead.doing = active, "Working"
	}
	lead.glyph = m.glyph(lead.group)
	all := []entry{lead}
	for _, a := range m.agents {
		g := groupOf(a.State)
		glyph := m.glyph(g)
		if a.State == roster.Errored {
			glyph = look.Style(look.Red).Render("✗")
		}
		doing := a.Doing
		if word := stateWord(a.State); word != "" {
			doing = word + " · " + a.Doing
		}
		all = append(all, entry{identity{a.Name, a.Instance}, g, glyph, doing})
	}
	slices.SortStableFunc(all, func(a, b entry) int { return cmp.Compare(a.group, b.group) })
	return all
}

type railTarget struct {
	row int
	who identity
}

type railRow struct {
	width               int
	selected            bool
	label, glyph, doing string
}

func (c *cardCache) railEntry(row railRow) string {
	if drawn, ok := c.railRows[row]; ok {
		return drawn
	}
	if len(c.railRows) >= railCacheCap || c.railRows == nil {
		c.railRows = make(map[railRow]string)
	}
	drawn := look.SidebarEntry(row.width, row.selected, row.label, row.glyph, row.doing)
	c.railRows[row] = drawn
	return drawn
}

func (m Model) railView() (string, []railTarget) {
	width := m.Split() - 2*panePadding
	var b strings.Builder
	rows := 0
	write := func(text string) {
		b.WriteString(text)
		rows += strings.Count(text, "\n")
	}
	write("\n" + look.PaneTitle("Sub-agents", m.railFocused) + "\n" + look.Muted(widget.Fit(fmt.Sprintf("%d agents · live activity", len(m.agents)), width)) + "\n\n" + look.SectionLabel("Overview") + "\n")
	targets := []railTarget{{rows, identity{}}}
	write(look.SidebarItem(width, m.filter == identity{}, allActivity, strconv.Itoa(m.sheet().kept)) + "\n")
	entries := m.entries()
	for g, name := range [...]string{"Active", "Waiting", "Dead"} {
		members := slices.DeleteFunc(slices.Clone(entries), func(e entry) bool { return e.group != group(g) })
		write("\n" + look.SectionLabel(name) + look.Faint("  "+strconv.Itoa(len(members))) + "\n")
		for _, e := range members {
			targets = append(targets, railTarget{rows, e.who})
			write(m.cards.railEntry(railRow{width, m.filter == e.who, e.who.label(), e.glyph, e.doing}) + "\n")
			if e.who.name == orchestrator && m.plan != "" {
				write("  " + look.Faint(widget.Fit(m.plan, width-2)) + "\n")
			}
		}
	}
	return b.String(), targets
}
