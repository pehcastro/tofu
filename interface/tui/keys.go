package tui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/feed"
	"tofu/interface/tui/shells"
	"tofu/internal/llm"
)

const (
	searchAction   = "Search"
	commandsAction = "Commands"
	settingsAction = "Settings"
	modelsAction   = "Models"
	quoteAction    = "Quote selection"
	altPrefix      = "alt+"
	quotePrefix    = "> "
)

func (a *App) key(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if len(a.requirements) > 0 {
		return a.setupKey(key)
	}
	if key == "ctrl+c" {
		if a.selection.Dragged {
			return a.copy(selectionUnit, a.selectedText(), true)
		}
		return a.interrupt()
	}
	a.pressedAt = time.Time{}
	if capturing, open := a.top().(*shortcutsDialog); open {
		return capturing.key(a, msg)
	}
	if cmd, taken := a.shortcut(msg.Key().Keystroke()); taken {
		return cmd
	}
	if top := a.top(); top != nil {
		return top.key(a, msg)
	}
	if a.current == screenChat {
		if cmd, taken := a.composerKey(msg); taken {
			return cmd
		}
	}
	if key == "tab" && !a.settings.Searching() {
		return a.show(screen((int(a.tab()) + 1) % len(tabNames())))
	}
	if index, jumps := tabDigit(key); jumps && !a.settings.Searching() {
		return a.show(screen(index))
	}
	if cmd, taken := a.screenKey(key); taken {
		return cmd
	}
	if key == "esc" {
		return a.show(screenChat)
	}
	return nil
}

func (a *App) shortcut(pressed string) (tea.Cmd, bool) {
	action := ""
	for name, bound := range a.shortcuts {
		if bound != "" && bound == pressed {
			action = name
		}
	}
	if action == "" {
		return nil, false
	}
	a.dialogs = nil
	switch {
	case action == searchAction && a.current == screenSettings:
		a.settingsKey(pressed)
	case action == searchAction:
		a.push(a.searchDialog())
	case action == commandsAction:
		a.push(a.commandsDialog())
	case action == settingsAction:
		return a.show(screenSettings), true
	case action == modelsAction:
		a.openPicker(false)
	case action == quoteAction:
		return a.quoteSelection(), true
	}
	return nil, true
}

func (a *App) quoteSelection() tea.Cmd {
	if a.lastSelection != "" {
		quoted := quotePrefix + strings.ReplaceAll(a.lastSelection, "\n", "\n"+quotePrefix) + "\n"
		a.lastSelection = ""
		cmd := a.show(screenChat)
		a.view.Insert(quoted)
		return cmd
	}
	selected := a.feed.Selected()
	if selected == "" {
		return nil
	}
	cmd := a.show(screenChat)
	a.view.Insert("[quote#" + selected + "] ")
	return cmd
}

func (a *App) composerKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if a.view.TakesAnswerDigits() {
		switch key {
		case "1":
			a.answer(AllowedOnce)
			return nil, true
		case "2":
			a.answer(Denied)
			return nil, true
		case "3":
			a.answer(AlwaysHere)
			return nil, true
		}
	}
	if handled, cmd := a.menuKey(key); handled {
		return cmd, true
	}
	switch key {
	case "shift+tab":
		a.cycleEffort()
		return nil, true
	case "ctrl+v", "alt+v":
		return a.view.Paste(a.board), true
	case "enter":
		return a.submit(), true
	case "ctrl+x":
		a.view.Unqueue()
		return nil, true
	case "alt+up":
		a.view.PickQueued(-1)
		return nil, true
	case "alt+down":
		a.view.PickQueued(1)
		return nil, true
	case "ctrl+y":
		return a.copyAnswer(), true
	case "alt+y":
		call, found := a.view.LastCall()
		return a.copy(callUnit, call, found), true
	case "up":
		if a.view.HistoryUp() {
			return nil, true
		}
	case "down":
		if a.view.HistoryDown() {
			return nil, true
		}
	}
	if _, scrolled := a.view.Scroll(key); scrolled {
		return nil, true
	}
	_, jumps := tabDigit(key)
	switch {
	case key == "tab", key == "esc", jumps && strings.HasPrefix(key, altPrefix):
		return nil, false
	case msg.Key().Mod&tea.ModAlt != 0:
		return nil, true
	}
	return a.view.Update(msg), true
}

