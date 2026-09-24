package subagent

import (
	"strconv"
	"strings"
	"time"

	"tofu/interface/tui/pane"
	"tofu/interface/tui/progress"
	"tofu/interface/tui/theme"
	roster "tofu/internal/subagent"
	"tofu/internal/widget"
)

const (
	minimumWidth  = 20
	nameColumn    = 10
	sinceLeast    = 6
	progressDots  = 7
	watchShare    = 3
	watchMinimum  = 22
	gap           = "  "
	indent        = "    "
	divider       = " │ "
	holderJoin    = " ∩ "
	holdArrow     = " → "
	pickedMark    = "›"
	filledDot     = "▪"
	emptyDot      = "▫"
	callMarker    = "⟩ "
	resultMarker  = "  "
	reportMarker  = "▌ "
	ownershipHead = "ownership"
	overlapMark   = "(overlap)"
	title         = "sub-agents"
	emptyTitle    = "no child is holding any paths in this session"
	emptyBody     = "when the turn hands a ticket to a child, the child appears here with the globs it holds, what it is doing, and its report when it ends."
	pickHint      = "↑↓ pick a child"
	watchHint     = "pick a child to watch it work"
	quietChild    = "no tool call yet"
	rowsHidden    = " earlier rows hidden"
)

type State = roster.State

func spelling(state State) (mark, words string) {
	switch state {
	case roster.Working:
		return "● ", "working"
	case roster.WaitingAnswer:
		return "? ", "waiting for an answer"
	case roster.InReview:
		return "⤺ ", "in review"
	case roster.Parked:
		return "‖ ", "parked"
	case roster.Errored:
		return "✗ ", "errored"
	case roster.Finished:
		return "✓ ", "finished"
	}
	panic("subagent: unknown state " + state.String())
}

func Label(state State) string {
	_, words := spelling(state)
	return words
}

func Mark(state State) string {
	mark, _ := spelling(state)
	return mark
}

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
		if child.State == roster.Working {
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
	listHead, listBody := m.list(listWidth)
	watchHead, watchBody := m.watch(watchWidth)
	list := pane.Fill(keepingTheEnd(listHead, listBody, m.height, listWidth), m.height, listWidth)
	watch := pane.Fill(keepingTheEnd(watchHead, watchBody, m.height, watchWidth), m.height, watchWidth)
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

func (m Model) list(width int) (head []string, body [][]string) {
	blank := pane.Cell("", width, theme.Text())
	head = []string{pane.Cell(title+gap+m.summary(), width, theme.Accent()), blank}
	clock := widget.Column(m.Children, func(child Child) string { return widget.Until(child.Since) }, sinceLeast)
	for index, child := range m.Children {
		style := theme.Text()
		if index+1 == m.pick {
			style = theme.Accent()
		}
		head = append(head, pane.Cell(m.row(index, child, width, clock), width, style))
	}
	body = [][]string{{blank, pane.Cell(ownershipHead, width, theme.Dim())}}
	for _, held := range regions(m.Children) {
		style := theme.Path()
		if len(held.holders) > 1 {
			style = theme.Warn()
		}
		body = append(body, pane.Block(indent, held.text(), width, style))
	}
	return head, append(body, []string{blank, pane.Cell(pickHint, width, theme.Faint())})
}

func (m Model) row(index int, child Child, width, clock int) string {
	picked := " "
	if index+1 == m.pick {
		picked = pickedMark
	}
	head := picked + Mark(child.State) + widget.Pad(child.Name, nameColumn)
	tail := widget.Lead(widget.Until(child.Since), clock) + gap + widget.Pad(dots(child), progressDots)
	room := max(width-widget.Cells(head)-widget.Cells(tail), 1)
	return head + widget.Pad(widget.Fit(child.Doing, room), room) + tail
}

func (m Model) watch(width int) (head []string, body [][]string) {
	child, picked := m.chosen()
	if !picked {
		return nil, [][]string{pane.Block("", watchHint, width, theme.Faint())}
	}
	head = []string{pane.Cell(child.Name+"  "+Label(child.State), width, theme.Accent()), pane.Cell("", width, theme.Text())}
	if child.State == roster.Working {
		line := progress.Line{Label: child.Doing, Since: child.Since, Tick: progress.TickInterval, Live: true}
		head = append(head, pane.Raw(line.View(width), width), pane.Cell("", width, theme.Text()))
	}
	for _, call := range child.Calls {
		whole := pane.Block(callMarker, strings.TrimSpace(call.Tool+gap+call.Text), width, theme.Tool())
		if call.Result != "" {
			whole = append(whole, pane.Block(resultMarker, call.Result, width, theme.Faint())...)
		}
		body = append(body, whole)
	}
	if len(child.Calls) == 0 {
		body = append(body, []string{pane.Cell(quietChild, width, theme.Faint())})
	}
	if child.Report != "" {
		prose := append([]string{pane.Cell("", width, theme.Text())}, pane.Block(reportMarker, child.Report, width, theme.Text())...)
		for _, line := range prose {
			body = append(body, []string{line})
		}
	}
	return head, body
}

func keepingTheEnd(head []string, body [][]string, height, width int) []string {
	rows := len(head)
	for _, whole := range body {
		rows += len(whole)
	}
	if rows <= height {
		for _, whole := range body {
			head = append(head, whole...)
		}
		return head
	}
	kept := min(len(head), max(height-1, 0))
	room, shown, first := max(height-kept-1, 0), 0, len(body)
	for first > 0 && shown+len(body[first-1]) <= room {
		first--
		shown += len(body[first])
	}
	cut := make([]string, 0, height)
	cut = append(cut, head[:kept]...)
	cut = append(cut, pane.Cell(strconv.Itoa(rows-kept-shown)+rowsHidden, width, theme.Faint()))
	for _, whole := range body[first:] {
		cut = append(cut, whole...)
	}
	return cut
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

func dots(child Child) string {
	if child.State != roster.Working || child.Total <= 0 {
		return ""
	}
	filled := min(child.Steps*progressDots/child.Total, progressDots)
	return strings.Repeat(filledDot, filled) + strings.Repeat(emptyDot, progressDots-filled)
}
