package tui

import (
	"cmp"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tofu/interface/tui/feed"
	"tofu/interface/tui/hostkeys"
	"tofu/interface/tui/look"
	"tofu/interface/tui/models"
	"tofu/interface/tui/palette"
	"tofu/interface/tui/quota"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/shells"
	library "tofu/internal/llm/models"
	isettings "tofu/internal/settings"
	"tofu/internal/sys"
)

const (
	killChoice        = "kill"
	keepChoice        = "keep"
	diffPage          = 12
	callPreviewRows   = 10
	callDialogWidest  = 120
	callDialogMargin  = 8
	callDialogPadding = 4
	noNewModel        = "the models reload found no new model"
	noModelsReload    = "this session has no models reload"
)

type dialog interface {
	over(a *App, base string) string
	key(a *App, msg tea.KeyPressMsg) tea.Cmd
	click(a *App, x, y int) tea.Cmd
}

func (a *App) top() dialog {
	if len(a.dialogs) == 0 {
		return nil
	}
	return a.dialogs[len(a.dialogs)-1]
}

func (a *App) push(d dialog) tea.Cmd {
	a.dialogs = append(a.dialogs, d)
	a.view.Blur()
	if files, open := d.(*filesDialog); open {
		return files.init
	}
	return nil
}

func (a *App) pop() tea.Cmd {
	if len(a.dialogs) > 1 {
		a.dialogs = a.dialogs[:len(a.dialogs)-1]
		return nil
	}
	return a.clearDialogs()
}

type commandsDialog struct{ palette.Commands }

func (a *App) commandsDialog() *commandsDialog {
	var items []palette.Item
	for _, command := range a.view.Commands {
		items = append(items, palette.Item{Title: command.Name, Description: command.What, Key: "/" + command.Name, ID: command.Name})
	}
	return &commandsDialog{palette.NewCommands(items)}
}

func (d *commandsDialog) over(a *App, base string) string { return d.Over(base, a.width, a.height) }

func (d *commandsDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	choice, cmd := d.Key(msg)
	return tea.Batch(cmd, a.chose(choice))
}

func (d *commandsDialog) click(a *App, x, y int) tea.Cmd {
	return a.chose(d.Click(x, y, a.width, a.height))
}

func (a *App) chose(choice palette.Choice) tea.Cmd {
	switch {
	case choice.Cancelled:
		return a.pop()
	case choice.Done:
		return tea.Batch(a.clearDialogs(), a.runCommand(choice.ID))
	}
	return nil
}

type searchDialog struct{ palette.Search }

func (a *App) searchDialog() *searchDialog {
	return &searchDialog{palette.NewSearch("Search", "Screens, agents and events · enter opens", a.find)}
}

func (d *searchDialog) over(a *App, base string) string { return d.Over(base, a.width, a.height) }

func (d *searchDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	choice, cmd := d.Key(msg)
	return tea.Batch(cmd, a.found(choice))
}

func (d *searchDialog) click(a *App, x, y int) tea.Cmd {
	return a.found(d.Click(x, y, a.width, a.height))
}

func (a *App) found(choice palette.SearchChoice) tea.Cmd {
	switch {
	case choice.Cancelled:
		return a.pop()
	case !choice.Done:
		return nil
	case choice.Result.Reference != "":
		return tea.Batch(a.clearDialogs(), a.follow(choice.Result.Reference))
	}
	cmd := a.show(screen(choice.Result.Screen))
	for _, row := range a.settings.Rows {
		if settingLabel(row) == choice.Result.Label {
			a.settings.Jump(row.Key)
		}
	}
	return cmd
}

func contains(text, query string) bool { return strings.Contains(strings.ToLower(text), query) }

func settingLabel(row settings.Row) string { return row.Category + " / " + row.Label }

func (a *App) find(query string) []palette.Result {
	var results []palette.Result
	for index, name := range append(tabNames(), "settings") {
		if contains(name, query) {
			results = append(results, palette.Result{Label: name, Detail: "screen", Screen: index})
		}
	}
	for _, subAgent := range a.subAgents {
		if contains(subAgent.Name+" "+subAgent.Doing, query) {
			results = append(results, palette.Result{Label: "&" + subAgent.Name + "  " + subAgent.Doing, Reference: "[&" + subAgent.Name + "]"})
		}
	}
	if query == "" {
		return results
	}
	for index := len(a.happened) - 1; index >= 0; index-- {
		event := a.happened[index]
		if event.Kind == feed.KindThinking && !a.flag(isettings.ShowThinking) {
			continue
		}
		label := event.Kind.String() + "  " + event.Title + "  " + event.Body
		if contains(label, query) {
			results = append(results, palette.Result{Label: label, Reference: "[" + event.Kind.String() + "#" + event.ID + "]"})
		}
	}
	for _, row := range a.settings.Rows {
		if contains(settingLabel(row)+" "+row.Description, query) {
			results = append(results, palette.Result{Label: settingLabel(row), Detail: row.Description, Screen: int(screenSettings)})
		}
	}
	return results
}

