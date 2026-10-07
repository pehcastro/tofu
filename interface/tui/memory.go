package tui

import (
	"cmp"
	"errors"
	"regexp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/palette"
	"tofu/interface/tui/session"
	"tofu/internal/memory"
	isettings "tofu/internal/settings"
)

const (
	rememberCommand   = "/remember "
	memoryEditCommand = "/memory edit "
	memoryRowHead     = "memory"
	rememberUsage     = "type /remember and what tofu should keep, like /remember never run cargo with more than 2 jobs"
	offerYes          = "yes"
	offerNo           = "no"
	offerAlways       = "always"
	entryRemove       = "remove"
	entryEdit         = "edit"
	entryKeep         = "keep"
)

type offerDialog struct {
	palette.Confirm
	rule      string
	scope     memory.Scope
	leadAskID string
}

func memoryCard(rule string, scope memory.Scope, leadAskID string) *offerDialog {
	where, other := "in every project", memory.Project
	if scope == memory.Project {
		where, other = "for this project", memory.Global
	}
	return &offerDialog{palette.NewConfirm("[&"+orchestrator+"] wants to add a "+string(scope)+" memory", "tab: a "+string(other)+" memory instead", rule, []palette.Item{
		{Title: "Yes", Description: "keep it " + where, Key: "1", ID: offerYes},
		{Title: "No", Description: "keep nothing", Key: "2", ID: offerNo},
		{Title: "Always", Description: "keep it, and keep every later one without asking: turns on auto memory", Key: "3", ID: offerAlways},
	}), rule, scope, leadAskID}
}

func (a *App) showLeadMemoryAsk() {
	id, decision, asks := a.view.AsksToRemember()
	alreadyShown, before := false, len(a.dialogs)
	a.dialogs = slices.DeleteFunc(a.dialogs, func(d dialog) bool {
		card, isCard := d.(*offerDialog)
		alreadyShown = alreadyShown || isCard && id != "" && card.leadAskID == id
		return isCard && card.leadAskID != id
	})
	if before > 0 && len(a.dialogs) == 0 {
		a.view.Focus()
	}
	if asks && !alreadyShown {
		a.push(memoryCard(decision.Remembers, decision.MemoryScope, id))
	}
}

func (d *offerDialog) over(a *App, base string) string { return d.Over(base, a.width, a.height) }

