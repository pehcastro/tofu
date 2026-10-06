package tui

import (
	"cmp"
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/session"
)

const (
	bangPrefix       = "!"
	bangDuringTurn   = "a ! command does not run while a turn runs: send it again once the turn ends"
	bangStillRunning = "a ! command is still running: wait for it, or esc stops it"
	bangStopped      = "stopped with esc, so the next prompt does not carry it"
	bangNoOutput     = "no output"
	bangUnwired      = "no shell is wired to this app"
)

type ranMsg struct {
	output  string
	stopped bool
}

func (a *App) runBang(task string, chips []session.Chip, whole, command string) tea.Cmd {
	switch {
	case command == "":
		return nil
	case a.busy:
		a.view.Redraft(whole)
		a.view.Append(session.Entry{Kind: session.Note, Body: bangDuringTurn})
		return nil
	}
	a.rememberPrompt(whole)
	a.view.Reset()
	a.view.Append(session.Entry{Kind: session.User, Body: task, Chips: chips})
	run := a.options.RunCommand
	if run == nil {
		a.view.Append(session.Entry{Kind: session.Failure, Body: bangUnwired})
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.stopCommand = cancel
	return func() tea.Msg {
		output, stopped := run(ctx, command)
		cancel()
		return ranMsg{output: output, stopped: stopped}
	}
}

func (a *App) ran(msg ranMsg) {
	a.stopCommand = nil
	body := cmp.Or(strings.TrimRight(msg.output, "\n"), bangNoOutput)
	if msg.stopped {
		body += "\n" + bangStopped
	}
	a.view.Append(session.Entry{Kind: session.Note, Body: body})
}
