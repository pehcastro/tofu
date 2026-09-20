package tui

import (
	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/session"
)

const turnRunningNote = "a turn is running. stop it with ctrl+c first"

func commands(options Options) []session.Command {
	listed := []session.Command{
		{Name: "session", What: "the stream, the tool activity and the composer"},
		{Name: "crew", What: "the children, what each owns and what each is doing"},
		{Name: "settings", What: "the providers and the file each value came from"},
		{Name: "copy", What: "put the last answer on the clipboard"},
		{Name: "copy-call", What: "put the last tool call and its result on the clipboard"},
		{Name: "detail", What: "show or hide each tool call's full output"},
	}
	if options.ResumeHead != nil {
		listed = append(listed, session.Command{Name: "resume", What: "carry the last session into the next task"})
	}
	if options.NewSession != nil {
		listed = append(listed, session.Command{Name: "new", What: "start fresh, carrying nothing from the last session"})
	}
	return append(listed, session.Command{Name: "quit", What: "leave tofu"})
}

func (a *App) menuKey(key string) (bool, tea.Cmd) {
	if _, open := a.view.Picked(); !open {
		return false, nil
	}
	switch key {
	case "up":
		a.view.MovePick(-1)
	case "down":
		a.view.MovePick(1)
	case "tab":
		a.view.Complete()
	case "esc":
		a.view.CloseMenu()
	case "enter":
		if name, isCommand := a.view.Accept(); isCommand {
			return true, a.runCommand(name)
		}
	default:
		return false, nil
	}
	return true, nil
}

func (a *App) runCommand(name string) tea.Cmd {
	if name == "" {
		return nil
	}
	a.view.Reset()
	switch name {
	case "session":
		a.show(viewSession)
	case "crew":
		a.show(viewCrew)
	case "settings":
		a.show(viewSettings)
	case "copy":
		return a.copyAnswer()
	case "copy-call":
		call, found := a.view.LastCall()
		return a.copy(callUnit, call, found)
	case "detail":
		a.view.ToggleOpen()
	case "resume":
		a.carry(a.options.ResumeHead)
	case "new":
		a.carry(a.options.NewSession)
	case "quit":
		return tea.Quit
	default:
		a.view.Append(session.Entry{Kind: session.Note, Body: "there is no /" + name + ". type / to see the commands."})
	}
	return nil
}

func (a *App) carry(change func() string) {
	if a.busy {
		a.view.Append(session.Entry{Kind: session.Note, Body: turnRunningNote})
		return
	}
	a.view.Append(session.Entry{Kind: session.Note, Body: change()})
}
