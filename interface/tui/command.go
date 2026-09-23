package tui

import (
	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/links"
	"tofu/interface/tui/quote"
	"tofu/interface/tui/session"
	isession "tofu/internal/session"
	"tofu/internal/sys"
)

const (
	turnRunningNote = "a turn is running. stop it with ctrl+c first"
	noRecordYet     = "nothing is recorded under this repository yet"
)

func commands(options Options) []session.Command {
	listed := []session.Command{
		{Name: "chat", What: "the conversation and the composer"},
		{Name: "work", What: "every tool call whole, with its arguments and its output"},
		{Name: "file-edits", What: "a diff feed of every change, who made it and where"},
		{Name: "sub-agents", What: "the children, what each owns and what each is doing"},
		{Name: "shells", What: "the persistent processes an agent left running"},
		{Name: "models", What: "every model the signed subscriptions serve, and which one the next turn runs"},
		{Name: "settings", What: "the providers and the file each value came from"},
		{Name: "links", What: "every link this conversation carried, newest first"},
		{Name: "quote", What: "cite a past turn by id, newest first"},
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
	case "models":
		a.openPicker()
	case "settings":
		a.show(viewSettings)
	case "links":
		a.links.Set(a.recordedLinks())
		a.show(viewLinks)
	case "quote":
		a.quote.Set(a.recordedTurns())
		a.show(viewQuote)
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

func (a *App) recordedTalk() (isession.Conversation, string) {
	store := isession.OpenAt(sys.StateDir(a.options.Root))
	id := a.sessionID
	if id == "" {
		head, err := store.Head()
		if err != nil {
			return isession.Conversation{}, noRecordYet
		}
		id = head.ID
	}
	talk, err := store.Conversation(id)
	if err != nil {
		return isession.Conversation{}, err.Error()
	}
	return talk, ""
}

func (a *App) recordedLinks() ([]links.Link, string) {
	talk, trouble := a.recordedTalk()
	if trouble != "" {
		return nil, trouble
	}
	return links.Collect(talk), ""
}

func (a *App) recordedTurns() ([]quote.Turn, string) {
	talk, trouble := a.recordedTalk()
	if trouble != "" {
		return nil, trouble
	}
	return quote.Collect(talk), ""
}

func (a *App) linksKey(key string) tea.Cmd {
	if key != "enter" {
		a.links.Key(key)
		return nil
	}
	one, picked := a.links.Picked()
	a.show(viewChat)
	return a.copy(linkUnit, one.URL, picked)
}

func (a *App) quoteKey(key string) {
	if key != "enter" {
		a.quote.Key(key)
		return
	}
	one, picked := a.quote.Picked()
	a.show(viewChat)
	if picked {
		a.view.Insert(quote.Ref(one.Event))
	}
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
