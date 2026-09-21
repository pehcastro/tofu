package tui

import (
	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/session"
)

const turnRunningNote = "a turn is running. stop it with ctrl+c first"

func commands(options Options) []session.Command {
	listed := []session.Command{
		{Name: "chat", What: "the conversation and the composer"},
		{Name: "work", What: "every tool call whole, with its arguments and its output"},
		{Name: "file-edits", What: "a diff feed of every change, who made it and where"},
		{Name: "sub-agents", What: "the children, what each owns and what each is doing"},
		{Name: "shells", What: "the persistent processes an agent left running"},
		{Name: "settings", What: "the providers and the file each value came from"},
		{Name: "copy", What: "put the last answer on the clipboard"},
		{Name: "copy-call", What: "put the last tool call and its result on the clipboard"},
	}
	if options.Reload != nil {
		listed = append(listed, session.Command{Name: "reload", What: "re-read rules, skills and hooks from disk"})
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
	case "chat":
		a.show(viewChat)
	case "work":
		a.show(viewWork)
	case "file-edits":
		a.show(viewEdits)
	case "sub-agents":
		a.show(viewCrew)
	case "shells":
		a.show(viewShells)
	case "settings":
		a.show(viewSettings)
	case "copy":
		return a.copyAnswer()
	case "copy-call":
		call, found := a.view.LastCall()
		return a.copy(callUnit, call, found)
	case "reload":
		a.reload()
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

func (a *App) reload() {
	if a.options.Reload == nil {
		return
	}
	a.view.Append(session.Entry{Kind: session.Note, Body: a.options.Reload()})
}

func (a *App) carry(change func() string) {
	if a.busy {
		a.view.Append(session.Entry{Kind: session.Note, Body: turnRunningNote})
		return
	}
	a.view.Append(session.Entry{Kind: session.Note, Body: change()})
}
