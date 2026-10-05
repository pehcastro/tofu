package tui

import (
	"cmp"
	"context"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/feed"
	"tofu/interface/tui/keyfield"
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
	if a.settingUp() {
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
	case action == keymap.HistoryAction:
		return tea.Batch(a.show(screenChat), a.push(a.historyDialog())), true
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
	rows := composerRows()
	if at := slices.IndexFunc(rows, func(row keyRow) bool { return slices.Contains(row.keys, key) }); at >= 0 {
		if cmd, taken := rows[at].run(a); taken {
			return cmd, true
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

func composerRows() []keyRow {
	done := func(act func(a *App)) func(a *App) (tea.Cmd, bool) {
		return func(a *App) (tea.Cmd, bool) {
			act(a)
			return nil, true
		}
	}
	return []keyRow{
		{composerGroup, []string{"enter"}, "send, queued mid-turn", func(a *App) (tea.Cmd, bool) { return a.submit(), true }},
		{composerGroup, []string{"shift+tab"}, "cycle the effort", done((*App).cycleEffort)},
		{composerGroup, []string{"ctrl+v", "alt+v"}, "paste text or an image", func(a *App) (tea.Cmd, bool) { return a.view.Paste(a.board), true }},
		{composerGroup, []string{"@"}, "attach a file", func(a *App) (tea.Cmd, bool) {
			if !a.view.AtWordStart() {
				return nil, false
			}
			return a.push(a.filesDialog()), true
		}},
		{composerGroup, []string{"up"}, "previous prompt", func(a *App) (tea.Cmd, bool) { return nil, a.view.HistoryUp() }},
		{composerGroup, []string{"down"}, "next prompt", func(a *App) (tea.Cmd, bool) { return nil, a.view.HistoryDown() }},
		{composerGroup, []string{"ctrl+o"}, "open the last tool call", func(a *App) (tea.Cmd, bool) { return a.expand(a.view.CallBeside("", -1)), true }},
		{composerGroup, []string{"ctrl+y"}, "copy the last answer", func(a *App) (tea.Cmd, bool) { return a.copyAnswer(), true }},
		{composerGroup, []string{"alt+y"}, "copy the last tool call", func(a *App) (tea.Cmd, bool) {
			call, found := a.view.LastCall()
			return a.copy(callUnit, call, found), true
		}},
		{turnGroup, []string{"esc"}, "stop, if composer empty", func(a *App) (tea.Cmd, bool) {
			if !a.busy || a.view.Value() != "" {
				return nil, false
			}
			a.stopTurn()
			return nil, true
		}},
		{turnGroup, []string{"ctrl+x"}, "drop the queued prompt", done(func(a *App) { a.view.Unqueue() })},
		{turnGroup, []string{"alt+up"}, "pick an earlier queued", done(func(a *App) { a.view.PickQueued(-1) })},
		{turnGroup, []string{"alt+down"}, "pick a later queued", done(func(a *App) { a.view.PickQueued(1) })},
	}
}

func tabDigit(key string) (int, bool) {
	index, err := strconv.Atoi(strings.TrimPrefix(key, altPrefix))
	return index - 1, err == nil && index >= 1 && index <= len(tabNames())
}

func (a *App) pasted(msg tea.PasteMsg) tea.Cmd {
	if a.settingUp() {
		if a.entry.variable != "" && a.entry.checking == 0 {
			a.entry.field.Paste(msg.Content)
			a.entry.refusal = ""
		}
		return nil
	}
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
	if cmd, asked := a.resumeCommand(); asked {
		return cmd
	}
	if a.undoCommand() {
		return nil
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
	if a.entry.variable != "" {
		return a.entryKey(key)
	}
	switch key {
	case "q", "esc", "ctrl+c":
		return tea.Quit
	case "r":
		return func() tea.Msg { return a.checkedRequirements() }
	}
	index, err := strconv.Atoi(key)
	step := a.currentStep()
	var run func() *exec.Cmd
	switch {
	case err != nil || index < 1:
		return nil
	case len(step.Choices) == 0 && index <= len(a.requirements):
		run = a.requirements[index-1].Run
		if run == nil {
			run = a.options.Login
		}
	case index > len(step.Choices):
		return nil
	case step.Choices[index-1].Key != "":
		choice := step.Choices[index-1]
		a.entry = setupEntry{label: choice.Label, variable: choice.Key, field: keyfield.New(choice.Key)}
		a.setupNote = ""
		return tea.ClearScreen
	default:
		run = step.Choices[index-1].Run
	}
	if run == nil {
		return nil
	}
	return tea.ExecProcess(run(), func(error) tea.Msg { return a.checkedRequirements() })
}

func (a *App) currentStep() Requirement {
	for _, step := range a.requirements {
		if step.Done == "" {
			return step
		}
	}
	return Requirement{}
}

func (a *App) entryKey(key string) tea.Cmd {
	switch {
	case key == "ctrl+c":
		return tea.Quit
	case key == "esc":
		a.entry = setupEntry{}
		return tea.ClearScreen
	case a.entry.checking != 0:
	case key == "enter":
		if !a.entry.field.Enter() || a.options.SaveKey == nil {
			return nil
		}
		a.keyChecks++
		a.entry.checking, a.entry.refusal = a.keyChecks, ""
		check, save, variable, value := a.keyChecks, a.options.SaveKey, a.entry.variable, a.entry.field.Value()
		return func() tea.Msg {
			text, err := save(context.Background(), variable, value)
			return keySavedMsg{check: check, text: text, err: err}
		}
	default:
		a.entry.field.Type(key)
		a.entry.refusal = ""
	}
	return nil
}

func (a *App) keySaved(msg keySavedMsg) tea.Cmd {
	if msg.check == 0 || msg.check != a.entry.checking {
		return nil
	}
	if msg.err != nil {
		a.entry.checking, a.entry.refusal = 0, msg.err.Error()
		return nil
	}
	a.entry, a.setupNote = setupEntry{}, msg.text
	return tea.Batch(tea.ClearScreen, func() tea.Msg { return a.checkedRequirements() })
}

func (a *App) checkedRequirements() requirementsMsg {
	if a.options.Recheck == nil {
		return requirementsMsg(a.requirements)
	}
	return requirementsMsg(a.options.Recheck())
}
