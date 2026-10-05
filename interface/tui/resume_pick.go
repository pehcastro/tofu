package tui

import (
	"cmp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/palette"
	"tofu/interface/tui/session"
)

const (
	resumeHint = "newest first · the marked one is in use · enter resumes"
	inUseMark  = "● "
	otherMark  = "  "

	resumeWithHandle = "/resume "
)

type SessionRow struct {
	ID, Name, Task, Facts string
	InUse                 bool
}

func (a *App) resumePicker() tea.Cmd {
	if a.refusedMidTurn() {
		return nil
	}
	found, err := a.options.Sessions()
	trouble := ""
	if err != nil {
		trouble = err.Error()
	}
	return a.push(&recordedDialog{
		searchDialog: searchDialog{palette.NewSearch("Resume", troubleOr(trouble, resumeHint), func(query string) []palette.Result {
			var results []palette.Result
			for _, one := range found {
				if !contains(one.Name+" "+one.Task, query) {
					continue
				}
				mark := otherMark
				if one.InUse {
					mark = inUseMark
				}
				results = append(results, palette.Result{Label: mark + cmp.Or(one.Name, short(one.ID)) + "  " + one.Facts + "  " + one.Task, Reference: one.ID})
			}
			return results
		})},
		choose: func(a *App, result palette.Result) tea.Cmd {
			a.resumeTo(result.Reference)
			return nil
		},
	})
}

func (a *App) resumeCommand() (tea.Cmd, bool) {
	handle, asked := strings.CutPrefix(a.view.Draft(), resumeWithHandle)
	if !asked || a.options.Resume == nil {
		return nil, false
	}
	a.view.Reset()
	if handle = strings.TrimSpace(handle); handle == "" {
		return a.resumePicker(), true
	}
	a.resumeTo(handle)
	return nil, true
}

func (a *App) resumeTo(handle string) {
	if a.refusedMidTurn() {
		return
	}
	note, chat := a.options.Resume(handle)
	for _, event := range chat {
		a.absorb(event)
	}
	a.view.Stop()
	a.view.Append(session.Entry{Kind: session.Note, Body: note})
}
