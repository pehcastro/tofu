package session

import (
	"strings"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	commandPrefix  = "/"
	blockPrefix    = "*"
	escapePrefix   = `\`
	atSigil        = "@"
	hereSigil      = "./"
	upSigil        = "../"
	wordBreaks     = " \t\n"
	commandColumn  = 14
	menuMost       = 8
	pickedMarker   = "▸ "
	unpickedMarker = "  "
)

type Command struct {
	Name string
	What string
}

func (m Model) Command() (string, bool) {
	typed, sliced := strings.CutPrefix(m.composer.Value(), commandPrefix)
	if !sliced || strings.HasPrefix(typed, commandPrefix) || strings.HasPrefix(typed, blockPrefix) {
		return "", false
	}
	if strings.ContainsAny(typed, wordBreaks) {
		return "", false
	}
	return typed, true
}

func pathSigils() [3]string { return [3]string{upSigil, hereSigil, atSigil} }

func lastWord(value string) (string, int) {
	at := strings.LastIndexAny(value, wordBreaks) + 1
	return value[at:], at
}

func (m Model) Path() (string, string, bool) {
	word, _ := lastWord(m.composer.Value())
	for _, sigil := range pathSigils() {
		if typed, is := strings.CutPrefix(word, sigil); is {
			return sigil, typed, true
		}
	}
	return "", "", false
}

func (m Model) menuRows() ([]Command, string) {
	if m.closed {
		return nil, ""
	}
	if typed, asked := m.Command(); asked {
		return namesUnder(m.Commands, typed), commandPrefix
	}
	sigil, typed, asked := m.Path()
	if !asked {
		return nil, ""
	}
	return pathsHolding(m.Paths, typed), sigil
}

func namesUnder(known []Command, typed string) []Command {
	var shown []Command
	for _, row := range known {
		if strings.HasPrefix(row.Name, typed) {
			shown = append(shown, row)
		}
	}
	return shown
}

func pathsHolding(paths []string, typed string) []Command {
	folded := strings.ToLower(typed)
	rows := make([]Command, 0, menuMost)
	for _, path := range paths {
		if !strings.Contains(strings.ToLower(path), folded) {
			continue
		}
		if rows = append(rows, Command{Name: path}); len(rows) == menuMost {
			break
		}
	}
	return rows
}

func (m Model) pickedRow() (Command, string, bool) {
	rows, sigil := m.menuRows()
	if len(rows) == 0 {
		return Command{}, "", false
	}
	return rows[min(m.picked, len(rows)-1)], sigil, true
}

func (m Model) Picked() (string, bool) {
	row, _, open := m.pickedRow()
	return row.Name, open
}

func (m *Model) MovePick(by int) {
	rows, _ := m.menuRows()
	if len(rows) == 0 {
		return
	}
	m.picked = (min(m.picked, len(rows)-1) + by + len(rows)) % len(rows)
}

func (m *Model) CloseMenu() {
	rows, _ := m.menuRows()
	m.closed = len(rows) > 0
}

func (m *Model) replaceLastWord(text string) {
	value := m.composer.Value()
	_, at := lastWord(value)
	m.composer.SetValue(value[:at] + text)
	m.composer.CursorEnd()
}

func (m *Model) Complete() {
	rows, sigil := m.menuRows()
	if len(rows) == 0 {
		return
	}
	shared := rows[0].Name
	for _, row := range rows[1:] {
		for !strings.HasPrefix(row.Name, shared) {
			shared = shared[:len(shared)-1]
		}
	}
	m.replaceLastWord(sigil + shared)
}

func (m *Model) Accept() (string, bool) {
	row, sigil, open := m.pickedRow()
	if !open {
		return "", false
	}
	if sigil == commandPrefix {
		return row.Name, true
	}
	m.replaceLastWord(sigil + row.Name + " ")
	return "", false
}

func (m Model) commandLines() []string {
	rows, sigil := m.menuRows()
	picked := min(m.picked, len(rows)-1)
	lines := make([]string, 0, len(rows))
	for index, row := range rows {
		marker, style := unpickedMarker, theme.Dim()
		if sigil != commandPrefix {
			style = theme.Path()
		}
		if index == picked {
			marker, style = pickedMarker, theme.Accent()
		}
		name := marker + sigil + row.Name
		if row.What == "" {
			lines = append(lines, style.Render(widget.Fit(name, m.width)))
			continue
		}
		what := widget.Fit(row.What, max(m.width-commandColumn, 1))
		lines = append(lines, style.Render(widget.Pad(name, commandColumn))+theme.Faint().Render(what))
	}
	return lines
}
