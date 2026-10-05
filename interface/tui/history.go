package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/palette"
)

const (
	historyHint      = "every prompt you sent · enter puts it in the composer"
	emptyHistoryHint = "no prompt sent yet · esc closes"
)

func (a *App) rememberPrompt(prompt string) {
	if a.prompts == nil {
		return
	}
	if err := a.prompts.Add(prompt); err != nil {
		a.notify("the prompt history was not written: " + err.Error())
	}
}

func (a *App) historyDialog() *recordedDialog {
	var prompts []string
	if a.prompts != nil {
		prompts = a.prompts.Prompts()
	}
	hint := historyHint
	if len(prompts) == 0 {
		hint = emptyHistoryHint
	}
	return &recordedDialog{
		searchDialog: searchDialog{palette.NewSearch("Prompt history", hint, func(query string) []palette.Result {
			var results []palette.Result
			for _, prompt := range prompts {
				if contains(prompt, query) {
					results = append(results, palette.Result{Label: strings.Join(strings.Fields(prompt), " "), Reference: prompt})
				}
			}
			return results
		})},
		choose: func(a *App, result palette.Result) tea.Cmd {
			cmd := a.show(screenChat)
			a.view.Reset()
			return tea.Batch(cmd, a.view.InsertPaste(result.Reference))
		},
	}
}
