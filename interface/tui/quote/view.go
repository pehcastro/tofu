package quote

import (
	"strconv"
	"strings"

	"tofu/interface/tui/pane"
	"tofu/interface/tui/theme"
	"tofu/interface/tui/trace"
	"tofu/internal/widget"
)

const (
	minimumWidth = 20
	gap          = "  "
	pickedMark   = "› "
	plainMark    = "  "
	title        = "quote"
	pickHint     = "↑↓ pick   enter writes the reference   type to filter"
	emptyTitle   = "this session has nothing to quote yet"
	filterHint   = "filter: "
	noMatch      = "nothing here matches "
)

type Model struct {
	turns   []Turn
	trouble string
	query   string
	pick    int
	width   int
	height  int
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m *Model) Set(turns []Turn, trouble string) {
	m.turns, m.trouble, m.query, m.pick = turns, trouble, "", 0
}

func (m Model) rows() []Turn {
	if m.query == "" {
		return m.turns
	}
	folded := strings.ToLower(m.query)
	var shown []Turn
	for _, one := range m.turns {
		if strings.Contains(strings.ToLower(one.Text+" "+one.From+" "+trace.Short(one.Event)), folded) {
			shown = append(shown, one)
		}
	}
	return shown
}

func (m *Model) Key(key string) {
	switch key {
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	case "backspace":
		if m.query != "" {
			m.query, m.pick = m.query[:len(m.query)-1], 0
		}
	default:
		if letter := []rune(key); len(letter) == 1 && letter[0] >= ' ' {
			m.query, m.pick = m.query+key, 0
		}
	}
}

func (m *Model) move(by int) {
	if next := m.pick + by; next >= 0 && next < len(m.rows()) {
		m.pick = next
	}
}

func (m Model) Picked() (Turn, bool) {
	rows := m.rows()
	if m.pick >= len(rows) {
		return Turn{}, false
	}
	return rows[m.pick], true
}

func (m Model) View() string {
	rows := m.rows()
	lines := []string{pane.Cell(title+gap+summary(len(rows)), m.width, theme.Accent()), pane.Cell("", m.width, theme.Text())}
	if len(rows) == 0 {
		lines = append(lines, pane.Cell(m.nothing(), m.width, theme.Faint()))
	}
	idRoom, fromRoom := m.columns(rows)
	for index, one := range rows {
		style := theme.Text()
		if index == m.pick {
			style = theme.Accent()
		}
		lines = append(lines, pane.Cell(m.row(index, one, idRoom, fromRoom), m.width, style))
	}
	lines = append(lines, pane.Cell("", m.width, theme.Text()), pane.Cell(pickHint, m.width, theme.Faint()))
	if m.query != "" {
		lines = append(lines, pane.Cell(filterHint+m.query, m.width, theme.Text()))
	}
	return strings.Join(pane.Fill(lines, m.height, m.width), "\n")
}

func (m Model) nothing() string {
	switch {
	case m.trouble != "":
		return m.trouble
	case m.query != "":
		return noMatch + m.query
	}
	return emptyTitle
}

func summary(shown int) string {
	if shown == 1 {
		return "1 turn"
	}
	return strconv.Itoa(shown) + " turns"
}

func (m Model) columns(rows []Turn) (int, int) {
	id := widget.Column(rows, func(one Turn) string { return trace.Short(one.Event) }, 0)
	from := widget.Column(rows, func(one Turn) string { return one.From }, 0)
	return id, min(from, m.width/4)
}

func (m Model) row(index int, one Turn, idRoom, fromRoom int) string {
	mark := plainMark
	if index == m.pick {
		mark = pickedMark
	}
	head := mark + widget.Pad(trace.Short(one.Event), idRoom) + gap + widget.Pad(widget.Fit(one.From, fromRoom), fromRoom) + gap
	room := max(m.width-widget.Cells(head), 1)
	return head + widget.Pad(widget.Fit(one.Text, room), room)
}
