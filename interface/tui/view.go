package tui

import (
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/cover"
	"tofu/interface/tui/feed"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
	isettings "tofu/internal/settings"
	"tofu/internal/widget"
)

const (
	setupTitle  = "tofu cannot start a turn yet"
	setupKeys   = "[1-9] run the fix   [r] check again   [q] quit"
	setupIndent = "   "
	setupWatch  = "or run the command in another terminal: tofu picks it up here"
	asciiEnv    = "TOFU_ASCII"
	dumbTerm    = "dumb"
)

func tabNames() []string { return []string{"chat", "sub-agents", "file edits", "shells"} }

func (a *App) tab() screen {
	if a.current == screenSettings {
		return screenChat
	}
	return a.current
}

func (a *App) slug() string {
	if a.picked != "" {
		return a.picked
	}
	return a.provider + "/" + a.model
}

func (a *App) theme() look.Theme { return look.Theme(a.text(isettings.Theme)) }

func (a *App) View() tea.View {
	content := a.frozen
	if !a.selection.Dragged || content == "" {
		content = a.frame()
	}
	var caret *tea.Cursor
	if a.current == screenChat && len(a.dialogs) == 0 && len(a.requirements) == 0 && !a.intro.shown {
		caret = a.view.Cursor()
		if caret != nil {
			caret.Y += bodyTop
		}
	}
	content = pointer.Paint(content, a.selection, a.pane())
	if os.Getenv(asciiEnv) != "" || os.Getenv("TERM") == dumbTerm {
		content = look.ASCII(content)
	}
	a.drawn = content
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.BackgroundColor = look.Canvas(a.theme())
	view.Cursor = caret
	return view
}

func (a *App) frame() string {
	if a.intro.shown && len(a.requirements) == 0 {
		return cover.WelcomeInputView(a.intro.identity, a.width, a.height, a.intro.input.View(), -1, a.coverDetails())
	}
	base := a.base()
	if top := a.top(); top != nil {
		base = top.over(a, base)
	}
	return look.Apply(base, a.theme())
}

func (a *App) base() string {
	if a.current == screenSettings && len(a.requirements) == 0 {
		view := a.settings.View()
		if a.status.Note == "" {
			return view
		}
		rows := strings.Split(view, "\n")
		rows[len(rows)-1] = look.ChromeRow(a.width, look.PanelLight, look.Painted(" "+a.status.Note, look.Text, look.PanelLight), "")
		return strings.Join(rows, "\n")
	}
	head := frame.Head{
		Path:        a.options.Repo,
		Branch:      a.options.Branch,
		Provider:    a.provider,
		Model:       a.model,
		SessionName: a.sessionName,
		SessionID:   a.sessionID,
		At:          a.options.Now(),
		Started:     a.started,
		Fresh:       a.status.Fresh,
	}
	if a.forking {
		head.Notice = frame.ForkNotice
	}
	counts := []int{0, a.status.Agents, 0, 0}
	tabs := make([]frame.Tab, len(counts))
	for index, name := range tabNames() {
		tabs[index] = frame.Tab{Label: name, Count: counts[index]}
	}
	top, hits := frame.Top(head, tabs, int(a.tab()), a.width)
	a.hits = hits
	status := a.status
	status.At, status.Mode = head.At, frame.StatusMode(a.text(isettings.StatusBar))
	if len(a.requirements) > 0 {
		status.Mode = frame.StatusHidden
	}
	right := ""
	if a.current == screenChat {
		right = frame.ChatRight(a.slug(), string(a.shownEffort()))
	}
	framed := top + "\n" + strings.Repeat(" ", a.width) + "\n" + a.body() + "\n" + frame.Footer(status, a.width, right)
	if track, scrolls := a.track(); scrolls {
		return pointer.Overlay(framed, a.width, track)
	}
	return framed
}

func (a *App) body() string {
	rows := a.height - chromeRows
	if len(a.requirements) > 0 {
		return a.setupView(rows)
	}
	switch a.current {
	case screenChat:
		a.view.ChatShowsTools = a.flag(isettings.ChatShowsTools)
		a.view.FoldHidesShell = a.flag(isettings.FoldHidesShell)
		return a.view.View()
	case screenAgents:
		a.feed.SetDensity(a.text(isettings.Density))
		a.feed.SetAgents(a.busy, a.agents())
		return a.feed.View()
	case screenEdits:
		return a.edits.View()
	case screenShells:
		return a.shells.View()
	case screenSettings:
	}
	panic("tui: unknown screen")
}

func (a *App) agents() []feed.Agent {
	agents := make([]feed.Agent, len(a.children))
	for index, child := range a.children {
		agents[index] = feed.Agent{Name: child.Name, State: child.State, Doing: child.Doing, Since: child.Since}
	}
	return agents
}

func (a *App) setupView(rows int) string {
	lines := []string{look.Style(look.Amber).Render(widget.Fit(setupTitle, a.width)), ""}
	for index, requirement := range a.requirements {
		number := strconv.Itoa(index + 1)
		for _, line := range widget.Wrap(number+". "+requirement.What, a.width) {
			lines = append(lines, look.Style(look.Text).Render(line))
		}
		for _, line := range widget.Wrap("press "+number+" to run   "+requirement.Fix, a.width-widget.Cells(setupIndent)) {
			lines = append(lines, look.Muted(setupIndent+line))
		}
		lines = append(lines, "")
	}
	if a.options.Recheck != nil {
		lines = append(lines, look.Muted(widget.Fit(setupWatch, a.width)), "")
	}
	lines = append(lines, look.Faint(widget.Fit(setupKeys, a.width)))
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return strings.Join(lines[:rows], "\n")
}
