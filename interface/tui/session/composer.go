package session

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/internal/host"
	"tofu/internal/widget"
)

const (
	composerMinRows   = 2
	composerMaxRows   = 8
	composerPadTop    = 1
	composerPadX      = 2
	cueCells          = 2
	composerSideCells = 2*composerPadX + cueCells
	focusedCue        = "█"
	blurredCue        = "▯"
	quoteToken        = "[quote#"
	messageToken      = "[" + messageKind + "#"
	fileToken         = "[File @"
	agentToken        = "[&"
	textTokenHead     = "[Text "
	pastingTokenHead  = "[pasting "
	escape            = '\x1b'
)

func (m *Model) Cursor() *tea.Cursor {
	caret := m.composer.Cursor()
	if caret == nil || m.composer.Value() == "" {
		return nil
	}
	caret.X += composerPadX
	caret.Y += m.transcriptRows() + m.statusRows() + 1 + len(m.attached) + composerPadTop
	return caret
}

func (m *Model) caret() int {
	lines := strings.Split(m.composer.Value(), "\n")
	at := len(string([]rune(lines[m.composer.Line()])[:m.composer.Column()]))
	for _, line := range lines[:m.composer.Line()] {
		at += len(line) + 1
	}
	return at
}

func (m *Model) setDraft(head, tail string) {
	m.composer.SetValue(head + tail)
	whole := m.composer.Value()
	m.composer.SetValue(tail)
	m.composer.MoveToBegin()
	m.composer.InsertString(head)
	if m.composer.Value() != whole {
		m.composer.SetValue(whole)
	}
}

func (m *Model) transcriptRows() int {
	menu, _ := m.menuRows()
	rows := m.statusRows() + 1 + len(m.attached) + composerPadTop + m.composer.Height() + len(menu)
	return max(m.height-rows, 1)
}

func (m *Model) statusRows() int {
	rows := len(m.turnLines()) + len(m.Question) + len(m.queueLines())
	if _, open := m.openAsk(); open {
		rows += askBlockRows
	}
	if rows > 0 {
		rows++
	}
	return rows
}

func (m *Model) footer() []string {
	lines := slices.Concat(m.turnLines(), m.askLines(), m.Question, m.queueLines())
	if len(lines) > 0 {
		lines = append([]string{""}, lines...)
	}
	lines = append(lines, "")
	for _, attached := range m.attached {
		lines = append(lines, attached.Render(m.width))
	}
	lines = append(lines, m.composerLines()...)
	lines = append(lines, m.commandLines()...)
	for index, line := range lines {
		lines[index] = widget.Pad(line, m.width)
	}
	return lines
}

type composerKey struct {
	value                string
	row, column, scroll  int
	focused              bool
	width, height        int
	selectFrom, selectTo textarea.Position
	selecting            bool
}

func (m *Model) composerLines() []string {
	from, to, selecting := m.composer.Selection()
	key := composerKey{m.composer.Value(), m.composer.Line(), m.composer.Column(), m.composer.ScrollYOffset(), m.composer.Focused(), m.width, m.composer.Height(), from, to, selecting}
	if m.bar == nil || key != m.barKey {
		m.bar, m.barKey = strings.Split(m.composerBar(), "\n"), key
	}
	return m.bar
}

func (m *Model) composerBar() string {
	view := colourTokens(m.composer.View())
	if m.composer.Value() == "" {
		cue := blurredCue
		if m.composer.Focused() {
			cue = focusedCue
		}
		view = look.Style(look.Blue).Render(cue) + " " + view
	}
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.composer.Height()+composerPadTop).
		Padding(composerPadTop, composerPadX, 0, composerPadX).
		Background(lipgloss.Color(string(look.PanelLight))).
		Render(look.KeepSurfaceBackground(view, look.PanelLight))
}

func colourTokens(view string) string {
	var out strings.Builder
	for {
		open := strings.IndexByte(view, '[')
		if open < 0 {
			break
		}
		if open > 0 && view[open-1] == escape {
			out.WriteString(view[:open+1])
			view = view[open+1:]
			continue
		}
		length := strings.IndexByte(view[open:], ']')
		if length < 0 {
			break
		}
		out.WriteString(view[:open])
		out.WriteString(paintToken(view[open : open+length+1]))
		view = view[open+length+1:]
	}
	out.WriteString(view)
	return out.String()
}

func paintToken(token string) string {
	switch {
	case strings.HasPrefix(token, quoteToken):
		return look.TypedID("quote", strings.TrimSuffix(strings.TrimPrefix(token, quoteToken), "]"))
	case strings.HasPrefix(token, fileToken), strings.HasPrefix(token, agentToken):
		return look.Accent(token)
	case strings.HasPrefix(token, textTokenHead) && strings.HasSuffix(token, textTokenTail):
		return look.Style(look.Amber).Render(token)
	case strings.HasPrefix(token, host.ImageTokenHead):
		return look.Style(look.Blue).Render(token)
	case strings.HasPrefix(token, pastingTokenHead):
		return look.Faint(token)
	}
	return token
}
