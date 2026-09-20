package crew

import (
	"strconv"
	"strings"
	"time"

	"boji/interface/tui/pane"
	"boji/interface/tui/theme"
	roster "boji/internal/crew"
	"boji/internal/widget"
)

const (
	minimumWidth  = 20
	nameColumn    = 10
	sinceColumn   = 6
	progressDots  = 7
	watchShare    = 3
	watchMinimum  = 22
	gap           = "  "
	indent        = "    "
	divider       = " │ "
	holderJoin    = " ∩ "
	holdArrow     = " → "
	pickedMark    = "›"
	runningMark   = "● "
	doneMark      = "✓ "
	handbackMark  = "⤺ "
	filledDot     = "▪"
	emptyDot      = "▫"
	callMarker    = "⟩ "
	resultMarker  = "  "
	reportMarker  = "▌ "
	ownershipHead = "ownership"
	overlapMark   = "(overlap)"
	title         = "crew"
	emptyTitle    = "no child is holding any paths in this session"
	emptyBody     = "when the turn hands a ticket to a child, the child appears here with the globs it holds, what it is doing, and its report when it ends."
	pickHint      = "↑↓ pick a child"
	watchHint     = "pick a child to watch it work"
	quietChild    = "no tool call yet"
)

type State int

const (
	Running State = iota
	Done
	HandedBack
)

type Call struct {
	Tool   string
	Text   string
	Result string
}

type Child struct {
	Name   string
	Owns   []string
	Doing  string
	Since  time.Duration
	Steps  int
	Total  int
	Tokens int
	State  State
	Calls  []Call
	Report string
}

type Model struct {
	Children []Child
	pick     int
	width    int
	height   int
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m *Model) Key(key string) {
	switch key {
	case "down", "j":
		m.move(1)
	case "up", "k":
		m.move(-1)
	}
}

func (m *Model) move(by int) {
	if next := m.pick + by; next >= 0 && next <= len(m.Children) {
		m.pick = next
	}
}

func (m Model) Running() int {
	count := 0
	for _, child := range m.Children {
		if child.State == Running {
			count++
		}
	}
	return count
}

func (m Model) summary() string {
	count := strconv.Itoa(len(m.Children)) + " children"
	if len(m.Children) == 1 {
		count = "1 child"
	}
	return count + ", " + strconv.Itoa(m.Running()) + " running"
}

func (m Model) View() string {
	if len(m.Children) == 0 {
		return strings.Join(pane.Fill(m.nobody(), m.height, m.width), "\n")
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

func (m Model) nobody() []string {
	lines := []string{pane.Cell(emptyTitle, m.width, theme.Dim()), pane.Cell("", m.width, theme.Dim())}
	return append(lines, pane.Block("", emptyBody, m.width, theme.Faint())...)
}

func (m Model) list(width int) []string {
	blank := pane.Cell("", width, theme.Text())
	lines := []string{pane.Cell(title+gap+m.summary(), width, theme.Accent()), blank}
	for index, child := range m.Children {
		style := theme.Text()
		if index+1 == m.pick {
			style = theme.Accent()
		}
		lines = append(lines, pane.Cell(m.row(index, child, width), width, style))
	}
	lines = append(lines, blank, pane.Cell(ownershipHead, width, theme.Dim()))
	for _, held := range regions(m.Children) {
		style := theme.Dim()
		if len(held.holders) > 1 {
			style = theme.Warn()
		}
		lines = append(lines, pane.Block(indent, held.text(), width, style)...)
	}
	return append(lines, blank, pane.Cell(pickHint, width, theme.Faint()))
}

func (m Model) row(index int, child Child, width int) string {
	picked := " "
	if index+1 == m.pick {
		picked = pickedMark
	}
	head := picked + Mark(child.State) + widget.Pad(child.Name, nameColumn)
	tail := widget.Lead(widget.Elapsed(child.Since), sinceColumn) + gap + widget.Pad(dots(child), progressDots)
	room := max(width-widget.Cells(head)-widget.Cells(tail), 1)
	return head + widget.Pad(widget.Fit(child.Doing, room), room) + tail
}

func (m Model) watch(width int) []string {
	child, picked := m.chosen()
	if !picked {
		return pane.Block("", watchHint, width, theme.Faint())
	}
	lines := []string{pane.Cell(child.Name, width, theme.Accent()), pane.Cell("", width, theme.Text())}
	for _, call := range child.Calls {
		lines = append(lines, pane.Block(callMarker, call.Tool+gap+call.Text, width, theme.Tool())...)
		lines = append(lines, pane.Block(resultMarker, call.Result, width, theme.Faint())...)
	}
	if len(child.Calls) == 0 {
		lines = append(lines, pane.Cell(quietChild, width, theme.Faint()))
	}
	if child.Report == "" {
		return lines
	}
	lines = append(lines, pane.Cell("", width, theme.Text()))
	return append(lines, pane.Block(reportMarker, child.Report, width, theme.Text())...)
}

func (m Model) chosen() (Child, bool) {
	if m.pick == 0 {
		return Child{}, false
	}
	return m.Children[m.pick-1], true
}

type region struct {
	globs   []string
	holders []string
}

func (r region) text() string {
	text := strings.Join(r.holders, holderJoin) + holdArrow + strings.Join(r.globs, " ")
	if len(r.holders) > 1 {
		return text + " " + overlapMark
	}
	return text
}

func regions(children []Child) []region {
	var held []region
	for _, child := range children {
		for _, glob := range child.Owns {
			held = append(held, region{globs: []string{glob}, holders: []string{child.Name}})
		}
	}
	for left := 0; left < len(held); left++ {
		for right := left + 1; right < len(held); right++ {
			if !touching(held[left], held[right]) {
				continue
			}
			for _, glob := range held[right].globs {
				held[left].globs = once(held[left].globs, glob)
			}
			for _, holder := range held[right].holders {
				held[left].holders = once(held[left].holders, holder)
			}
			held = append(held[:right], held[right+1:]...)
			right = left
		}
	}
	return held
}

func touching(left, right region) bool {
	for _, one := range left.globs {
		for _, other := range right.globs {
			if overlapping(one, other) {
				return true
			}
		}
	}
	return false
}

func overlapping(one, other string) bool {
	var alone, together roster.Roster
	if alone.Hold(roster.SubAgent{ID: "other", Owns: []string{other}}) != nil ||
		together.Hold(roster.SubAgent{ID: "one", Owns: []string{one}}) != nil {
		return false
	}
	return together.Hold(roster.SubAgent{ID: "other", Owns: []string{other}}) != nil
}

func once(list []string, value string) []string {
	for _, held := range list {
		if held == value {
			return list
		}
	}
	return append(list, value)
}

func Mark(state State) string {
	switch state {
	case Running:
		return runningMark
	case Done:
		return doneMark
	case HandedBack:
		return handbackMark
	}
	panic("crew: unknown child state")
}

func dots(child Child) string {
	if child.State != Running || child.Total <= 0 {
		return ""
	}
	filled := min(child.Steps*progressDots/child.Total, progressDots)
	return strings.Repeat(filledDot, filled) + strings.Repeat(emptyDot, progressDots-filled)
}
