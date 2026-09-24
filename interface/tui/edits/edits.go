package edits

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/pane"
	"tofu/interface/tui/subagent"
	"tofu/interface/tui/theme"
	"tofu/interface/tui/trace"
	"tofu/internal/widget"
)

const (
	editWindow   = 500
	drawWindow   = 400
	minimumWidth = 20
	sidebarShare = 2
	markerRoom   = 1
	gap          = "  "
	indent       = "  "
	tabStop      = "    "
	divider      = " │ "
	pickedMark   = "›"
	hunkMark     = "@@"
	addedMark    = "+"
	removedMark  = "-"
	fromHeader   = "--- "
	intoHeader   = "+++ "
	title        = "file edits"
	feedLabel    = "  feed"
	Self         = "tofu"
	emptyTitle   = "no file has changed in this session"
	emptyBody    = "when the turn edits or writes a file the change appears here with its diff, and the session keeps one row saying which file changed and by how much."
	quietAgent   = "this agent has changed no file"
	undrawnNote  = "… %d lines in all, the first %d drawn"
	editedVerb   = "edited"
	createdVerb  = "created"
	pickHint     = "↑↓ pick"
	scrollHint   = "pgup older"
	whenFormat   = "15:04:05"
	oscStart     = "\x1b]8;;"
	oscEnd       = "\x1b\\"
)

type Edit struct {
	Agent   string
	Path    string
	ID      string
	When    time.Time
	verb    string
	body    string
	undrawn string
	added   int
	removed int
}

func Changed(agent, path, diff, created, id string, when time.Time) (Edit, bool) {
	edit := Edit{Agent: cmp.Or(agent, Self), Path: path, ID: id, When: when, verb: editedVerb}
	switch {
	case diff != "":
		for _, header := range []string{fromHeader, intoHeader} {
			if strings.HasPrefix(diff, header) {
				_, diff, _ = strings.Cut(diff, "\n")
			}
		}
		edit.body = expand(diff)
		for line := range strings.SplitSeq(edit.body, "\n") {
			switch {
			case strings.HasPrefix(line, addedMark):
				edit.added++
			case strings.HasPrefix(line, removedMark):
				edit.removed++
			}
		}
	case created != "":
		edit.verb = createdVerb
		lines := strings.Split(expand(created), "\n")
		edit.added = len(lines)
		if len(lines) > drawWindow {
			edit.undrawn = fmt.Sprintf(undrawnNote, len(lines), drawWindow)
			lines = lines[:drawWindow]
		}
		for index, line := range lines {
			lines[index] = addedMark + line
		}
		edit.body = strings.Join(lines, "\n")
	default:
		return Edit{}, false
	}
	return edit, true
}

func expand(text string) string {
	return strings.ReplaceAll(strings.TrimRight(text, "\n"), "\t", tabStop)
}

func (e Edit) Tally() string {
	return addedMark + strconv.Itoa(e.added) + " " + removedMark + strconv.Itoa(e.removed)
}

func hyperlink(root, path string) string {
	target := path
	if root != "" && !filepath.IsAbs(path) {
		target = filepath.ToSlash(filepath.Join(root, path))
	}
	uri := "file://" + target
	return oscStart + uri + oscEnd + path + oscStart + oscEnd
}

type agent struct {
	name  string
	state subagent.State
	edits int
}

type Model struct {
	Children []subagent.Child
	Busy     bool
	Root     string
	edits    []Edit
	pick     int
	top      int
	width    int
	height   int
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m *Model) Add(edit Edit) {
	m.edits = append(m.edits, edit)
	if len(m.edits) > editWindow {
		m.edits = m.edits[len(m.edits)-editWindow:]
	}
}

func (m *Model) Key(key string) {
	switch key {
	case "down", "j":
		m.move(1)
	case "up", "k":
		m.move(-1)
	case "pgdown":
		m.scroll(1)
	case "pgup":
		m.scroll(-1)
	case "home":
		m.top = 0
	}
}

func (m *Model) move(by int) {
	if next := m.pick + by; next >= 0 && next <= len(m.Children)+1 {
		m.pick, m.top = next, 0
	}
}

func (m *Model) scroll(by int) {
	picked, shown := m.picked(), 0
	for _, edit := range m.edits {
		if picked == "" || picked == edit.Agent {
			shown++
		}
	}
	m.top = min(max(m.top+by, 0), max(shown-1, 0))
}

