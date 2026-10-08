package session

import (
	"strings"

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

func (m *Model) transcriptRows() int {
	menu, _ := m.menuRows()
	rows := m.statusRows() + 1 + len(m.attached) + composerPadTop + m.composer.Height() + len(menu)
	return max(m.height-rows, 1)
}

func (m *Model) statusRows() int {
	rows := len(m.turnLines()) + len(m.queueLines())
	if _, open := m.openAsk(); open {
		rows += askBlockRows
	}
	if rows > 0 {
		rows++
	}
	return rows
}

func (m *Model) footer() []string {
	lines := append(append(m.turnLines(), m.askLines()...), m.queueLines()...)
	if len(lines) > 0 {
		lines = append([]string{""}, lines...)
	}
	lines = append(lines, "")
	for _, attached := range m.attached {
		lines = append(lines, attached.Render(m.width))
	}
	lines = append(lines, strings.Split(m.composerBar(), "\n")...)
	lines = append(lines, m.commandLines()...)
	for index, line := range lines {
		lines[index] = widget.Pad(line, m.width)
	}
	return lines
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
