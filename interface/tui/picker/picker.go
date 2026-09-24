package picker

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
	filterHint   = "filter: "
	noMatch      = "nothing here matches "
)

type Column[T any] struct {
	Text func(T) string
	Wide bool
	Most func(total int) int
}

type Look[T any] struct {
	Title   string
	One     string
	Many    string
	Hint    string
	Empty   string
	Match   func(T) string
	Columns []Column[T]
}

func Quarter(total int) int { return total / 4 }

type Model[T any] struct {
	look    Look[T]
	all     []T
	trouble string
	query   string
	pick    int
	width   int
	height  int
}

func (m *Model[T]) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m *Model[T]) Set(look Look[T], all []T, trouble string) {
	m.look, m.all, m.trouble, m.query, m.pick = look, all, trouble, "", 0
}

func (m Model[T]) Rows() []T {
	if m.query == "" {
		return m.all
	}
	folded := strings.ToLower(m.query)
	var shown []T
	for _, one := range m.all {
		if strings.Contains(strings.ToLower(m.look.Match(one)), folded) {
			shown = append(shown, one)
		}
	}
	return shown
}

func (m *Model[T]) Key(key string) {
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

func (m *Model[T]) move(by int) {
	if next := m.pick + by; next >= 0 && next < len(m.Rows()) {
		m.pick = next
	}
}

func (m Model[T]) Picked() (T, bool) {
	rows := m.Rows()
	if m.pick >= len(rows) {
		var none T
		return none, false
	}
	return rows[m.pick], true
}

func (m Model[T]) View() string {
	rows := m.Rows()
	lines := []string{pane.Cell(m.look.Title+gap+m.summary(len(rows)), m.width, theme.Accent()), pane.Cell("", m.width, theme.Text())}
	if len(rows) == 0 {
		lines = append(lines, pane.Cell(m.nothing(), m.width, theme.Faint()))
	}
	rooms := m.rooms(rows)
	for index, one := range rows {
		style := theme.Text()
		if index == m.pick {
			style = theme.Accent()
		}
		lines = append(lines, pane.Cell(m.row(index, one, rooms), m.width, style))
	}
	lines = append(lines, pane.Cell("", m.width, theme.Text()), pane.Cell(m.look.Hint, m.width, theme.Faint()))
	if m.query != "" {
		lines = append(lines, pane.Cell(filterHint+m.query, m.width, theme.Text()))
	}
	return strings.Join(pane.Fill(lines, m.height, m.width), "\n")
}

func (m Model[T]) nothing() string {
	switch {
	case m.trouble != "":
		return m.trouble
	case m.query != "":
		return noMatch + m.query
	}
	return m.look.Empty
}

func (m Model[T]) summary(shown int) string {
	if shown == 1 {
		return "1 " + m.look.One
	}
	return strconv.Itoa(shown) + " " + m.look.Many
}

func (m Model[T]) rooms(rows []T) []int {
	rooms := make([]int, len(m.look.Columns))
	taken := widget.Cells(plainMark) + (len(m.look.Columns)-1)*widget.Cells(gap)
	wide := -1
	for index, column := range m.look.Columns {
		if column.Wide {
			wide = index
			continue
		}
		rooms[index] = widget.Column(rows, column.Text, 0)
		if column.Most != nil {
			rooms[index] = min(rooms[index], column.Most(m.width))
		}
		taken += rooms[index]
	}
	if wide >= 0 {
		rooms[wide] = max(m.width-taken, 1)
	}
	return rooms
}

func (m Model[T]) row(index int, one T, rooms []int) string {
	mark := plainMark
	if index == m.pick {
		mark = pickedMark
	}
	cells := make([]string, 0, len(rooms))
	for at, column := range m.look.Columns {
		cells = append(cells, widget.Pad(widget.Fit(column.Text(one), rooms[at]), rooms[at]))
	}
	return mark + strings.Join(cells, gap)
}