func (m Model) agents() []agent {
	self := agent{name: Self, state: subagent.Done}
	if m.Busy {
		self.state = subagent.Running
	}
	children := make([]agent, 0, len(m.Children))
	for _, child := range m.Children {
		children = append(children, agent{name: child.Name, state: child.State})
	}
	slices.SortStableFunc(children, func(left, right agent) int {
		return running(right.state) - running(left.state)
	})
	rows := append([]agent{self}, children...)
	for index := range rows {
		for _, edit := range m.edits {
			if edit.Agent == rows[index].name {
				rows[index].edits++
			}
		}
	}
	return rows
}

func running(state subagent.State) int {
	if state == subagent.Running {
		return 1
	}
	return 0
}

func (m Model) picked() string {
	rows := m.agents()
	if m.pick <= 0 || m.pick > len(rows) {
		return ""
	}
	return rows[m.pick-1].name
}

func (m Model) View() string {
	if len(m.edits) == 0 {
		return strings.Join(pane.Fill(m.nothing(), m.height, m.width), "\n")
	}
	sidebar, sideWidth := m.sidebar()
	feedWidth := max(m.width-sideWidth-widget.Cells(divider), minimumWidth)
	side := pane.Fill(sidebar, m.height, sideWidth)
	feed := pane.Fill(m.feed(feedWidth), m.height, feedWidth)
	rows := make([]string, m.height)
	for row := range rows {
		rows[row] = side[row] + theme.Rule().Render(divider) + feed[row]
	}
	return strings.Join(rows, "\n")
}

func (m Model) nothing() []string {
	lines := []string{pane.Cell(emptyTitle, m.width, theme.Dim()), pane.Cell("", m.width, theme.Dim())}
	return append(lines, pane.Block("", emptyBody, m.width, theme.Faint())...)
}

type sideRow struct {
	label string
	count string
}

func (m Model) sidebar() ([]string, int) {
	rows := []sideRow{{label: feedLabel, count: strconv.Itoa(len(m.edits))}}
	for _, found := range m.agents() {
		rows = append(rows, sideRow{label: subagent.Mark(found.state) + found.name, count: strconv.Itoa(found.edits)})
	}
	least := max(widget.Cells(title), widget.Cells(pickHint), widget.Cells(scrollHint))
	space := strings.Repeat(" ", markerRoom)
	width := widget.Column(rows, func(row sideRow) string { return space + row.label + gap + row.count }, least)
	width = min(width, m.width/sidebarShare)
	blank := pane.Cell("", width, theme.Text())
	lines := []string{pane.Cell(title, width, theme.Accent()), blank}
	for index, row := range rows {
		marker, style := " ", theme.Text()
		if index == m.pick {
			marker, style = pickedMark, theme.Accent()
		}
		room := max(width-widget.Cells(row.count), 1)
		lines = append(lines, style.Render(widget.Pad(widget.Fit(marker+row.label, room), room)+row.count))
	}
	return append(lines, blank, pane.Cell(pickHint, width, theme.Faint()), pane.Cell(scrollHint, width, theme.Faint())), width
}

func (m Model) feed(width int) []string {
	picked := m.picked()
	lines := make([]string, 0, m.height)
	skipped := 0
	for index := len(m.edits) - 1; index >= 0 && len(lines) < m.height; index-- {
		edit := m.edits[index]
		if picked != "" && picked != edit.Agent {
			continue
		}
		if skipped < m.top {
			skipped++
			continue
		}
		lines = append(lines, m.editRows(edit, width, m.height-len(lines))...)
	}
	if len(lines) == 0 {
		return pane.Block("", quietAgent, width, theme.Faint())
	}
	return lines
}

func (m Model) editRows(edit Edit, width, room int) []string {
	head := edit.Agent + " " + edit.verb + " " + hyperlink(m.Root, edit.Path)
	if !edit.When.IsZero() {
		head += gap + edit.When.Format(whenFormat)
	}
	if id := trace.Short(edit.ID); id != "" {
		head += gap + id
	}
	tally := edit.Tally()
	named := max(width-widget.Cells(tally)-widget.Cells(gap), 1)
	lines := []string{theme.Text().Render(widget.Pad(widget.Fit(head, named), named)) + gap + theme.Dim().Render(tally)}
	if edit.undrawn != "" {
		lines = append(lines, pane.Cell(indent+edit.undrawn, width, theme.Dim()))
	}
	for line := range strings.SplitSeq(edit.body, "\n") {
		if len(lines) >= room {
			return lines
		}
		lines = append(lines, pane.Cell(indent+line, width, styleOf(line)))
	}
	return append(lines, pane.Cell("", width, theme.Text()))
}

func styleOf(line string) lipgloss.Style {
	switch {
	case strings.HasPrefix(line, hunkMark):
		return theme.Dim()
	case strings.HasPrefix(line, addedMark):
		return theme.Added()
	case strings.HasPrefix(line, removedMark):
		return theme.Removed()
	}
	return theme.Faint()
}
