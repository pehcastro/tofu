package palette

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
)

const (
	dialogMaxWidth  = 68
	confirmMaxWidth = 62
	dialogInset     = 8
	dialogPadding   = 2
	rowMinWidth     = 22
	dialogMargin    = 2
	paneMargin      = 1
	paneHeightInset = 4
	firstRowLine    = 6
	modalRowLines   = 3
	listChrome      = 13
	minVisibleRows  = 2
	filterInset     = 2
	growToContent   = 1
)

type Item struct{ Title, Description, Key, ID string }

type Choice struct {
	ID                      string
	Done, Cancelled, Inside bool
}

type list[T any] struct {
	input  textinput.Model
	cursor int
	shown  []T
	find   func(query string) []T
}

func newList[T any](find func(query string) []T) list[T] {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Type to filter"
	input.SetVirtualCursor(true)
	input.SetStyles(look.FilterStyles())
	input.Focus()
	return list[T]{input: input, shown: find(""), find: find}
}

func (l *list[T]) key(msg tea.KeyPressMsg) (picked T, done, cancelled bool, cmd tea.Cmd) {
	switch msg.String() {
	case "esc":
		return picked, false, true, nil
	case "up":
		l.cursor = wrap(l.cursor-1, len(l.shown))
		return picked, false, false, nil
	case "down":
		l.cursor = wrap(l.cursor+1, len(l.shown))
		return picked, false, false, nil
	case "enter":
		if len(l.shown) == 0 {
			return picked, false, false, nil
		}
		return l.shown[l.cursor], true, false, nil
	}
	return picked, false, false, l.edit(msg)
}

func (l *list[T]) edit(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	l.input, cmd = l.input.Update(msg)
	l.shown = l.find(strings.ToLower(strings.TrimSpace(l.input.Value())))
	l.cursor = 0
	return cmd
}

func wrap(index, count int) int {
	count = max(1, count)
	return (index%count + count) % count
}

func heading(title, styledSubtitle string, width int) string {
	return look.Sides(look.Title(title), look.Faint("esc"), width) + "\n" + styledSubtitle + "\n\n"
}

func place(modal string, width, height, margin int) (x, y int) {
	return max(margin, (width-lipgloss.Width(modal))/2), max(margin, (height-lipgloss.Height(modal))/2)
}

func compose(base, modal string, x, y int) string {
	return look.Over(look.Dim(base), modal, x, y)
}

func rowAt(modal string, x, y, stride int, labels []string) (index int, inside bool) {
	if x < 0 || y < 0 || x >= lipgloss.Width(modal) || y >= lipgloss.Height(modal) {
		return -1, false
	}
	row := y - firstRowLine
	if row < 0 || row%stride != 0 || row/stride >= len(labels) {
		return -1, true
	}
	line := ansi.Strip(strings.Split(modal, "\n")[y])
	if !pointer.TextHit(line, labels[row/stride], x) {
		return -1, true
	}
	return row / stride, true
}
