package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/internal/ui/tui/chat/session"
	"tofu/internal/ui/tui/input/pointer"
)

const (
	selectionUnit  = "the selection"
	settingsHit    = -1
	cardEdges      = "│╭╮╰╯─"
	cardSide       = "│"
	cardPadding    = "  "
	pageTrackInset = 4
)

func (a *App) track() (pointer.Track, bool) {
	var track pointer.Track
	switch a.current {
	case screenChat:
		track = a.view.Track()
	case screenAgents:
		track = a.feed.Track()
	case screenEdits:
		track = a.edits.Track()
	case screenShells:
		track = a.shells.Track()
	case screenSettings:
		return track, false
	}
	track.Top = bodyTop
	if a.current != screenChat {
		track.Height = a.height - pageTrackInset
	}
	return track, !a.settingUp()
}

func (a *App) pane() pointer.Pane {
	split := 0
	switch a.current {
	case screenAgents:
		split = a.feed.Split()
	case screenEdits:
		split = a.edits.Split()
	case screenShells:
		split = a.shells.Split()
	case screenChat, screenSettings:
	}
	return pointer.Pane{Split: split, Width: a.width, Top: bodyTop, Bottom: a.height - 2}
}

type heldSpan struct {
	from, to   session.Point
	on, placed bool
}

type edgeScrollMsg struct{}

func (a *App) place() {
	if a.span.on && !a.span.placed {
		a.span.from, a.span.placed = a.view.PointAt(a.selection.Start.X, a.selection.Start.Y-bodyTop), true
	}
}

func (a *App) edge() string {
	if !a.span.on || !a.selection.Dragged {
		return ""
	}
	switch y := a.selection.End.Y; {
	case y <= bodyTop:
		return session.WheelUp
	case y >= bodyTop+a.view.Track().Height-1:
		return session.WheelDown
	}
	return ""
}

func edgeTick() tea.Cmd {
	return tea.Tick(pulseInterval, func(time.Time) tea.Msg { return edgeScrollMsg{} })
}

func (a *App) edgeScroll() tea.Cmd {
	turn := a.edge()
	if a.edgeTicking = turn != ""; !a.edgeTicking {
		return nil
	}
	a.view.Scroll(turn)
	a.span.to = a.view.PointAt(a.selection.End.X, a.selection.End.Y-bodyTop)
	return edgeTick()
}

func (a *App) press(msg tea.MouseClickMsg) {
	if msg.Button != tea.MouseLeft {
		return
	}
	track, scrolls := a.track()
	scrolls = scrolls && len(a.dialogs) == 0
	if scrolls {
		if drag, grabbed := pointer.Press(msg.X, msg.Y, a.width, track); grabbed {
			a.selection, a.drag, a.span = pointer.Selection{}, drag, heldSpan{}
			a.scrollTo(drag.Scroll(msg.Y))
			return
		}
	}
	at := pointer.Cell{X: msg.X, Y: msg.Y}
	a.frozen = ""
	a.selection = pointer.Selection{Start: at, End: at, Pressed: true, Mods: pointer.Mods{Alt: msg.Mod&tea.ModAlt != 0, Shift: msg.Mod&tea.ModShift != 0, Ctrl: msg.Mod&tea.ModCtrl != 0}}
	a.span = heldSpan{on: scrolls && a.current == screenChat && track.Total > 0 && msg.Y >= bodyTop && msg.Y < bodyTop+track.Height}
}

func (a *App) motion(msg tea.MouseMotionMsg) tea.Cmd {
	if a.drag.Active {
		a.scrollTo(a.drag.Scroll(msg.Y))
		return nil
	}
	if !a.selection.Pressed || msg.Button != tea.MouseLeft {
		return nil
	}
	a.selection.End = pointer.Cell{X: msg.X, Y: msg.Y}
	a.selection.Dragged = a.selection.Dragged || pointer.Distance(a.selection.Start, a.selection.End) > 1
	if !a.selection.Dragged {
		return nil
	}
	if !a.span.on {
		if a.frozen == "" {
			a.frozen = a.frame()
		}
		return nil
	}
	a.place()
	a.span.to = a.view.PointAt(msg.X, msg.Y-bodyTop)
	if a.edgeTicking || a.edge() == "" {
		return nil
	}
	a.edgeTicking = true
	return edgeTick()
}

func (a *App) release(msg tea.MouseReleaseMsg) tea.Cmd {
	if a.drag.Active {
		a.scrollTo(a.drag.Scroll(msg.Y))
		a.drag = pointer.Drag{}
		return nil
	}
	if !a.selection.Pressed {
		return nil
	}
	held := a.selection
	held.Pressed, held.End = false, pointer.Cell{X: msg.X, Y: msg.Y}
	held.Dragged = held.Dragged || pointer.Distance(held.Start, held.End) > 1
	a.selection = held
	text := ""
	if held.Dragged {
		if a.span.on {
			a.place()
			a.span.to = a.view.PointAt(msg.X, msg.Y-bodyTop)
		}
		text = a.selectedText()
	}
	a.selection, a.frozen, a.span = pointer.Selection{}, "", heldSpan{}
	switch {
	case !held.Dragged:
		return a.click(held.Start.X, held.Start.Y, held.Mods)
	case text == "":
		return nil
	}
	a.lastSelection = text
	return a.copy(selectionUnit, text, true)
}

