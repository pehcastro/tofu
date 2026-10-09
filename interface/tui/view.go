package tui

import (
	"cmp"
	"os"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/edits"
	"tofu/interface/tui/feed"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
	"tofu/interface/tui/shells"
	"tofu/internal/konst"
	isettings "tofu/internal/settings"
	isubagent "tofu/internal/subagent"
	"tofu/internal/widget"
)

const (
	setupTitle     = "set up tofu: two steps before the first turn"
	setupKeys      = "[1-9] pick   [r] check again   [esc] quit"
	setupEntryKeys = "[enter] check and store   [esc] back"
	setupChecking  = "checking the key with its provider, esc goes back"
	setupIndent    = "   "
	setupDone      = "✓ "
	setupNow       = "› "
	setupLater     = "○ "
	setupGap       = "   "
	asciiEnv       = "TOFU_ASCII"
	dumbTerm       = "dumb"
)

const (
	upToDate          = "up to date · "
	welcomeNoticeRows = 2
	shimmerCells      = 4
	shimmerStep       = 2
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
	if a.current == screenChat && len(a.dialogs) == 0 && !a.settingUp() {
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
	view.ReportFocus = true
	view.BackgroundColor = look.Canvas(a.theme())
	view.Cursor = caret
	return view
}

func (a *App) frame() string {
	base := a.base()
	if top := a.top(); top != nil {
		base = top.over(a, base)
	}
	return look.Apply(base, a.theme())
}

func (a *App) base() string {
	if a.current == screenSettings && !a.settingUp() {
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
	counts := []int{0, a.status.Agents, 0, a.runningShells()}
	tabs := make([]frame.Tab, len(counts))
	for index, name := range tabNames() {
		tabs[index] = frame.Tab{Label: name, Count: counts[index]}
	}
	top, hits := frame.Top(head, tabs, int(a.tab()), a.width)
	a.hits = hits
	status := a.status
	status.At, status.Mode, status.InUse = head.At, frame.StatusMode(a.text(isettings.StatusBar)), a.sourcesInUse()
	if a.settingUp() {
		status.Mode = frame.StatusHidden
	}
	right := ""
	if a.current == screenChat {
		right = frame.ChatRight(a.slug(), string(a.shownEffort()))
	}
	framed := top + "\n" + strings.Repeat(" ", a.width) + "\n" + a.body() + "\n" + frame.Footer(status, a.width, right)
	if track, scrolls := a.track(); scrolls && a.current != screenChat {
		return pointer.Overlay(framed, a.width, track)
	}
	return framed
}

func (a *App) runningShells() int {
	running := 0
	for _, entry := range a.shells.Entries {
		if entry.State == shells.Running {
			running++
		}
	}
	return running
}

func (a *App) sourcesInUse() []string {
	sources := []string{a.provider}
	for _, subAgent := range a.subAgents {
		if source, _, _ := strings.Cut(subAgent.Model, "/"); subAgent.State == isubagent.Working && source != "" && !slices.Contains(sources, source) {
			sources = append(sources, source)
		}
	}
	return sources
}

func (a *App) body() string {
	rows := a.height - chromeRows
	if a.settingUp() {
		return a.setupView(rows)
	}
	switch a.current {
	case screenChat:
		a.view.ChatShowsTools = a.flag(isettings.ChatShowsTools)
		a.view.FoldHidesShell = a.flag(isettings.FoldHidesShell)
		a.view.Welcome = nil
		a.view.Notice, _ = a.shimmer()
		a.view.Question = nil
		if len(a.questions) > 0 {
			a.view.Question = a.questions[0].Lines(a.width, a.options.Now())
		}
		if a.intro.shown {
			a.view.Welcome, a.view.Notice = a.welcome, ""
		}
		return a.view.View()
	case screenAgents:
		return a.feed.View()
	case screenEdits:
		a.preferEdits()
		return a.edits.View()
	case screenShells:
		return a.shells.View()
	case screenSettings:
	}
	panic("tui: unknown screen")
}

func (a *App) welcome(width, rows int) []string {
	noticeRows := 0
	if a.updateChecked {
		noticeRows = welcomeNoticeRows
	}
	art := a.intro.identity.Fit(width, rows-noticeRows)
	last := len(art) - 1
	for last > 0 && strings.TrimSpace(ansi.Strip(art[last])) == "" {
		last--
	}
	if a.top() == nil {
		for index, row := range art {
			art[index] = look.Unthemed(row)
		}
	}
	if noticeRows == 0 {
		return art
	}
	art = append(art, slices.Repeat([]string{strings.Repeat(" ", width)}, noticeRows)...)
	said := cmp.Or(string(a.updateSaid), upToDate+konst.Version)
	art[last+noticeRows] = lipgloss.PlaceHorizontal(width, lipgloss.Center, look.Muted(widget.Fit(said, width)))
	return art
}

func (a *App) shimmer() (string, bool) {
	notice := []rune(string(a.updateSaid))
	if len(notice) == 0 {
		return "", false
	}
	glint := (a.pulse - a.updateAt) * shimmerStep
	if glint >= len(notice)+shimmerCells || a.text(isettings.Animations) == animationsOff {
		return look.Muted(string(notice)), false
	}
	from, to := max(glint-shimmerCells, 0), min(glint, len(notice))
	return look.Muted(string(notice[:from])) + look.Style(look.Text).Render(string(notice[from:to])) + look.Muted(string(notice[to:])), true
}

func (a *App) preferEdits() {
	contextLines, _ := strconv.Atoi(a.text(isettings.DiffContext))
	a.edits.SetPreferences(edits.Preferences{
		ContextLines: contextLines,
		PlainPaths:   a.text(isettings.Hyperlinks) == isettings.LinksOff,
		NoAuthors:    !a.flag(isettings.GroupByAgent),
	})
}

func (a *App) retention() feed.Retention {
	kept := a.text(isettings.AgentFeeds)
	switch kept {
	case isettings.FeedsFull:
		return feed.KeepAll
	case isettings.FeedsSummary:
		return feed.KeepRecent
	case isettings.FeedsOff:
		return feed.RailOnly
	}
	panic("tui: unknown agent feeds setting " + kept)
}

func (a *App) syncFeed() {
	agents := make([]feed.Agent, len(a.subAgents))
	for index, subAgent := range a.subAgents {
		agents[index] = feed.Agent{Name: subAgent.Name, Definition: subAgent.Agent, Model: subAgent.Model, State: subAgent.State, Doing: subAgent.Doing, Since: subAgent.Since, Owns: subAgent.Owns, Report: subAgent.Report}
	}
	a.feed.SetDensity(a.text(isettings.Density))
	a.feed.SetRetention(a.retention())
	a.feed.SetThinking(a.flag(isettings.ShowThinking))
	a.feed.SetAgents(a.busy, agents)
}

func (a *App) setupView(rows int) string {
	var lines []string
	add := func(colour look.Color, text string) {
		lines = append(lines, look.Style(colour).Render(widget.Fit(text, a.width)))
	}
	indented := func(colour look.Color, text string) {
		for _, line := range widget.Wrap(text, a.width-widget.Cells(setupIndent)) {
			add(colour, setupIndent+line)
		}
	}
	add(look.Amber, setupTitle)
	add(look.Text, "")
	if a.setupNote != "" {
		add(look.Mint, a.setupNote)
		add(look.Text, "")
	}
	current, keys := -1, setupKeys
	for index, step := range a.requirements {
		title := strconv.Itoa(index+1) + ". " + cmp.Or(step.Step, step.What)
		switch {
		case step.Done != "":
			add(look.Mint, setupDone+title+setupGap+step.Done)
			add(look.Text, "")
			continue
		case current >= 0:
			add(look.FaintColor, setupLater+title)
			add(look.Text, "")
			continue
		}
		current = index
		add(look.Text, setupNow+title)
		if step.Step != "" {
			indented(look.MutedColor, step.What)
		}
		add(look.Text, "")
		switch {
		case a.entry.variable != "":
			keys = setupEntryKeys
			add(look.Text, setupIndent+a.entry.label)
			add(look.Text, widget.Pad(setupIndent+a.entry.field.Line(), a.width))
			switch {
			case a.entry.checking != 0:
				indented(look.MutedColor, setupChecking)
			case a.entry.refusal != "":
				indented(look.Red, a.entry.refusal)
			case a.entry.field.Warning() != "":
				indented(look.Amber, a.entry.field.Warning())
			}
		case len(step.Choices) > 0:
			for number, choice := range step.Choices {
				add(look.Text, setupIndent+strconv.Itoa(number+1)+"  "+choice.Label)
			}
		default:
			indented(look.MutedColor, "press "+strconv.Itoa(index+1)+" to run   "+step.Fix)
		}
		if step.Fix != "" && a.options.Recheck != nil && a.entry.variable == "" {
			add(look.Text, "")
			indented(look.MutedColor, "or run "+step.Fix+" in another terminal: tofu picks it up here")
		}
		add(look.Text, "")
	}
	add(look.FaintColor, keys)
	for len(lines) < rows {
		add(look.Text, "")
	}
	return strings.Join(lines[:rows], "\n")
}