func (d *offerDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	keyed := map[string]string{"1": offerYes, "2": offerNo, "3": offerAlways}
	if msg.String() == "tab" {
		if d.scope == memory.Global {
			d.scope = memory.Project
		} else {
			d.scope = memory.Global
		}
		d.Confirm = memoryCard(d.rule, d.scope, d.leadAskID).Confirm
		return nil
	}
	if answer, pressed := keyed[msg.String()]; pressed {
		return d.decide(a, palette.Choice{ID: answer, Done: true})
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
	answer := Denied
	if choice.ID == offerYes || choice.ID == offerAlways {
		answer = AllowedOnce
		if d.scope == memory.Global {
			answer = AlwaysHere
		}
	}
	if choice.ID == offerAlways && a.store != nil {
		note := "auto memory is on, as you answered always. tofu settings set autoMemory false asks first again"
		if err := a.store.Set(isettings.Global, isettings.AutoMemory, 1); err != nil {
			note = "auto memory was not turned on: " + err.Error()
		}
		a.refreshSettingsRows()
		a.view.Append(session.Entry{Kind: session.Note, Body: note})
	}
	if a.options.Host != nil && a.options.Host.Answer(d.leadAskID, answer) {
		a.view.Resume(d.leadAskID)
	}
	return a.pop()
}

func (a *App) rememberedByTheLead(output string) {
	saved := regexp.MustCompile(`^\[memory#(m\d+)\] (saved to memory, [^:]+: .*?)\. The words it came from`).FindStringSubmatch(output)
	if saved != nil {
		a.view.Append(session.Entry{Kind: session.Note, Head: memoryRowHead, ID: saved[1], Body: saved[2]})
	}
}

func (a *App) rememberTyped(whole string) bool {
	if text, edit := strings.CutPrefix(whole, memoryEditCommand); edit {
		a.view.Reset()
		a.editEntry(text)
		return true
	}
	text, typed := strings.CutPrefix(whole, rememberCommand)
	if !typed {
		return false
	}
	a.view.Reset()
	rule := strings.Join(strings.Fields(text), " ")
	if rule == "" {
		a.view.Append(session.Entry{Kind: session.Note, Body: rememberUsage})
		return true
	}
	shelves, err := memory.Open(cmp.Or(a.options.Root, "."))
	var added memory.Entry
	if err == nil {
		added, err = shelves.Add(memory.Entry{Scope: memory.Global, Kind: memory.KindPerson, Text: rule, Said: whole, Session: a.sessionRoot, At: a.options.Now(), By: memory.ByPerson}, "")
	}
	if err != nil {
		hint := ""
		if errors.As(err, new(memory.FullError)) {
			hint = ". tofu memory lists what to remove"
		}
		a.view.Append(session.Entry{Kind: session.Note, Body: "not remembered: " + err.Error() + hint})
		return true
	}
	a.view.Append(session.Entry{Kind: session.Note, Head: memoryRowHead, ID: added.ID, Body: "remembered for you · " + string(added.Scope) + " · " + added.Text + " · undo: " + added.Undo()})
	if a.options.Host != nil {
		a.options.Host.Remembered(added.Saved())
	}
	return true
}

func (a *App) editEntry(typed string) {
	fields := strings.SplitN(typed, " ", 3)
	if len(fields) < 3 {
		a.view.Append(session.Entry{Kind: session.Note, Body: "type /memory edit <global|project> <id> <the new text>"})
		return
	}
	shelves, err := memory.Open(cmp.Or(a.options.Root, "."))
	scope := memory.Scope(fields[0])
	if err == nil && scope != memory.Global && scope != memory.Project {
		err = errors.New("the scope is global or project, not " + fields[0])
	}
	var old memory.Entry
	if err == nil {
		old, err = shelves.Find(scope, fields[1])
	}
	var changed memory.Entry
	if err == nil {
		old.Text = strings.TrimSpace(fields[2])
		changed, err = shelves.Add(old, old.ID)
	}
	if err != nil {
		a.view.Append(session.Entry{Kind: session.Note, Body: "not changed: " + err.Error()})
		return
	}
	a.view.Append(session.Entry{Kind: session.Note, Head: memoryRowHead, ID: changed.ID, Body: "changed · " + string(changed.Scope) + " · " + changed.Text})
}

type memoryDialog struct {
	searchDialog
	entries []memory.Entry
}

func (a *App) memoryDialog() *memoryDialog {
	shelves, err := memory.Open(cmp.Or(a.options.Root, "."))
	entries := append(slices.Clone(shelves.Global.Entries), shelves.Project.Entries...)
	hint := "every entry tofu remembers for you · enter removes or edits"
	if err != nil {
		hint = "memory could not be read: " + err.Error()
	}
	return &memoryDialog{searchDialog{palette.NewSearch("Memory", hint, func(query string) []palette.Result {
		var results []palette.Result
		for _, e := range entries {
			if contains(e.Text+" "+e.ID, query) {
				results = append(results, palette.Result{Label: e.Text, Detail: e.ID + " · " + string(e.Scope), Reference: e.ID})
			}
		}
		return results
	})}, entries}
}

func (d *memoryDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	choice, cmd := d.Key(msg)
	return tea.Batch(cmd, d.chose(a, choice))
}

func (d *memoryDialog) click(a *App, x, y int) tea.Cmd {
	return d.chose(a, d.Click(x, y, a.width, a.height))
}

func (d *memoryDialog) chose(a *App, choice palette.SearchChoice) tea.Cmd {
	switch {
	case choice.Cancelled:
		return a.pop()
	case !choice.Done:
		return nil
	}
	at := slices.IndexFunc(d.entries, func(e memory.Entry) bool { return e.ID == choice.Result.Reference })
	if at < 0 {
		return nil
	}
	e := d.entries[at]
	return a.push(&entryDialog{palette.NewConfirm(e.Ref(), e.Text, string(e.Scope)+" · "+e.At.Format("2006-01-02"), []palette.Item{
		{Title: "Remove", Description: "forget it; the undo is printed in the chat", Key: "x", ID: entryRemove},
		{Title: "Edit", Description: "put it in the composer to change and send", Key: "e", ID: entryEdit},
		{Title: "Keep", Description: "leave it as it is", Key: "esc", ID: entryKeep},
	}), e})
}

type entryDialog struct {
	palette.Confirm
	entry memory.Entry
}

func (d *entryDialog) over(a *App, base string) string { return d.Over(base, a.width, a.height) }

func (d *entryDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "x":
		return d.decide(a, palette.Choice{ID: entryRemove, Done: true})
	case "e":
		return d.decide(a, palette.Choice{ID: entryEdit, Done: true})
	}
	return d.decide(a, d.Key(msg))
}

func (d *entryDialog) click(a *App, x, y int) tea.Cmd {
	return d.decide(a, d.Click(x, y, a.width, a.height))
}

func (d *entryDialog) decide(a *App, choice palette.Choice) tea.Cmd {
	switch {
	case choice.Cancelled || choice.ID == entryKeep:
		return a.pop()
	case !choice.Done:
		return nil
	case choice.ID == entryEdit:
		cmd := a.clearDialogs()
		a.view.Insert(memoryEditCommand + string(d.entry.Scope) + " " + d.entry.ID + " " + d.entry.Text)
		return cmd
	}
	shelves, err := memory.Open(cmp.Or(a.options.Root, "."))
	if err == nil {
		_, err = shelves.Remove(d.entry.Scope, d.entry.ID)
	}
	body := "removed · " + d.entry.Text + " · undo: /remember " + d.entry.Text
	if err != nil {
		body = "not removed: " + err.Error()
	}
	a.view.Append(session.Entry{Kind: session.Note, Head: memoryRowHead, ID: d.entry.ID, Body: body})
	return a.clearDialogs()
}