func (a *App) selectedText() string {
	if a.span.on {
		return a.view.Between(a.span.from, a.span.to)
	}
	frame := a.frozen
	if frame == "" {
		frame = a.frame()
	}
	text := pointer.SelectedText(frame, a.selection, a.pane())
	if a.current != screenAgents && a.current != screenEdits {
		return text
	}
	return unframed(text)
}

func unframed(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if strings.ContainsAny(line, cardEdges) && strings.Trim(line, " "+cardEdges) == "" {
			continue
		}
		if rest, bordered := strings.CutPrefix(strings.TrimLeft(line, " "), cardSide); bordered {
			line = strings.TrimPrefix(rest, cardPadding)
		}
		kept = append(kept, strings.TrimRight(strings.TrimSuffix(strings.TrimRight(line, " "), cardSide), " "))
	}
	indent := -1
	for _, line := range kept[min(1, len(kept)):] {
		if lead := len(line) - len(strings.TrimLeft(line, " ")); line != "" && (indent < 0 || lead < indent) {
			indent = lead
		}
	}
	for index := 1; index < len(kept); index++ {
		kept[index] = kept[index][min(len(kept[index]), max(0, indent)):]
	}
	return strings.Join(kept, "\n")
}

func (a *App) click(x, y int, mods pointer.Mods) tea.Cmd {
	if a.settingUp() {
		return nil
	}
	ref := pointer.ReferenceAt(a.drawn, x, y)
	collectOnly := mods.Shift || mods.Alt && mods.Ctrl
	if len(a.dialogs) == 0 && (mods.Alt || mods.Shift || mods.Ctrl) && a.insertReference(ref, !collectOnly) {
		return a.view.Focus()
	}
	if top := a.top(); top != nil {
		return top.click(a, x, y)
	}
	tab, onTab := a.tabAt(x, y)
	if onTab && tab == settingsHit {
		return a.show(screenSettings)
	}
	if a.current == screenSettings {
		return a.applyIntent(a.settings.Click(x, y))
	}
	if onTab {
		return a.show(screen(tab))
	}
	if y < bodyTop || y >= a.height-1 {
		return nil
	}
	switch a.current {
	case screenAgents:
		followed, handled := a.feed.Click(x, y-bodyTop)
		if followed != "" {
			return a.follow(followed)
		}
		if handled {
			return nil
		}
	case screenEdits:
		a.edits.Click(x, y-bodyTop)
		return nil
	case screenShells:
		a.shells.Click(x, y-bodyTop)
		return nil
	case screenChat:
		if id, call := a.view.ExpandAt(x, y-bodyTop); id != "" {
			return a.open(id, call)
		}
		cmd := a.follow(ref)
		if a.current != screenChat {
			_, id := pointer.SplitReference(ref)
			a.recordReach(strings.TrimPrefix(id, "#"))
		}
		return cmd
	}
	return a.follow(ref)
}

func (a *App) tabAt(x, y int) (int, bool) {
	if y != 0 || a.current == screenSettings {
		return 0, false
	}
	for _, hit := range a.hits {
		if x >= hit.Start && x < hit.End {
			return hit.Index, true
		}
	}
	return 0, false
}

func (a *App) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return nil
	}
	if top := a.top(); top != nil {
		if diff, reading := top.(*diffDialog); reading {
			diff.scroll = max(0, diff.scroll+delta*wheelRows)
			return nil
		}
		code := tea.KeyDown
		if delta < 0 {
			code = tea.KeyUp
		}
		return top.key(a, tea.KeyPressMsg{Code: code})
	}
	if a.settingUp() || a.intro.shown {
		return nil
	}
	switch a.current {
	case screenChat:
		turn := session.WheelDown
		if delta < 0 {
			turn = session.WheelUp
		}
		a.place()
		a.view.Scroll(turn)
		if a.span.on {
			a.selection.End, a.selection.Dragged = pointer.Cell{X: msg.X, Y: msg.Y}, true
			a.span.to = a.view.PointAt(msg.X, msg.Y-bodyTop)
		}
	case screenAgents:
		a.feed.Wheel(delta)
	case screenEdits:
		pane := "right"
		if msg.X < a.edits.Split() {
			pane = "left"
		}
		a.edits.Key(pane)
		a.edits.Wheel(delta < 0)
	case screenShells:
		a.shells.Wheel(delta * wheelRows)
	case screenSettings:
		return a.applyIntent(a.settings.Wheel(msg.X, delta))
	}
	return nil
}

func (a *App) scrollTo(behindNewest int) {
	switch a.current {
	case screenChat:
		a.view.SetScroll(behindNewest)
	case screenAgents:
		a.feed.SetScroll(behindNewest)
	case screenEdits:
		a.edits.SetScroll(behindNewest)
	case screenShells:
		track := a.shells.Track()
		a.shells.Wheel(track.Total - track.Visible - track.FromTop - behindNewest)
	case screenSettings:
	}
}