type filesDialog struct {
	palette.Files
	init tea.Cmd
}

func (a *App) filesDialog() *filesDialog {
	files, init := palette.NewFiles(a.options.Root)
	return &filesDialog{Files: files, init: init}
}

func (d *filesDialog) over(a *App, base string) string { return d.Over(base, a.width, a.height) }

func (d *filesDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd { return d.update(a, msg) }

func (d *filesDialog) click(*App, int, int) tea.Cmd { return nil }

func (d *filesDialog) update(a *App, msg tea.Msg) tea.Cmd {
	choice, cmd := d.Update(msg)
	switch {
	case choice.Cancelled:
		return a.pop()
	case choice.Done:
		focus := a.show(screenChat)
		a.view.Insert("[File @" + choice.Path + "] ")
		return focus
	case choice.Notice != "":
		a.notify(choice.Notice)
	}
	return cmd
}

func (a *App) toFiles(msg tea.Msg) (tea.Cmd, bool) {
	files, open := a.top().(*filesDialog)
	if !open {
		return nil, false
	}
	return files.update(a, msg), true
}

type confirmDialog struct {
	palette.Confirm
	shell string
}

func killDialog(entry shells.Entry) *confirmDialog {
	return &confirmDialog{palette.NewConfirm("Stop this shell?", "The process ends and its log stays.", entry.Name+" · "+entry.Command, []palette.Item{
		{Title: "Stop the process", Description: "End it now", ID: killChoice},
		{Title: "Cancel", Description: "Leave it running", ID: keepChoice},
	}), entry.Name}
}

func (d *confirmDialog) over(a *App, base string) string { return d.Over(base, a.width, a.height) }

func (d *confirmDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd { return d.decide(a, d.Key(msg)) }

func (d *confirmDialog) click(a *App, x, y int) tea.Cmd {
	return d.decide(a, d.Click(x, y, a.width, a.height))
}

func (d *confirmDialog) decide(a *App, choice palette.Choice) tea.Cmd {
	if !choice.Done && !choice.Cancelled {
		return nil
	}
	if choice.ID == killChoice {
		a.kill(d.shell)
	}
	return a.pop()
}

type modelsDialog struct{ picker models.Model }

func (d *modelsDialog) over(a *App, base string) string {
	return d.picker.Dialog(base, a.width, a.height)
}

func (d *modelsDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	return a.choseModel(d.picker.Key(msg.String()))
}

func (d *modelsDialog) click(a *App, x, y int) tea.Cmd {
	d.picker.SetSize(a.width, a.height)
	return a.choseModel(d.picker.Click(x, y))
}

func (a *App) choseModel(intent models.Intent) tea.Cmd {
	switch intent.Action {
	case models.None:
		return nil
	case models.Close:
		return a.pop()
	case models.Pick:
		a.runNextTurnOn(intent.Slug, intent.Effort)
		return a.pop()
	case models.Bind:
		if err := library.BindRole(sys.StateDir(a.options.Root), intent.Role, intent.Slug); err != nil {
			a.notify(err.Error())
			return nil
		}
		a.roles = nil
		a.refreshSettingsRows()
		a.notify(string(intent.Role) + " now runs " + intent.Slug)
		return a.pop()
	case models.Assign:
		path, err := a.assignSubAgent(intent.Agent, intent.Slug)
		if err != nil {
			a.notify(err.Error())
			return nil
		}
		a.roles = nil
		a.refreshSettingsRows()
		a.notify(a.nowRuns(intent.Agent) + ", in " + path)
		return a.pop()
	case models.Login:
		if a.options.Login == nil {
			return nil
		}
		return tea.ExecProcess(a.options.Login(), nil)
	case models.Reload:
		reload := a.options.ReloadModels
		if reload == nil {
			return func() tea.Msg { return pickerReloadedMsg(noModelsReload) }
		}
		return func() tea.Msg { return pickerReloadedMsg(cmp.Or(reload(), noNewModel)) }
	}
	panic("tui: unknown model intent")
}

type pickerReloadedMsg string

func (a *App) pickerReloaded(note string) tea.Cmd {
	a.notify(note)
	open, isPicker := a.top().(*modelsDialog)
	if !isPicker {
		return nil
	}
	loaded, err := a.options.Models()
	if err != nil {
		a.notify(err.Error())
		return a.pop()
	}
	open.picker = open.picker.Rebuild(loaded)
	return nil
}

type quotaDialog struct{}

func (quotaDialog) over(a *App, base string) string {
	return quota.Dialog(base, a.width, a.height, a.status.Quotas, a.options.Now())
}

func (quotaDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "enter", "q":
		return a.pop()
	}
	return nil
}

func (quotaDialog) click(*App, int, int) tea.Cmd { return nil }

type diffDialog struct {
	id     string
	scroll int
}

func (d *diffDialog) over(a *App, base string) string {
	a.preferEdits()
	panel := a.edits.Panel(a.width, a.height, "#"+d.id, d.scroll)
	return look.Over(look.Dim(base), panel, max(1, (a.width-lipgloss.Width(panel))/2), max(1, (a.height-lipgloss.Height(panel))/2))
}

func (d *diffDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	page := max(1, a.height-diffPage)
	switch msg.String() {
	case "esc":
		return a.pop()
	case "up", "k":
		d.scroll = max(0, d.scroll-1)
	case "down", "j":
		d.scroll++
	case "pgup":
		d.scroll = max(0, d.scroll-page)
	case "pgdown":
		d.scroll += page
	case "home":
		d.scroll = 0
	}
	return nil
}

func (d *diffDialog) click(*App, int, int) tea.Cmd { return nil }

type callDialog struct {
	id     string
	scroll int
}

func (a *App) expand(id string) tea.Cmd {
	if _, _, found := a.view.Expansion(id, a.width); !found {
		return nil
	}
	return a.push(&callDialog{id: id})
}

func (d *callDialog) expansion(a *App) (int, string, []string) {
	outer := max(1, min(callDialogWidest, a.width-callDialogMargin))
	head, body, _ := a.view.Expansion(d.id, max(1, outer-callDialogPadding))
	d.scroll = min(max(0, d.scroll), max(0, len(body)-callPreviewRows))
	return outer, head, body
}

func (d *callDialog) over(a *App, base string) string {
	outer, head, body := d.expansion(a)
	shown := body[d.scroll:min(len(body), d.scroll+callPreviewRows)]
	shown = append(shown, make([]string, callPreviewRows-len(shown))...)
	footer := look.Faint(fmt.Sprintf("lines %d-%d/%d · up/down scroll · p/n call · ", d.scroll+1, d.scroll+min(len(body), callPreviewRows), len(body))) + look.Accent("Close")
	panel := look.TintedSurface(outer, look.Panel, head+"\n\n"+strings.Join(shown, "\n")+"\n\n"+footer)
	return look.Over(look.Dim(base), panel, max(1, (a.width-lipgloss.Width(panel))/2), max(1, (a.height-lipgloss.Height(panel))/2))
}

func (d *callDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "enter", "q":
		return a.pop()
	case "up", "k":
		d.scroll--
	case "down", "j":
		d.scroll++
	case "pgup":
		d.scroll -= callPreviewRows
	case "pgdown":
		d.scroll += callPreviewRows
	case "home":
		d.scroll = 0
	case "end":
		_, _, body := d.expansion(a)
		d.scroll = len(body)
	case "p":
		d.id, d.scroll = a.view.CallBeside(d.id, -1), 0
	case "n":
		d.id, d.scroll = a.view.CallBeside(d.id, 1), 0
	}
	d.expansion(a)
	return nil
}

