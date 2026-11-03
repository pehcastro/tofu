package tui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/links"
	"tofu/interface/tui/palette"
	"tofu/interface/tui/quote"
	"tofu/interface/tui/session"
	"tofu/internal/command"
	isession "tofu/internal/session"
	isettings "tofu/internal/settings"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/ui/keymap"
)

const (
	turnRunningNote       = "a turn is running. stop it with ctrl+c first"
	waitsForSubAgentsNote = "the orchestrator is idle and its turn stays open until its sub-agents report, so this runs when the turn ends. ctrl+c twice stops them now"
	noSessionYet          = "this window has no session yet, so there is nothing to branch or read until its first task"
	undoWithCount         = "/undo "
	compactWithFocus      = "/compact "
	branchAtMessage       = "/branch "
	noSideHere            = "this interface cannot switch sessions, so it opens no side chat. tofu session branch <session> --side makes one"
	noSideOpened          = "no side chat was opened: "
)

func commands(options Options) []session.Command {
	var listed []session.Command
	for _, one := range command.Table() {
		if offered(options, one.Needs) {
			listed = append(listed, session.Command{Name: one.Name, What: one.Summary})
		}
	}
	return listed
}

func offered(options Options, needs command.Needs) bool {
	switch needs {
	case command.Always:
		return true
	case command.Reload:
		return options.Reload != nil
	case command.Resume:
		return options.Resume != nil
	case command.Fresh:
		return options.NewSession != nil
	case command.Compact:
		return options.Compact != nil
	case command.Fork:
		return options.Fork != nil
	case command.Branch:
		return options.BranchAt != nil
	case command.Undo:
		return options.Undo != nil
	case command.Cron, command.Turn, command.DiffTurn, command.Standing:
		return options.Host != nil
	}
	panic("tui: unknown command need " + string(needs))
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
	case "orders":
		return a.show(screenOrders)
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
	case "permissions":
		a.listStanding()
	case "reload":
		a.reload()
	case "resume":
		return a.resumePicker()
	case "side":
		return a.openSide()
	case "new":
		a.carry(a.newSession)
	case "compact":
		a.carry(func() string { return a.options.Compact("") })
	case "fork":
		a.carry(a.options.Fork)
	case "branch":
		a.carry(a.branchAt(""))
	case "undo":
		a.carry(func() string { return a.options.Undo("") })
	case "cron", "loop", "goal":
		return a.cronLine("/" + name)
	case "simplify", "debug", "hunt":
		a.view.Insert("/" + name)
		return a.send()
	case "help":
		var lines []string
		for _, one := range command.Table() {
			if offered(a.options, one.Needs) {
				lines = append(lines, strings.TrimSpace("/"+one.Name+" "+one.Args)+"  "+one.Summary)
			}
		}
		a.view.Append(session.Entry{Kind: session.Note, Body: strings.Join(lines, "\n")})
	case "quit":
		return tea.Quit
	default:
		a.view.Append(session.Entry{Kind: session.Note, Body: "there is no /" + name + ". type / to see the commands."})
	}
	return nil
}

func (a *App) newSession() string {
	note := a.options.NewSession()
	if a.options.Host != nil && a.options.Host.ID() == "" {
		a.sessionName, a.sessionID, a.sessionRoot, a.started = "", "", "", a.options.Now()
	}
	return note
}

func (a *App) recordedSession() (*isession.Store, string, string) {
	if a.sessionID == "" {
		return nil, "", noSessionYet
	}
	state, err := sys.ProjectStateDirAt(a.options.Root)
	if err != nil {
		return nil, "", err.Error()
	}
	return isession.OpenAt(state), a.sessionID, ""
}

func (a *App) recordedTalk() (isession.Conversation, string) {
	store, id, trouble := a.recordedSession()
	if trouble != "" {
		return isession.Conversation{}, trouble
	}
	talk, err := store.Conversation(id)
	if err != nil {
		return isession.Conversation{}, err.Error()
	}
	return talk, ""
}

