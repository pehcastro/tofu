package shells

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
	"tofu/internal/widget"
)

const (
	sidebarShare   = 4
	sidebarMaximum = 31
	sidebarMinimum = 18
	detailMinimum  = 26
	minimumWidth   = sidebarMinimum + detailMinimum
	panePadding    = 2
	trackGap       = 1
	entryTop       = 4
	entryRows      = 2
	outputChrome   = 15
	outputMinimum  = 4
	pageMinimum    = 4
	processMaximum = 70
	processHeight  = 6
	processRows    = 4
	processPadding = 1
	factLabel      = 10
	title          = "Shells"
	emptyBody      = "No background processes in this session yet."
	emptyRule      = "Only a process kept past its bash call shows here: a server or\n  watcher started in the background, or a command still running when\n  its call stops waiting, such as a long build or test run. A command\n  that ends inside its wait never shows."
	outputHint     = "  ·  wheel / PgUp / PgDn"
	silentRunning  = "nothing printed yet"
	silentEnded    = "printed nothing"
	killAskHint    = " request kill · confirmation required"
	killNowHint    = " kill"
	noOwner        = "tofu"
	unknownRuntime = "unknown"
)

type State string

const (
	Running  State = "running"
	LeftOver State = "left over"
	Exited   State = "exited"
	Killed   State = "killed"
)

func (s State) Endable() bool { return s == Running || s == LeftOver }

type Intent int

const (
	IntentNone Intent = iota
	IntentKillAsk
	IntentKillNow
)

type Entry struct {
	Name     string
	Command  string
	State    State
	Started  time.Time
	Ended    *time.Time
	ExitCode *int
	PID      int
	Dir      string
	Owner    string
	Log      string
	OneShot  bool
}

type row struct {
	name, label     string
	pid             int
	running, picked bool
}

type listing struct {
	width   int
	summary string
	rows    []row
	text    string
}

type window struct {
	log                   string
	width, height, scroll int
}

type cache struct {
	sidebar, main, process look.PaneCache
	listing                listing
	window                 window
	output                 string
}

type Model struct {
	Entries   []Entry
	pick      int
	scroll    int
	width     int
	height    int
	killNow   bool
	requested *Entry
	now       func() time.Time
	cache     *cache
}

func New(now func() time.Time) Model {
	return Model{now: now, cache: &cache{}}
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m *Model) SetKillConfirm(on bool) {
	m.killNow = !on
}

func (m *Model) Set(entries []Entry) {
	m.Entries = entries
	if m.pick >= len(entries) {
		m.pick = max(len(entries)-1, 0)
	}
	m.scrollBy(0)
}

func (m *Model) Remove(name string) {
	kept := m.Entries[:0]
	for _, entry := range m.Entries {
		if entry.Name != name {
			kept = append(kept, entry)
		}
	}
	if m.requested != nil && m.requested.Name == name {
		m.requested = nil
	}
	m.Set(kept)
}

func (m *Model) Key(key string) Intent {
	m.requested = nil
	switch key {
	case "down", "j":
		m.move(1)
	case "up":
		m.move(-1)
	case "pgup":
		m.scrollBy(max(pageMinimum, m.height/2))
	case "pgdown":
		m.scrollBy(-max(pageMinimum, m.height/2))
	case "k":
		return m.kill()
	}
	return IntentNone
}

func (m *Model) kill() Intent {
	entry, picked := m.Picked()
	switch {
	case !picked || !entry.State.Endable():
		return IntentNone
	case m.killNow:
		return IntentKillNow
	}
	m.requested = &entry
	return IntentKillAsk
}

func (m Model) KillRequested() (Entry, bool) {
	if m.requested == nil {
		return Entry{}, false
	}
	return *m.requested, true
}

func (m *Model) Wheel(delta int) {
	m.scrollBy(-delta)
}

func (m *Model) Click(x, y int) bool {
	start, end := m.sidebarWindow()
	row := y - entryTop
	if x < 0 || x >= m.Split() || row < 0 || row%entryRows != 0 || start+row/entryRows >= end {
		return false
	}
	m.pick, m.scroll = start+row/entryRows, 0
	return true
}

func (m *Model) move(by int) {
	if next := m.pick + by; next >= 0 && next < len(m.Entries) {
		m.pick, m.scroll = next, 0
	}
}

func (m *Model) scrollBy(lines int) {
	m.scroll = max(0, min(m.scroll+lines, m.logLines()-m.outputHeight()))
}

func (m Model) Picked() (Entry, bool) {
	if len(m.Entries) == 0 {
		return Entry{}, false
	}
	return m.Entries[m.pick], true
}

func (m Model) Split() int {
	return min(max(sidebarMinimum, min(sidebarMaximum, m.width/sidebarShare)), m.width-detailMinimum)
}

func (m Model) Track() pointer.Track {
	height, total := m.outputHeight(), m.logLines()
	return pointer.Track{Total: total, Visible: height, FromTop: max(0, total-height-m.scroll)}
}

func (m Model) outputHeight() int {
	return max(outputMinimum, m.height-outputChrome-m.extraFactRows())
}

func (m Model) logLines() int {
	entry, picked := m.Picked()
	log := strings.TrimSuffix(entry.Log, "\n")
	if !picked || log == "" {
		return 0
	}
	return strings.Count(log, "\n") + 1
}

func (m Model) sidebarWindow() (start, end int) {
	return look.VisibleRows(len(m.Entries), m.pick, max(1, (m.height-entryTop)/entryRows))
}

