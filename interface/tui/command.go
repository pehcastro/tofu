package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/links"
	"tofu/interface/tui/palette"
	"tofu/interface/tui/quote"
	"tofu/interface/tui/session"
	"tofu/internal/keymap"
	isession "tofu/internal/session"
	"tofu/internal/sys"
)

const (
	turnRunningNote = "a turn is running. stop it with ctrl+c first"
	noRecordYet     = "nothing is recorded under this repository yet"
	undoWithCount   = "/undo "
)

func commands(options Options) []session.Command {
	listed := []session.Command{
		{Name: "chat", What: "the conversation and the composer"},
		{Name: "sub-agents", What: "every tool call and every sub-agent, in one feed"},
		{Name: "file-edits", What: "every changed line, who made it and where"},
		{Name: "shells", What: "the persistent processes an agent left running"},
		{Name: "models", What: "every model the signed subscriptions serve, and which one the next turn runs"},
		{Name: "status", What: "each subscription's quota windows and when they reset"},
		{Name: "attach", What: "reference a file in this workspace"},
		{Name: "settings", What: "appearance, keys, roles, and the file each value came from"},
		{Name: "keys", What: "every key the app answers to, and what it does"},
		{Name: "links", What: "every link this conversation carried, newest first"},
		{Name: "quote", What: "cite a past turn by id, newest first"},
		{Name: "copy", What: "put the last answer on the clipboard"},
		{Name: "copy-call", What: "put the last tool call and its result on the clipboard"},
		{Name: "memory", What: "what tofu remembers for you, global and for this project"},
		{Name: "remember", What: "keep a line for every later session: /remember <what>"},
	}
	if options.Reload != nil {
		listed = append(listed, session.Command{Name: "reload", What: "re-read settings, rules, skills, sub-agents, models, instructions and keys from disk"})
	}
	if options.Resume != nil {
		listed = append(listed, session.Command{Name: "resume", What: "pick a session of this project to carry into the next task"})
	}
	if options.NewSession != nil {
		listed = append(listed, session.Command{Name: "new", What: "start fresh, carrying nothing from the last session"})
	}
	if options.Compact != nil {
		listed = append(listed, session.Command{Name: "compact", What: "shrink the old tool results the next turn carries to an artifact handle each"})
	}
	if options.Undo != nil {
		listed = append(listed, session.Command{Name: "undo", What: "put back the files the last turn changed, or the last N with /undo N"})
	}
	if options.Host != nil {
		listed = append(listed,
			session.Command{Name: "cron", What: "the scheduled jobs: list, history, edit, pause, resume, delete"},
			session.Command{Name: "loop", What: "repeat a prompt on an interval: /loop 10m <prompt>"},
			session.Command{Name: "goal", What: `fire after each turn until a check passes: /goal <prompt> --until "<cmd>"`})
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
		return a.show(screenChat)
	case "sub-agents":
		return a.show(screenAgents)
	case "file-edits":
		return a.show(screenEdits)
	case "shells":
		return a.show(screenShells)
	case "models":
		a.openPicker("")
	case "status":
		a.push(quotaDialog{})
	case "attach":
		return a.push(a.filesDialog())
	case "settings":
		return a.show(screenSettings)
	case "keys":
		a.push(a.keysDialog())
	case "links":
		a.push(a.linksDialog())
	case "quote":
		a.push(a.quoteDialog())
	case "copy":
		return a.copyAnswer()
	case "copy-call":
		call, found := a.view.LastCall()
		return a.copy(callUnit, call, found)
	case "memory":
		return a.push(a.memoryDialog())
	case "remember":
		a.view.Append(session.Entry{Kind: session.Note, Body: rememberUsage})
	case "reload":
		a.reload()
	case "resume":
		return a.resumePicker()
	case "new":
		a.carry(a.options.NewSession)
	case "compact":
		a.carry(a.options.Compact)
	case "undo":
		a.carry(func() string { return a.options.Undo("") })
	case "cron", "loop", "goal":
		return a.cronLine("/" + name)
	case "quit":
		return tea.Quit
	default:
		a.view.Append(session.Entry{Kind: session.Note, Body: "there is no /" + name + ". type / to see the commands."})
	}
	return nil
}

