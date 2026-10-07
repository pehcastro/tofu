package tui

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/palette"
	"tofu/interface/tui/session"
	"tofu/internal/memory"
)

const (
	rememberCommand = "/remember "
	rememberUsage   = "type /remember and what tofu should keep, like /remember never run cargo with more than 2 jobs"
	offerTitle      = "Remember this?"
)

type offerDialog struct {
	palette.Confirm
	offer memory.Offer
}

func (a *App) offerTyped(typed string) tea.Cmd {
	if offer, offered := memory.OfferFor(typed); offered {
		return a.offerMemory(offer)
	}
	return nil
}

func (a *App) offerMemory(offer memory.Offer) tea.Cmd {
	global, err := memory.GlobalDir()
	asks := true
	if err == nil {
		asks, err = memory.AsksFirst(global)
	}
	if !asks && err == nil {
		a.remember(offer, offer.Scope)
		return nil
	}
	return a.push(&offerDialog{palette.NewConfirm(offerTitle, offer.Text, strings.Join(strings.Fields(offer.Said), " "), []palette.Item{
		{Title: "For you, in every project", Description: "kept in ~/.tofu/memory", Key: "1", ID: string(memory.AnswerGlobal)},
		{Title: "For this project", Description: "kept beside this project's sessions", Key: "2", ID: string(memory.AnswerProject)},
		{Title: "No", Description: "keep nothing", Key: "3", ID: string(memory.AnswerNo)},
	}), offer})
}

func (d *offerDialog) over(a *App, base string) string { return d.Over(base, a.width, a.height) }

func (d *offerDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	keyed := map[string]memory.Answer{"1": memory.AnswerGlobal, "2": memory.AnswerProject, "3": memory.AnswerNo}
	if answer, pressed := keyed[msg.String()]; pressed {
		return d.decide(a, palette.Choice{ID: string(answer), Done: true})
	}
	return d.decide(a, d.Key(msg))
}

func (d *offerDialog) click(a *App, x, y int) tea.Cmd {
	return d.decide(a, d.Click(x, y, a.width, a.height))
}

func (d *offerDialog) decide(a *App, choice palette.Choice) tea.Cmd {
	if !choice.Done && !choice.Cancelled {
		return nil
	}
	answer := memory.Answer(cmp.Or(choice.ID, string(memory.AnswerNo)))
	switch {
	case answer == memory.AnswerNo:
		a.recordAnswer(answer)
		a.view.Append(session.Entry{Kind: session.Note, Body: "not remembered"})
	case a.remember(d.offer, memory.Scope(answer)):
		a.recordAnswer(answer)
	}
	return a.pop()
}

func (a *App) recordAnswer(answer memory.Answer) {
	global, err := memory.GlobalDir()
	if err == nil {
		err = memory.Record(global, answer)
	}
	if err != nil {
		a.view.Append(session.Entry{Kind: session.Note, Body: "your answer was not counted: " + err.Error()})
	}
}

func (a *App) remember(offer memory.Offer, scope memory.Scope) bool {
	shelves, err := memory.Open(cmp.Or(a.options.Root, "."))
	var added memory.Entry
	if err == nil {
		added, err = shelves.Add(memory.Entry{Scope: scope, Kind: offer.Kind, Text: offer.Text, Said: offer.Said, Session: a.sessionID, At: a.options.Now(), By: memory.ByOffer}, "")
	}
	if err != nil {
		hint := ""
		if errors.As(err, new(memory.FullError)) {
			hint = ". tofu memory lists what to remove"
		}
		a.view.Append(session.Entry{Kind: session.Note, Body: "not remembered: " + err.Error() + hint})
		return false
	}
	flag := ""
	if scope == memory.Global {
		flag = " --global"
	}
	a.view.Append(session.Entry{Kind: session.Note, Body: fmt.Sprintf("remembered for you · %s · %s · undo: tofu memory remove%s %s", added.ID, scope, flag, added.ID)})
	return true
}

func (a *App) rememberTyped(whole string) (tea.Cmd, bool) {
	text, typed := strings.CutPrefix(whole, rememberCommand)
	if !typed {
		return nil, false
	}
	a.view.Reset()
	offer, offered := memory.OfferFor("remember: " + text)
	if !offered {
		a.view.Append(session.Entry{Kind: session.Note, Body: rememberUsage})
		return nil, true
	}
	offer.Said = whole
	return a.offerMemory(offer), true
}

func (a *App) memoryNote() string {
	shelves, err := memory.Open(cmp.Or(a.options.Root, "."))
	if err != nil {
		return "memory could not be read: " + err.Error()
	}
	lines := []string{"remembered · tofu memory remove [--global] <id> forgets one"}
	for _, shelf := range []memory.Shelf{shelves.Global, shelves.Project} {
		lines = append(lines, fmt.Sprintf("%s · %d entries", shelf.Scope, len(shelf.Entries)))
		for _, e := range shelf.Entries {
			lines = append(lines, "  "+e.ID+"  "+e.Text)
		}
	}
	return strings.Join(lines, "\n")
}
