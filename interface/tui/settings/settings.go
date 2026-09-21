package settings

import (
	"strconv"
	"strings"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	groupColumn  = 12
	nameColumn   = 30
	sourceMark   = "← "
	sourceGap    = 2
	indent       = "  "
	minimumWidth = 20
	searchHint   = "search: "
)

type Kind int

const (
	Bool Kind = iota
	Int
)

type Row struct {
	Key             string
	Group           string
	Label           string
	Value           string
	Kind            Kind
	Changed         bool
	RestartRequired bool
	RestartPending  bool
	Source          string
}

type Model struct {
	Providers      []Provider
	ChatShowsTools bool
	Rows           []Row
	Scopes         []string
	Scope          int
	Query          string
	cursor         int
	width          int
	height         int
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m *Model) ToggleChatShowsTools() { m.ChatShowsTools = !m.ChatShowsTools }

func (m *Model) SetRows(rows []Row) {
	m.Rows = rows
	if m.cursor >= len(rows)+1 {
		m.cursor = max(0, len(rows))
	}
}

type Action int

const (
	ActionNone Action = iota
	ActionToggle
	ActionIncrement
	ActionDecrement
	ActionCycleScope
)

type Intent struct {
	Action Action
	Key    string
}

func (m *Model) Key(key string) Intent {
	switch key {
	case "up":
		m.moveCursor(-1)
	case "down":
		m.moveCursor(1)
	case "left":
		return m.step(-1)
	case "right":
		return m.step(1)
	case "enter", "space":
		return m.step(1)
	case "backspace":
		if m.Query != "" {
			m.Query = m.Query[:len(m.Query)-1]
		}
	default:
		if r, ok := searchRune(key); ok {
			m.Query += string(r)
		}
	}
	return Intent{}
}

func searchRune(key string) (rune, bool) {
	runes := []rune(key)
	if len(runes) != 1 || runes[0] < ' ' {
		return 0, false
	}
	return runes[0], true
}

func (m *Model) moveCursor(by int) {
	last := len(m.Rows)
	m.cursor = min(max(m.cursor+by, 0), last)
}

func (m *Model) step(by int) Intent {
	if m.cursor == 0 {
		return Intent{Action: ActionCycleScope}
	}
	row := m.Rows[m.cursor-1]
	if row.Kind == Int {
		if by > 0 {
			return Intent{Action: ActionIncrement, Key: row.Key}
		}
		return Intent{Action: ActionDecrement, Key: row.Key}
	}
	return Intent{Action: ActionToggle, Key: row.Key}
}

func (m Model) View() string {
	lines := []string{theme.Accent().Render(widget.Fit("settings", m.width)), ""}
	lines = append(lines, providerLines(m.Providers, m.width)...)
	lines = append(lines, "", m.scopeLine())
	lines = append(lines, m.settingLines()...)
	if pending := m.restartLine(); pending != "" {
		lines = append(lines, "", theme.Warn().Render(widget.Fit(pending, m.width)))
	}
	if m.Query != "" {
		lines = append(lines, "", theme.Text().Render(widget.Fit(m.searchLine(), m.width)))
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) scopeLine() string {
	parts := make([]string, len(m.Scopes))
	for i, name := range m.Scopes {
		if i == m.Scope {
			parts[i] = "[" + name + "]"
			continue
		}
		parts[i] = name
	}
	cursor := " "
	if m.cursor == 0 {
		cursor = ">"
	}
	return theme.Text().Render(cursor + " scope: " + strings.Join(parts, "  "))
}

func (m Model) settingLines() []string {
	var lines []string
	group := ""
	for index, row := range m.Rows {
		if row.Group != group {
			group = row.Group
			lines = append(lines, "", theme.Dim().Render(indent+group))
		}
		lines = append(lines, m.settingRow(index, row))
	}
	return lines
}

func (m Model) settingRow(index int, row Row) string {
	cursor := " "
	if m.cursor == index+1 {
		cursor = ">"
	}
	mark := " "
	if row.Changed {
		mark = "*"
	}
	warn := ""
	if row.RestartPending {
		warn = " (needs restart)"
	}
	head := cursor + mark + " " + pad(row.Label, nameColumn) + row.Value + warn
	source := sourceMark + row.Source
	if gap, fits := fitsWithSource(head, source, m.width); fits {
		return theme.Text().Render(head) + theme.Faint().Render(gap+source)
	}
	return theme.Text().Render(widget.Fit(head, m.width))
}

func fitsWithSource(head, source string, width int) (string, bool) {
	if widget.Cells(head)+sourceGap+widget.Cells(source) > width {
		return "", false
	}
	return strings.Repeat(" ", width-widget.Cells(head)-widget.Cells(source)), true
}

func (m Model) restartLine() string {
	var pending []string
	for _, row := range m.Rows {
		if row.RestartPending {
			pending = append(pending, row.Key)
		}
	}
	if len(pending) == 0 {
		return ""
	}
	return "restart needed to apply: " + strings.Join(pending, ", ")
}

func (m Model) searchLine() string {
	word := "matches"
	if len(m.Rows) == 1 {
		word = "match"
	}
	return searchHint + m.Query + "  " + strconv.Itoa(len(m.Rows)) + " " + word
}

func pad(text string, width int) string {
	if widget.Cells(text) >= width {
		return text + " "
	}
	return text + strings.Repeat(" ", width-widget.Cells(text))
}
