package links

import (
	"strconv"
	"strings"

	"tofu/interface/tui/pane"
	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	minimumWidth = 20
	gap          = "  "
	pickedMark   = "› "
	plainMark    = "  "
	title        = "links"
	pickHint     = "↑↓ pick   enter copies   type to filter"
	emptyTitle   = "this session carried no link"
	filterHint   = "filter: "
	noMatch      = "nothing here matches "
	timesMark    = "×"
)

type Model struct {
	found   []Link
	trouble string
	query   string
	pick    int
	width   int
	height  int
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m *Model) Set(found []Link, trouble string) {
	m.found, m.trouble, m.query, m.pick = found, trouble, "", 0
}

func (m Model) rows() []Link {
	if m.query == "" {
		return m.found
	}
	folded := strings.ToLower(m.query)
	var shown []Link
	for _, one := range m.found {
		if strings.Contains(strings.ToLower(one.URL+" "+one.From), folded) {
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

func (m Model) Picked() (Link, bool) {
	rows := m.rows()
	if m.pick >= len(rows) {
		return Link{}, false
	}
	return rows[m.pick], true
}

func (m Model) View() string {
	rows := m.rows()
	lines := []string{pane.Cell(title+gap+summary(len(rows)), m.width, theme.Accent()), pane.Cell("", m.width, theme.Text())}
	if len(rows) == 0 {
		lines = append(lines, pane.Cell(m.nothing(), m.width, theme.Faint()))
	}
	fromRoom, countRoom := m.columns(rows)
	for index, one := range rows {
		style := theme.Text()
		if index == m.pick {
			style = theme.Accent()
		}
		lines = append(lines, pane.Cell(m.row(index, one, fromRoom, countRoom), m.width, style))
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
		return "1 link"
	}
	return strconv.Itoa(shown) + " links"
}

func repeats(one Link) string {
	if one.Count < 2 {
		return ""
	}
	return timesMark + strconv.Itoa(one.Count)
}

func (m Model) columns(rows []Link) (int, int) {
	from := widget.Column(rows, func(one Link) string { return one.From }, 0)
	return min(from, m.width/4), widget.Column(rows, repeats, 0)
}

func (m Model) row(index int, one Link, fromRoom, countRoom int) string {
	mark := plainMark
	if index == m.pick {
		mark = pickedMark
	}
	tail := gap + widget.Pad(widget.Fit(one.From, fromRoom), fromRoom) + gap + widget.Pad(repeats(one), countRoom)
	room := max(m.width-widget.Cells(mark)-widget.Cells(tail), 1)
	return mark + widget.Pad(widget.Fit(one.URL, room), room) + tail
}
