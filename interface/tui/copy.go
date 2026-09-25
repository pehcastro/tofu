package tui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/internal/sys"
	"tofu/internal/widget"
)

const (
	nothingToCopy = "there is nothing there to copy yet"
	answerUnit    = "the last answer"
	callUnit      = "the tool call"
	linkUnit      = "the link"
	copyFailed    = " was not copied: "
	copyWritten   = " copied, "
	copyHanded    = " handed to the terminal, which does not say whether it took it"
	sysPrefix     = "sys: "
)

type copyState int

const (
	copyToClipboard copyState = iota
	copyToTerminal
	copyRefused
)

type copiedMsg struct {
	state copyState
	unit  string
	text  string
	cause string
}

func (m copiedMsg) note() string {
	switch m.state {
	case copyToClipboard:
		return m.unit + copyWritten + widget.Size(len(m.text))
	case copyToTerminal:
		return m.unit + copyHanded
	case copyRefused:
		return m.unit + copyFailed + strings.TrimPrefix(m.cause, sysPrefix)
	}
	panic("tui: unknown copy state")
}

func (a *App) copy(unit, text string, found bool) tea.Cmd {
	if !found {
		a.status.Note = nothingToCopy
		return nil
	}
	write := a.options.Copy
	return func() tea.Msg {
		switch err := write(text); {
		case err == nil:
			return copiedMsg{state: copyToClipboard, unit: unit, text: text}
		case errors.Is(err, sys.ErrNoLocalClipboard):
			return copiedMsg{state: copyToTerminal, unit: unit, text: text}
		default:
			return copiedMsg{state: copyRefused, unit: unit, cause: err.Error()}
		}
	}
}

func (a *App) copyAnswer() tea.Cmd {
	answer, found := a.view.LastAnswer()
	return a.copy(answerUnit, answer, found)
}
