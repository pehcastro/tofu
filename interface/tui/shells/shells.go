package shells

import (
	"strconv"
	"strings"
	"time"

	"tofu/interface/tui/pane"
	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	minimumWidth = 20
	watchShare   = 2
	watchMinimum = 30
	divider      = " │ "
	pickedMark   = "›"
	runningMark  = "● "
	exitedMark   = "✓ "
	killedMark   = "✗ "
	gap          = "  "
	title        = "shells"
	pickHint     = "↑↓ pick"
	killHint     = "k kills the selected process"
	emptyTitle   = "no process is running in this session"
	emptyBody    = "a dev server, a build, a test run, anything an agent starts and leaves running appears here to inspect and to kill."
	watchHint    = "pick a process to read its log"
	quietLog     = "no output yet"
	clock        = "15:04:05"
)

type State string

const (
	Running State = "running"
	Exited  State = "exited"
	Killed  State = "killed"
)

func Mark(state State) string {
	switch state {
	case Running:
		return runningMark
	case Exited:
		return exitedMark
	case Killed:
		return killedMark
	}
	panic("shells: unknown state")
}

type Entry struct {
	Name     string
	Command  string
	State    State
	Started  time.Time
	ExitCode *int
	Log      string
}

type Model struct {
	Entries []Entry
	pick    int
	width   int
	height  int
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m *Model) Set(entries []Entry) {
	m.Entries = entries
	if m.pick >= len(entries) {
		m.pick = max(len(entries)-1, 0)
	}
}

func (m *Model) Remove(name string) {
	kept := m.Entries[:0]
	for _, entry := range m.Entries {
		if entry.Name != name {
			kept = append(kept, entry)
		}
	}
	m.Set(kept)
}

func (m *Model) Key(key string) {
	switch key {
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	}
}

func (m *Model) move(by int) {
	if next := m.pick + by; next >= 0 && next < len(m.Entries) {
		m.pick = next
	}
}

func (m Model) Picked() (Entry, bool) {
	if len(m.Entries) == 0 {
		return Entry{}, false
	}
	return m.Entries[m.pick], true
}

func (m Model) View() string {
	if len(m.Entries) == 0 {
		return strings.Join(pane.Fill(m.nothing(), m.height, m.width), "\n")
	}
	watchWidth := max(m.width/watchShare, watchMinimum)
	listWidth := max(m.width-watchWidth-widget.Cells(divider), minimumWidth)
	list := pane.Fill(m.list(listWidth), m.height, listWidth)
	watch := pane.Fill(m.watch(watchWidth), m.height, watchWidth)
	rows := make([]string, m.height)
	for row := range rows {
		rows[row] = list[row] + theme.Rule().Render(divider) + watch[row]
	}
	return strings.Join(rows, "\n")
}

func (m Model) nothing() []string {
	lines := []string{pane.Cell(emptyTitle, m.width, theme.Dim()), pane.Cell("", m.width, theme.Dim())}
	return append(lines, pane.Block("", emptyBody, m.width, theme.Faint())...)
}

func (m Model) list(width int) []string {
	blank := pane.Cell("", width, theme.Text())
	lines := []string{pane.Cell(title, width, theme.Accent()), blank}
	for index, entry := range m.Entries {
		style := theme.Text()
		if index == m.pick {
			style = theme.Accent()
		}
		lines = append(lines, pane.Cell(m.row(index, entry, width), width, style))
	}
	return append(lines, blank, pane.Cell(pickHint, width, theme.Faint()), pane.Cell(killHint, width, theme.Faint()))
}

func (m Model) row(index int, entry Entry, width int) string {
	picked := " "
	if index == m.pick {
		picked = pickedMark
	}
	head := picked + Mark(entry.State) + entry.Name
	tail := entry.Started.Format(clock)
	room := max(width-widget.Cells(head)-widget.Cells(gap)-widget.Cells(tail), 1)
	return head + widget.Pad(widget.Fit(gap+entry.Command, room), room) + gap + tail
}

func (m Model) watch(width int) []string {
	entry, picked := m.Picked()
	if !picked {
		return pane.Block("", watchHint, width, theme.Faint())
	}
	head := entry.Name + gap + string(entry.State) + gap + entry.Started.Format(clock)
	if entry.ExitCode != nil {
		head += gap + "exit " + strconv.Itoa(*entry.ExitCode)
	}
	lines := []string{pane.Cell(head, width, theme.Accent()), pane.Cell(entry.Command, width, theme.Faint()), pane.Cell("", width, theme.Text())}
	if entry.Log == "" {
		return append(lines, pane.Cell(quietLog, width, theme.Faint()))
	}
	for line := range strings.SplitSeq(entry.Log, "\n") {
		lines = append(lines, pane.Cell(line, width, theme.Tool()))
	}
	return lines
}