func (d *callDialog) click(*App, int, int) tea.Cmd { return nil }

type shortcutsDialog struct{ hostkeys.Shortcuts }

func (d *shortcutsDialog) over(a *App, base string) string { return d.Over(base, a.width, a.height) }

func (d *shortcutsDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	changed, closed := d.Key(msg)
	if changed {
		a.shortcuts = d.Bindings()
		a.settings.SetSearchKey(a.shortcuts[searchAction])
	}
	if closed {
		return a.pop()
	}
	return nil
}

func (d *shortcutsDialog) click(a *App, x, y int) tea.Cmd {
	d.Click(x, y, a.width, a.height)
	return nil
}

type hostDialog struct{ hostkeys.Host }

func (d *hostDialog) over(a *App, base string) string { return d.Over(base, a.width, a.height) }

func (d *hostDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	cmd, closed := d.Key(msg)
	if closed {
		return tea.Batch(cmd, a.pop())
	}
	return cmd
}

func (d *hostDialog) click(a *App, x, y int) tea.Cmd {
	cmd, closed := d.Click(x, y, a.width, a.height)
	if closed {
		return tea.Batch(cmd, a.pop())
	}
	return cmd
}

func (a *App) hostResult(msg tea.Msg) bool {
	for _, open := range a.dialogs {
		if host, isHost := open.(*hostDialog); isHost && host.Result(msg) {
			return true
		}
	}
	return false
}