func (a *App) recordedTalk() (isession.Conversation, string) {
	state, err := sys.ProjectStateDirAt(a.options.Root)
	if err != nil {
		return isession.Conversation{}, err.Error()
	}
	store := isession.OpenAt(state)
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

type recordedDialog struct {
	searchDialog
	choose func(a *App, result palette.Result) tea.Cmd
}

func (d *recordedDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	choice, cmd := d.Key(msg)
	return tea.Batch(cmd, d.chose(a, choice))
}

func (d *recordedDialog) click(a *App, x, y int) tea.Cmd {
	return d.chose(a, d.Click(x, y, a.width, a.height))
}

func (d *recordedDialog) chose(a *App, choice palette.SearchChoice) tea.Cmd {
	switch {
	case choice.Cancelled:
		return a.pop()
	case choice.Done:
		return tea.Batch(a.clearDialogs(), d.choose(a, choice.Result))
	}
	return nil
}

func (a *App) linksDialog() *recordedDialog {
	talk, trouble := a.recordedTalk()
	found := links.Collect(talk)
	return &recordedDialog{
		searchDialog: searchDialog{palette.NewSearch("Links", troubleOr(trouble, "every link this conversation carried · enter copies"), func(query string) []palette.Result {
			var results []palette.Result
			for _, one := range found {
				if contains(one.URL+" "+one.From, query) {
					results = append(results, palette.Result{Label: one.URL, Detail: one.From})
				}
			}
			return results
		})},
		choose: func(a *App, result palette.Result) tea.Cmd { return a.copy(linkUnit, result.Label, true) },
	}
}

func (a *App) quoteDialog() *recordedDialog {
	talk, trouble := a.recordedTalk()
	turns := quote.Collect(talk)
	return &recordedDialog{
		searchDialog: searchDialog{palette.NewSearch("Quote", troubleOr(trouble, "cite a past turn by id · enter writes the reference"), func(query string) []palette.Result {
			var results []palette.Result
			for _, one := range turns {
				label := short(one.Event) + "  " + one.From + "  " + one.Text
				if contains(label, query) {
					results = append(results, palette.Result{Label: label, Reference: quote.Ref(one.Event)})
				}
			}
			return results
		})},
		choose: func(a *App, result palette.Result) tea.Cmd {
			cmd := a.show(screenChat)
			a.view.Insert(result.Reference)
			return cmd
		},
	}
}

func troubleOr(trouble, hint string) string {
	if trouble != "" {
		return trouble
	}
	return hint
}

func (a *App) reload() {
	var said []string
	if a.options.Reload != nil {
		said = append(said, a.options.Reload())
	}
	if a.store != nil {
		if err := a.store.Reread(); err != nil {
			said = append(said, "the settings file could not be read again: "+err.Error())
		}
		a.refreshSettingsRows()
	}
	a.shortcuts = keymap.LoadShortcuts(a.options.Keymap)
	a.settings.SetSearchKey(a.shortcuts[searchAction])
	picked := a.picked
	a.readWires()
	a.picked, a.chosen = "", resolvedModel{}
	if refused := a.repick(picked); refused != "" {
		said = append(said, refused)
	}
	a.view.Append(session.Entry{Kind: session.Note, Body: strings.Join(said, "\n")})
}

func (a *App) repick(slug string) string {
	if slug == "" {
		return ""
	}
	loaded, _ := a.options.Models()
	if _, err := loaded.Select(slug); err != nil {
		return "the model picked for the next turn, " + slug + ", is now refused, so the next turn runs " + a.slug() + ": " + err.Error()
	}
	source, model, _ := strings.Cut(slug, "/")
	at := slices.IndexFunc(a.wires, func(wire Wire) bool { return wire.Provider == source })
	if at < 0 {
		return "the model picked for the next turn, " + slug + ", has no signed-in subscription now, so the next turn runs " + a.slug()
	}
	a.wire, a.provider, a.model, a.picked = a.wires[at].Name, source, model, slug
	return ""
}

func (a *App) carry(change func() string) {
	if !a.refusedMidTurn() {
		a.view.Append(session.Entry{Kind: session.Note, Body: change()})
	}
}

func (a *App) undoCommand() bool {
	count, asked := strings.CutPrefix(a.view.Draft(), undoWithCount)
	if !asked || a.options.Undo == nil {
		return false
	}
	a.view.Reset()
	a.carry(func() string { return a.options.Undo(strings.TrimSpace(count)) })
	return true
}

func (a *App) refusedMidTurn() bool {
	if a.busy {
		a.view.Append(session.Entry{Kind: session.Note, Body: turnRunningNote})
	}
	return a.busy
}
