package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/feed"
	"tofu/interface/tui/pointer"
	"tofu/internal/turn/tools"
)

func (a *App) follow(ref string) tea.Cmd {
	if ref == "" {
		return nil
	}
	if name, instance, isAgent := agentOf(ref); isAgent {
		if !a.feed.SelectAgent(name, instance) {
			return nil
		}
		return a.show(screenAgents)
	}
	kind, id := pointer.SplitReference(ref)
	id = strings.TrimPrefix(id, "#")
	switch kind {
	case "message", "request", "session":
		return a.show(screenChat)
	}
	at := a.happenedAt(id)
	if at < 0 || !kindMatches(kind, a.happened[at].Kind) {
		return nil
	}
	if a.happened[at].Kind == feed.KindEdit && kind == feed.KindEdit.String() {
		cmd := a.show(screenEdits)
		a.edits.Open(id)
		return cmd
	}
	cmd := a.show(screenAgents)
	a.feed.Focus(id)
	return cmd
}

func kindMatches(kind string, event feed.Kind) bool {
	return kind == "" || kind == event.String() || kind == feed.KindTool.String() && event == feed.KindEdit
}

func agentOf(ref string) (name string, instance int, isAgent bool) {
	inner, isAgent := strings.CutPrefix(strings.TrimSuffix(ref, "]"), "[&")
	if !isAgent {
		return "", 0, false
	}
	name, number, _ := strings.Cut(inner, " ")
	instance, _ = strconv.Atoi(strings.Trim(number, "{}"))
	return name, instance, true
}

func (a *App) insertReference(ref string, focusChat bool) bool {
	if ref == "" || pointer.Reference.FindString(ref) != ref {
		return false
	}
	token := ref
	if _, _, isAgent := agentOf(ref); !isAgent {
		_, id := pointer.SplitReference(ref)
		if id == "" {
			return false
		}
		token = tools.QuoteRef(strings.TrimPrefix(id, "#"))
	}
	if focusChat {
		a.current, a.dialogs = screenChat, nil
	}
	a.view.Insert(token + " ")
	return true
}
