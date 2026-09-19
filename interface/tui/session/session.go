package session

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"boji/interface/tui/theme"
	"boji/interface/tui/widget"
)

const (
	entryWindow    = 500
	composerRows   = 3
	footerRows     = composerRows + 2
	placeholder    = "what should boji do here?"
	idleHint       = "⏎ send   ⇧⏎ or ctrl+j newline   ctrl+c quit"
	busyHint       = "working   ctrl+c stops the turn"
	continuation   = "    "
	toolMarker     = "⟩ "
	assistantMark  = "▌ "
	userMarker     = "» "
	noteMarker     = "· "
	failureMarker  = "! "
	minimumColumns = 20
)

type Kind int

const (
	User Kind = iota
	Assistant
	Tool
	Result
	Note
	Failure
)

type Entry struct {
	Kind Kind
	Head string
	Body string
}

type Model struct {
	Busy     bool
	entries  []Entry
	composer textarea.Model
	width    int
	height   int
}

func New() Model {
	composer := textarea.New()
	composer.Placeholder = placeholder
	composer.ShowLineNumbers = false
	composer.Prompt = "▏ "
	composer.SetHeight(composerRows)
	composer.CharLimit = 0
	composer.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	return Model{composer: composer}
}

func (m *Model) Focus() tea.Cmd { return m.composer.Focus() }

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumColumns), height
	m.composer.SetWidth(m.width)
	m.composer.SetHeight(composerRows)
}

func (m *Model) Append(entry Entry) {
	m.entries = append(m.entries, entry)
	if len(m.entries) > entryWindow {
		m.entries = m.entries[len(m.entries)-entryWindow:]
	}
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	composer, cmd := m.composer.Update(msg)
	m.composer = composer
	return cmd
}

func (m Model) Value() string { return strings.TrimSpace(m.composer.Value()) }

func (m *Model) Reset() { m.composer.Reset() }

func (m Model) View() string {
	transcriptRows := max(m.height-footerRows, 1)
	lines := m.tail(transcriptRows)
	for len(lines) < transcriptRows {
		lines = append(lines, "")
	}
	hint := idleHint
	if m.Busy {
		hint = busyHint
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		strings.Join(lines, "\n"),
		theme.Rule().Render(strings.Repeat("─", m.width)),
		m.composer.View(),
		theme.Faint().Render(widget.Fit(hint, m.width)),
	)
}

func (m Model) tail(rows int) []string {
	var lines []string
	for index := len(m.entries) - 1; index >= 0 && len(lines) < rows; index-- {
		lines = append(render(m.entries[index], m.width), lines...)
	}
	if len(lines) > rows {
		return lines[len(lines)-rows:]
	}
	return lines
}

func render(entry Entry, width int) []string {
	marker, style := markerOf(entry.Kind)
	body := entry.Body
	if entry.Kind == Tool {
		body = entry.Head + "  " + entry.Body
	}
	wrapped := widget.Wrap(body, max(width-len([]rune(marker)), 1))
	lines := make([]string, 0, len(wrapped))
	for index, line := range wrapped {
		prefix := marker
		if index > 0 {
			prefix = strings.Repeat(" ", len([]rune(marker)))
		}
		lines = append(lines, style.Render(prefix+line))
	}
	return lines
}

func markerOf(kind Kind) (string, lipgloss.Style) {
	switch kind {
	case User:
		return userMarker, theme.Accent()
	case Assistant:
		return assistantMark, theme.Text()
	case Tool:
		return toolMarker, theme.Tool()
	case Result:
		return continuation, theme.Faint()
	case Note:
		return noteMarker, theme.Dim()
	case Failure:
		return failureMarker, theme.Fail()
	}
	panic("session: unknown entry kind")
}
