package shells

import (
	"fmt"
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
	processRows    = 5
	processPadding = 1
	factLabel      = 10
	title          = "Shells"
	emptyBody      = "No background processes in this session yet."
	outputHint     = "  ·  wheel / PgUp / PgDn"
	killAskHint    = " request kill · confirmation required"
	killNowHint    = " kill"
	noOwner        = "tofu"
	unknownRuntime = "unknown"
)

type State string

const (
	Running State = "running"
	Exited  State = "exited"
	Killed  State = "killed"
)

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
}

type cache struct {
	sidebar, main, process look.PaneCache
	log, styled            string
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
	case !picked || entry.State != Running:
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
		return look.FixedBlock(m.width, m.height, "\n  "+look.Title(title)+"\n  "+look.Muted(emptyBody))
	}
	c := m.cache
	if c == nil {
		c = &cache{}
	}
	split := m.Split()
	return look.JoinFixedPanes(
		c.sidebar.Surface(split, m.height, look.Panel, panePadding, "\n"+m.sidebar(split-2*panePadding)),
		c.main.Surface(m.width-split, m.height, "", panePadding, "\n"+m.detail(c, m.width-split-2*panePadding-trackGap)),
	)
}

func (m Model) sidebar(width int) string {
	running := 0
	for _, entry := range m.Entries {
		if entry.State == Running {
			running++
		}
	}
	var out strings.Builder
	out.WriteString(look.PaneTitle(title, true) + "\n" + look.Faint(strconv.Itoa(running)+" running  ·  "+strconv.Itoa(len(m.Entries)-running)+" exited") + "\n\n")
	start, end := m.sidebarWindow()
	for index, entry := range m.Entries[start:end] {
		out.WriteString(look.SidebarEntry(width, start+index == m.pick, entry.Name, badge(entry), "pid "+strconv.Itoa(entry.PID)) + "\n")
	}
	return out.String()
}

func (m Model) detail(c *cache, width int) string {
	entry := m.Entries[m.pick]
	owner := look.Muted(noOwner)
	if entry.Owner != "" {
		owner = look.AgentRef(entry.Owner)
	}
	facts := m.facts(entry, width)
	output, _, _ := look.Window(c.styledLog(entry.Log), width, m.outputHeight(), m.scroll)
	view := look.Sides(look.Title(entry.Name), badge(entry), width) + "\n" + look.Muted("Owned by ") + owner + "\n\n" +
		c.process.Surface(processWidth(width), processHeight+len(facts)-processRows, look.PanelLight, processPadding, strings.Join(facts, "\n")) + "\n\n" +
		look.SectionLabel("Output") + look.Faint(outputHint) + "\n" + output + "\n"
	if entry.State != Running {
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
	lines := []string{look.SectionLabel("Process")}
	for _, fact := range [][2]string{{"PID", strconv.Itoa(entry.PID)}, {"Runtime", m.runtime(entry)}, {"CWD", entry.Dir}, {"Command", entry.Command}} {
		label := widget.Pad(fact[0], factLabel)
		for _, part := range widget.Wrap(fact[1], room) {
			lines = append(lines, look.Muted(label+part))
			label = strings.Repeat(" ", factLabel)
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
	switch entry.State {
	case Running:
		return look.StateBadge(string(entry.State), true)
	case Exited:
		if entry.ExitCode != nil {
			return look.StateBadge(string(entry.State)+" "+strconv.Itoa(*entry.ExitCode), false)
		}
		return look.StateBadge(string(entry.State), false)
	case Killed:
		return look.StateBadge(string(entry.State), false)
	}
	panic("shells: unknown state " + string(entry.State))
}

func (m Model) runtime(entry Entry) string {
	var end time.Time
	switch {
	case entry.Ended != nil:
		end = *entry.Ended
	case entry.State != Running:
		return unknownRuntime
	case m.now != nil:
		end = m.now()
	default:
		end = time.Now()
	}
	seconds := int(end.Sub(entry.Started) / time.Second)
	return fmt.Sprintf("%dm %02ds", seconds/60, seconds%60)
}

func (c *cache) styledLog(log string) string {
	if c.log == log {
		return c.styled
	}
	lines := strings.Split(strings.TrimSuffix(log, "\n"), "\n")
	for index, line := range lines {
		lines[index] = look.OutputLine(line)
	}
	c.log, c.styled = log, strings.Join(lines, "\n")
	return c.styled
}
