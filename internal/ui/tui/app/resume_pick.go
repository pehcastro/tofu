package tui

import (
	"cmp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/internal/host"
	"tofu/internal/ui/tui/chat/session"
	"tofu/internal/ui/tui/panels/edits"
	"tofu/internal/ui/tui/pickers/palette"
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
			return a.resumeTo(result.Reference)
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
	return a.resumeTo(handle), true
}

func (a *App) resumeTo(handle string) tea.Cmd {
	if a.refusedMidTurn() {
		return nil
	}
	note, opened := a.options.Resume(handle)
	a.view.MarkEarlier()
	for _, event := range opened.Chat {
		a.absorb(event)
	}
	a.view.Stop()
	a.view.Append(session.Entry{Kind: session.Note, Body: note})
	a.earlier = opened.Earlier
	return a.loadEarlier()
}

type earlierMsg struct {
	from   *host.Earlier
	events []Event
	more   bool
}

func (a *App) loadEarlier() tea.Cmd {
	earlier := a.earlier
	if earlier == nil {
		return nil
	}
	return func() tea.Msg {
		events, more := earlier.Next()
		return earlierMsg{from: earlier, events: events, more: more}
	}
}

func (a *App) drawEarlier(msg earlierMsg) tea.Cmd {
	if msg.from != a.earlier {
		return nil
	}
	older, calls := a.view.Older(), map[string]Event{}
	for _, event := range msg.events {
		status := event.Text
		switch call := calls[event.ID]; {
		case event.Agent != "", event.Kind == EventNote && event.Origin.Kind == host.OriginCron:
			continue
		case event.Kind == EventToolCall:
			calls[event.ID] = event
		case event.Kind == EventToolResult && call.Promote:
			continue
		case event.Kind == EventToolResult:
			if edit, changed := edits.Changed(event.Agent, call.Text, event.Diff, event.Created, event.ID, event.At); changed {
				status = edit.Tally()
			}
		}
		drawChat(older, event, status)
	}
	a.view.Prepend(older)
	if !msg.more {
		a.earlier = nil
		return nil
	}
	return a.loadEarlier()
}