func tabDigit(key string) (int, bool) {
	index, err := strconv.Atoi(strings.TrimPrefix(key, altPrefix))
	return index - 1, err == nil && index >= 1 && index <= len(tabNames())
}

func (a *App) pasted(msg tea.PasteMsg) tea.Cmd {
	switch top := a.top().(type) {
	case nil:
		if a.current == screenChat {
			return a.view.Update(msg)
		}
	case *commandsDialog:
		return top.Paste(msg)
	case *searchDialog:
		return top.Paste(msg)
	case *recordedDialog:
		return top.Paste(msg)
	}
	return nil
}

func (a *App) submit() tea.Cmd {
	if name, asked := a.view.Command(); asked {
		return a.runCommand(name)
	}
	return a.send()
}

func (a *App) cycleEffort() {
	var offered []llm.Effort
	for _, wire := range a.wires {
		if wire.Provider == a.provider {
			offered = wire.Efforts
		}
	}
	levels := slices.DeleteFunc([]llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh, llm.EffortXHigh, llm.EffortMax},
		func(level llm.Effort) bool { return !slices.Contains(offered, level) })
	if len(levels) == 0 {
		return
	}
	at := slices.Index(levels, a.shownEffort())
	a.effort = levels[(at+1)%len(levels)]
}

func (a *App) shownEffort() llm.Effort {
	if a.effort == "" {
		return llm.EffortDefault
	}
	return a.effort
}

func (a *App) screenKey(key string) (tea.Cmd, bool) {
	switch a.current {
	case screenAgents:
		if at := a.happenedAt(a.feed.Selected()); key == "enter" && at >= 0 && a.happened[at].Kind == feed.KindEdit {
			return a.push(&diffDialog{id: a.happened[at].ID}), true
		}
		return nil, a.feed.Key(key)
	case screenEdits:
		a.edits.Key(key)
		return nil, key != "esc"
	case screenShells:
		switch a.shells.Key(key) {
		case shells.IntentKillAsk:
			entry, _ := a.shells.KillRequested()
			a.push(killDialog(entry))
		case shells.IntentKillNow:
			entry, _ := a.shells.Picked()
			a.kill(entry.Name)
		case shells.IntentNone:
		}
		return nil, key != "esc"
	case screenSettings:
		if key == "shift+tab" {
			key = "tab"
		}
		return a.settingsKey(key), true
	case screenChat:
	}
	return nil, false
}

func (a *App) kill(name string) {
	if a.options.KillShell == nil {
		return
	}
	if err := a.options.KillShell(name); err != nil {
		a.notify(err.Error())
		return
	}
	a.shells.Remove(name)
}

func (a *App) setupKey(key string) tea.Cmd {
	switch key {
	case "q", "ctrl+c":
		return tea.Quit
	case "r":
		return func() tea.Msg { return a.checkedRequirements() }
	}
	index, err := strconv.Atoi(key)
	if err != nil || index < 1 || index > len(a.requirements) {
		return nil
	}
	run := a.requirements[index-1].Run
	if run == nil {
		run = a.options.Login
	}
	if run == nil {
		return nil
	}
	return tea.ExecProcess(run(), func(error) tea.Msg { return a.checkedRequirements() })
}

func (a *App) checkedRequirements() requirementsMsg {
	if a.options.Recheck == nil {
		return requirementsMsg(a.requirements)
	}
	return requirementsMsg(a.options.Recheck())
}
