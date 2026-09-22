package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/session"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const (
	nothingToCopy = "there is nothing there to copy yet"
	answerUnit    = "the last answer"
	callUnit      = "the tool call"
	linkUnit      = "the link"
	copyFailed    = "the copy did not happen: "
	copyWritten   = " copied, "
	copyHanded    = " handed to the terminal, which does not say whether it took it"
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

func (m copiedMsg) entry() session.Entry {
	switch m.state {
	case copyToClipboard:
		return session.Entry{Kind: session.Note, Body: m.unit + copyWritten + widget.Size(len(m.text))}
	case copyToTerminal:
		return session.Entry{Kind: session.Note, Body: m.unit + copyHanded}
	case copyRefused:
		return session.Entry{Kind: session.Failure, Body: copyFailed + m.cause}
	}
	panic("tui: unknown copy state")
}

func (a *App) copy(unit, text string, found bool) tea.Cmd {
	if !found {
		a.view.Append(session.Entry{Kind: session.Note, Body: nothingToCopy})
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
