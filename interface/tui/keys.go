package tui

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/feed"
	"tofu/interface/tui/shells"
	"tofu/internal/keymap"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
	isettings "tofu/internal/settings"
)

const (
	searchAction   = "Search"
	commandsAction = "Commands"
	settingsAction = "Settings"
	modelsAction   = "Models"
	quoteAction    = "Quote selection"
	altPrefix      = "alt+"
	quotePrefix    = "> "
	thinkingKey    = "t"
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
	if key == "tab" && !a.settings.Typing() {
		return a.show(screen((int(a.tab()) + 1) % len(tabNames())))
	}
	if index, jumps := tabDigit(key); jumps && !a.settings.Typing() {
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
	cleared := a.clearDialogs()
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
		a.openPicker("")
	case action == quoteAction:
		return a.quoteSelection(), true
	case action == keymap.EditorAction:
		return tea.Batch(a.show(screenChat), a.openEditor()), true
	}
	return cleared, true
}

func (a *App) quoteSelection() tea.Cmd {
	if a.lastSelection != "" {
		quoted := quotePrefix + strings.ReplaceAll(a.lastSelection, "\n", "\n"+quotePrefix) + "\n"
		a.lastSelection = ""
		return tea.Batch(a.show(screenChat), a.view.InsertPaste(quoted))
	}
	selected := a.feed.Selected()
	if a.happenedAt(selected) < 0 {
		return nil
	}
	cmd := a.show(screenChat)
	a.view.Insert("[quote#" + selected + "] ")
	return cmd
}

func (a *App) composerKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if a.view.TakesAnswerDigits() {
		answers := [...]Answer{AllowedOnce, Denied, AlwaysHere}
		if a.view.AsksWhereToOverride() {
			answers = [...]Answer{AllowedOnce, AlwaysHere, Denied}
		}
		if at := slices.Index([]string{"1", "2", "3"}, key); at >= 0 {
			a.answer(answers[at])
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
	case "@":
		if a.view.AtWordStart() {
			return a.push(a.filesDialog()), true
		}
	case "enter":
		return a.submit(), true
	case "esc":
		if a.busy && a.view.Value() == "" {
			a.stopTurn()
			return nil, true
		}
	case "ctrl+x":
		a.view.Unqueue()
		return nil, true
	case "alt+up":
		a.view.PickQueued(-1)
		return nil, true
	case "alt+down":
		a.view.PickQueued(1)
		return nil, true
	case "ctrl+o":
		return a.expand(a.view.CallBeside("", -1)), true
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
		if a.current != screenChat {
			return nil
		}
		if board, dropped := a.board.Dropped(msg.Content); dropped {
			return a.view.Paste(board)
		}
		return a.view.InsertPaste(msg.Content)
	case *commandsDialog:
		return top.Paste(msg)
	case *searchDialog:
		return top.Paste(msg)
	case *recordedDialog:
		return top.Paste(msg)
	case *modelsDialog:
		top.picker.Paste(msg.Content)
	}
	return nil
}

func (a *App) submit() tea.Cmd {
	if cmd, asked := a.cronCommand(); asked {
		return cmd
	}
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
	chosen, known := a.chosenModel()
	levels := slices.DeleteFunc([]llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh, llm.EffortXHigh, llm.EffortMax},
		func(level llm.Effort) bool {
			return !slices.Contains(offered, level) || known && !slices.Contains(chosen.Efforts, level)
		})
	if len(levels) == 0 {
		return
	}
	at := slices.Index(levels, a.shownEffort())
	a.effort = levels[(at+1)%len(levels)]
}

func (a *App) chosenModel() (library.Model, bool) {
	if slug := a.slug(); a.chosen.slug != slug {
		a.chosen = resolvedModel{slug: slug}
		if loaded, err := a.options.Models(); err == nil {
			a.chosen.model, a.chosen.known = loaded.Resolve(slug)
		}
	}
	return a.chosen.model, a.chosen.known
}

func (a *App) shownEffort() llm.Effort {
	shown := cmp.Or(a.effort, llm.EffortDefault)
	if chosen, known := a.chosenModel(); known {
		return chosen.EffortTaken(shown)
	}
	return shown
}

func (a *App) screenKey(key string) (tea.Cmd, bool) {
	switch a.current {
	case screenAgents:
		if key == thinkingKey {
			a.commit(isettings.ShowThinking, onOff(!a.flag(isettings.ShowThinking)))
			a.refreshSettingsRows()
			return nil, true
		}
		selected := a.feed.Selected()
		if at := a.happenedAt(selected); key == "enter" && at >= 0 && a.happened[at].Kind == feed.KindEdit {
			a.recordReach(selected)
			return a.push(&diffDialog{id: selected}), true
		}
		taken := a.feed.Key(key)
		if taken && (key == "enter" || key == "space") {
			a.recordReach(selected)
		}
		return nil, taken
	case screenEdits:
		return nil, a.edits.Key(key)
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