func (m Model) View() string {
	if len(m.Entries) == 0 {
		return look.FixedBlock(m.width, m.height, "\n  "+look.Title(title)+"\n  "+look.Muted(emptyBody)+"\n\n  "+look.Faint(emptyRule))
	}
	c := m.cache
	if c == nil {
		c = &cache{}
	}
	split := m.Split()
	return look.JoinFixedPanes(
		c.sidebar.Surface(split, m.height, look.Panel, panePadding, "\n"+m.sidebar(c, split-2*panePadding)),
		c.main.Surface(m.width-split, m.height, "", panePadding, "\n"+m.detail(c, m.width-split-2*panePadding-trackGap)),
	)
}

func (m Model) sidebar(c *cache, width int) string {
	running, leftOver := 0, 0
	for _, entry := range m.Entries {
		switch entry.State {
		case Running:
			running++
		case LeftOver:
			leftOver++
		case Exited, Killed:
		}
	}
	summary := strconv.Itoa(running) + " running  ·  " + strconv.Itoa(len(m.Entries)-running) + " exited"
	if leftOver > 0 {
		summary = strconv.Itoa(running) + " running  ·  " + strconv.Itoa(leftOver) + " left over"
	}
	start, end := m.sidebarWindow()
	rows := make([]row, 0, end-start)
	for index, entry := range m.Entries[start:end] {
		rows = append(rows, row{entry.Name, label(entry), entry.PID, entry.State == Running, start+index == m.pick})
	}
	if c.listing.width == width && c.listing.summary == summary && slices.Equal(c.listing.rows, rows) {
		return c.listing.text
	}
	var out strings.Builder
	out.WriteString(look.PaneTitle(title, true) + "\n" + look.Faint(summary) + "\n\n")
	for _, row := range rows {
		out.WriteString(look.SidebarEntry(width, row.picked, row.name, look.StateBadge(row.label, row.running), "pid "+strconv.Itoa(row.pid)) + "\n")
	}
	c.listing = listing{width, summary, rows, out.String()}
	return c.listing.text
}

func (m Model) detail(c *cache, width int) string {
	entry := m.Entries[m.pick]
	owner := look.Muted(noOwner)
	if entry.Owner != "" {
		owner = look.AgentRef(entry.Owner)
	}
	facts := m.facts(entry, width)
	process := look.SectionLabel("Process")
	for _, fact := range facts {
		process += "\n" + look.Muted(fact)
	}
	view := look.Sides(look.Title(entry.Name), badge(entry), width) + "\n" + look.Muted("Owned by ") + owner + "\n\n" +
		c.process.Surface(processWidth(width), processHeight+len(facts)-processRows, look.PanelLight, processPadding, process) + "\n\n" +
		look.SectionLabel("Output") + look.Faint(outputHint) + "\n" + m.output(c, entry, width) + "\n"
	if !entry.State.Endable() {
		return view
	}
	hint := killAskHint
	if m.killNow {
		hint = killNowHint
	}
	return view + "\n" + look.Style(look.Red).Render("k") + look.Muted(hint)
}

func processWidth(detail int) int { return min(detail-2, processMaximum) }

func (m Model) facts(entry Entry, detail int) []string {
	room := max(1, processWidth(detail)-2*processPadding-factLabel)
	var lines []string
	for _, fact := range [][2]string{{"PID", strconv.Itoa(entry.PID)}, {"Runtime", m.runtime(entry)}, {"CWD", entry.Dir}, {"Command", entry.Command}} {
		name := widget.Pad(fact[0], factLabel)
		for _, part := range widget.Wrap(fact[1], room) {
			lines = append(lines, name+part)
			name = strings.Repeat(" ", factLabel)
		}
	}
	return lines
}

func (m Model) extraFactRows() int {
	entry, picked := m.Picked()
	if !picked {
		return 0
	}
	return len(m.facts(entry, m.width-m.Split()-2*panePadding-trackGap)) - processRows
}

func badge(entry Entry) string {
	return look.StateBadge(label(entry), entry.State == Running)
}

func label(entry Entry) string {
	switch entry.State {
	case Exited:
		if entry.ExitCode != nil {
			return string(entry.State) + " " + strconv.Itoa(*entry.ExitCode)
		}
		return string(entry.State)
	case Running, Killed, LeftOver:
		return string(entry.State)
	}
	panic("shells: unknown state " + string(entry.State))
}

func (m Model) runtime(entry Entry) string {
	var end time.Time
	switch {
	case entry.Ended != nil:
		end = *entry.Ended
	case !entry.State.Endable():
		return unknownRuntime
	case m.now != nil:
		end = m.now()
	default:
		end = time.Now()
	}
	seconds := int(end.Sub(entry.Started) / time.Second)
	return fmt.Sprintf("%dm %02ds", seconds/60, seconds%60)
}

func (m Model) output(c *cache, entry Entry, width int) string {
	shown := window{entry.Log, width, m.outputHeight(), m.scroll}
	if strings.TrimSpace(entry.Log) == "" {
		silent := silentEnded
		if entry.State.Endable() {
			silent = silentRunning
		}
		view, _, _ := look.Window(look.Faint(silent), shown.width, shown.height, shown.scroll)
		return view
	}
	if c.window == shown {
		return c.output
	}
	lines := strings.Split(strings.TrimSuffix(shown.log, "\n"), "\n")
	fromTop := max(0, len(lines)-shown.height-shown.scroll)
	visible := lines[fromTop:min(len(lines), fromTop+shown.height)]
	for index, line := range visible {
		visible[index] = look.OutputLine(line)
	}
	c.window = shown
	c.output, _, _ = look.Window(strings.Join(visible, "\n"), shown.width, shown.height, 0)
	return c.output
}
