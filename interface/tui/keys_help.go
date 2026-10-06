package tui

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/palette"
	"tofu/internal/keymap"
)

const (
	anywhereGroup = "anywhere"
	composerGroup = "composer"
	turnGroup     = "turn"
	tabsGroup     = "tabs"
	dialogsGroup  = "dialogs"
	setupGroup    = "setup"
)

type keyRow struct {
	group string
	keys  []string
	what  string
	run   func(a *App) (tea.Cmd, bool)
}

func (a *App) keyRows() []keyRow {
	var rows []keyRow
	for _, action := range keymap.Actions() {
		rows = append(rows, keyRow{anywhereGroup, []string{cmp.Or(a.shortcuts[action], "unbound")}, action, nil})
	}
	tabs := "1-" + strconv.Itoa(len(tabNames()))
	rows = append(rows, []keyRow{
		{anywhereGroup, []string{"ctrl+c"}, "stop; twice to quit", nil},
		{anywhereGroup, []string{"ctrl+c"}, "copy a dragged selection", nil},
	}...)
	rows = append(rows, composerRows()...)
	return append(rows, []keyRow{
		{composerGroup, []string{"!"}, "run a shell command, read by the next prompt", nil},
		{composerGroup, []string{"pgup", "pgdown"}, "scroll the chat", nil},
		{composerGroup, []string{"home", "end"}, "top or bottom, if empty", nil},
		{turnGroup, []string{"1", "2", "3"}, "answer the gate's ask", nil},
		{tabsGroup, []string{"tab"}, "next tab", nil},
		{tabsGroup, []string{altPrefix + tabs}, "go to that tab", nil},
		{tabsGroup, []string{tabs}, "go to it, outside chat", nil},
		{tabsGroup, []string{"esc"}, "back to chat", nil},
		{"agents", []string{thinkingKey}, "show or hide thinking", nil},
		{"agents", []string{"enter", "space"}, "open a call or a diff", nil},
		{"settings", []string{"shift+tab"}, "change the scope", nil},
		{dialogsGroup, []string{"up", "down"}, "move", nil},
		{dialogsGroup, []string{"tab"}, "complete a / command", nil},
		{dialogsGroup, []string{"enter"}, "pick", nil},
		{dialogsGroup, []string{"esc"}, "close", nil},
		{setupGroup, []string{"1-9"}, "pick", nil},
		{setupGroup, []string{"r"}, "check again", nil},
		{setupGroup, []string{"esc", "q", "ctrl+c"}, "esc or q quits", nil},
	}...)
}

func (a *App) keyResults(query string) []palette.Result {
	var results []palette.Result
	for _, row := range a.keyRows() {
		label := fmt.Sprintf("%-9s%-12s %s", row.group, strings.Join(row.keys, "/"), row.what)
		if contains(label, query) {
			results = append(results, palette.Result{Label: label})
		}
	}
	return results
}

func (a *App) keysDialog() *recordedDialog {
	return &recordedDialog{
		searchDialog: searchDialog{palette.NewSearch("Keys", "every key the app answers to · type to filter", a.keyResults)},
		choose:       func(*App, palette.Result) tea.Cmd { return nil },
	}
}
