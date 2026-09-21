package tui

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/pick"
	"tofu/interface/tui/session"
	"tofu/internal/widget"
)

const selectionUnit = "the selection"

var traceID = regexp.MustCompile(`#[0-9a-fA-F]+`)

func (a *App) mousePress(msg tea.MouseClickMsg) {
	if msg.Button != tea.MouseLeft {
		return
	}
	a.selection.Clear()
	a.pressed, a.holding = pick.Cell{X: msg.X, Y: msg.Y}, true
}

func (a *App) mouseDrag(msg tea.MouseMotionMsg) {
	at := pick.Cell{X: msg.X, Y: msg.Y}
	if !a.holding || msg.Button != tea.MouseLeft || at == a.pressed {
		return
	}
	if !a.selection.On() {
		a.selection.Begin(a.pressed)
	}
	a.selection.Extend(at)
}

func (a *App) mouseRelease() tea.Cmd {
	a.holding = false
	if !a.selection.On() {
		a.clicked(a.pressed)
		return nil
	}
	text := a.selection.Text(a.frame, a.width)
	if text == "" {
		a.selection.Clear()
		return nil
	}
	return a.copy(selectionUnit, text, true)
}

func (a *App) clicked(at pick.Cell) {
	if len(a.requirements) > 0 {
		return
	}
	if at.Y == stripRow {
		if index, hit := a.strip.Hit(at.X); hit {
			a.show(viewID(index))
		}
		return
	}
	if id, hit := idUnder(a.frame, at); hit {
		a.jumpToID(id)
	}
}

func (a *App) mouseWheel(msg tea.MouseWheelMsg) {
	if a.current != viewChat || len(a.requirements) > 0 {
		return
	}
	turn := ""
	switch msg.Button {
	case tea.MouseWheelUp:
		turn = session.WheelUp
	case tea.MouseWheelDown:
		turn = session.WheelDown
	default:
		return
	}
	if moved, scrolled := a.view.Scroll(turn); scrolled {
		a.selection.Shift(moved)
	}
}

func idUnder(frame []string, at pick.Cell) (string, bool) {
	if at.Y < 0 || at.Y >= len(frame) {
		return "", false
	}
	line := ansi.Strip(frame[at.Y])
	for _, span := range traceID.FindAllStringIndex(line, -1) {
		first := widget.Cells(line[:span[0]])
		if at.X >= first && at.X < first+widget.Cells(line[span[0]:span[1]]) {
			return strings.ToLower(line[span[0]+1 : span[1]]), true
		}
	}
	return "", false
}