func (a *App) openSide() tea.Cmd {
	if a.options.Resume == nil {
		a.view.Append(session.Entry{Kind: session.Note, Body: noSideHere})
		return nil
	}
	if a.refusedMidTurn() {
		return nil
	}
	side, err := a.branchSide()
	if err != nil {
		a.view.Append(session.Entry{Kind: session.Note, Body: noSideOpened + err.Error()})
		return nil
	}
	opened := "a side chat beside " + side.Header.BranchedFrom.Session + " opened, with " + a.text(isettings.SideChatAccess) + " access. the session it sits beside keeps running"
	if side.NoTask {
		opened += ". " + turn.SideNoTask
	}
	a.view.Append(session.Entry{Kind: session.Note, Body: opened})
	return a.resumeTo(side.Header.ID)
}

func (a *App) branchSide() (turn.SideOpened, error) {
	store, id, trouble := a.recordedSession()
	if trouble != "" {
		return turn.SideOpened{}, errors.New(trouble)
	}
	parent, err := store.Header(id)
	if err != nil {
		return turn.SideOpened{}, err
	}
	access, err := turn.AccessOf(a.text(isettings.SideChatAccess))
	if err != nil {
		return turn.SideOpened{}, err
	}
	var chosen *isession.Chosen
	if model, known := a.chosenModel(); known {
		chosen = &isession.Chosen{Wire: a.wire, Model: model.Slug()}
	}
	return turn.BranchSide(store, parent, turn.SideBranch{Access: access, Seed: turn.SeedSummary, Chosen: chosen, Effort: a.effort}, a.options.Now())
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

const settingsUnread = "the settings file could not be read again: "

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
			said = append(said, settingsUnread+err.Error())
		}
		a.refreshSettingsRows()
	}
	a.shortcuts = keymap.LoadShortcuts(a.options.Keymap)
	a.settings.SetSearchKey(a.shortcuts[searchAction])
	picked, bound := a.picked, a.bound
	a.readWires()
	a.picked, a.chosen = "", resolvedModel{}
	if picked == bound {
		picked = a.bound
	}
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
	if !a.runOn(slug, a.effort) {
		return "the model picked for the next turn, " + slug + ", has no signed-in subscription now, so the next turn runs " + a.slug()
	}
	return ""
}

func (a *App) carry(change func() string) {
	switch {
	case a.busy && !a.leading:
		a.afterTurn = append(a.afterTurn, change)
		a.view.Append(session.Entry{Kind: session.Note, Body: waitsForSubAgentsNote})
	case !a.refusedMidTurn():
		a.view.Append(session.Entry{Kind: session.Note, Body: change()})
	}
}

func (a *App) carryHeld() {
	for _, change := range a.afterTurn {
		a.view.Append(session.Entry{Kind: session.Note, Body: change()})
	}
	a.afterTurn = nil
}

func (a *App) commandWithArgument() bool {
	draft := a.view.Draft()
	if which, asked := strings.CutPrefix(draft, permissionsRevoke); asked && a.options.Host != nil {
		a.view.Reset()
		a.revokeStanding(strings.TrimSpace(which))
		return true
	}
	var change func() string
	if count, asked := strings.CutPrefix(draft, undoWithCount); asked && a.options.Undo != nil {
		change = func() string { return a.options.Undo(strings.TrimSpace(count)) }
	}
	if focus, asked := strings.CutPrefix(draft, compactWithFocus); asked && a.options.Compact != nil {
		change = func() string { return a.options.Compact(strings.TrimSpace(focus)) }
	}
	if message, asked := strings.CutPrefix(draft, branchAtMessage); asked && a.options.BranchAt != nil {
		change = a.branchAt(strings.TrimSpace(message))
	}
	if change == nil {
		return false
	}
	a.view.Reset()
	a.carry(change)
	return true
}

func (a *App) branchAt(message string) func() string {
	return func() string {
		note, words := a.options.BranchAt(message)
		if words != "" {
			a.view.Redraft(words)
		}
		return note
	}
}

func (a *App) refusedMidTurn() bool {
	if !a.busy {
		return false
	}
	note := turnRunningNote
	if a.options.Host != nil {
		if turn, running := a.options.Host.Turn(); running {
			note += " (" + turn + ")"
		}
	}
	a.view.Append(session.Entry{Kind: session.Note, Body: note})
	return true
}
