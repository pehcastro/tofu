package tui

import (
	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/pointer"
	"tofu/interface/tui/session"
)

const (
	selectionUnit = "the selection"
	settingsHit   = -1
)

type scrolledPane interface {
	Track() pointer.Track
	Split() int
}

func (a *App) scrolledPane() (scrolledPane, bool) {
	switch a.current {
	case screenAgents:
		return a.feed, true
	case screenEdits:
		return a.edits, true
	case screenShells:
		return a.shells, true
	case screenChat, screenSettings:
	}
	return nil, false
}

func (a *App) track() (pointer.Track, bool) {
	scrolled, has := a.scrolledPane()
	if !has || len(a.requirements) > 0 {
		return pointer.Track{}, false
	}
	track := scrolled.Track()
	track.Top += bodyTop
	return track, true
}

func (a *App) pane() pointer.Pane {
	split := 0
	if scrolled, has := a.scrolledPane(); has {
		split = scrolled.Split()
	}
	return pointer.Pane{Split: split, Width: a.width, Top: bodyTop, Bottom: a.height - 2}
}

func (a *App) press(msg tea.MouseClickMsg) {
	if msg.Button != tea.MouseLeft {
		return
	}
	if track, scrolls := a.track(); scrolls && len(a.dialogs) == 0 {
		if drag, grabbed := pointer.Press(msg.X, msg.Y, a.width, track); grabbed {
			a.selection, a.drag = pointer.Selection{}, drag
			a.scrollTo(drag.Scroll(msg.Y))
			return
		}
	}
	at := pointer.Cell{X: msg.X, Y: msg.Y}
	a.frozen = ""
	a.selection = pointer.Selection{Start: at, End: at, Pressed: true, Mods: pointer.Mods{Alt: msg.Mod&tea.ModAlt != 0, Shift: msg.Mod&tea.ModShift != 0, Ctrl: msg.Mod&tea.ModCtrl != 0}}
}

func (a *App) motion(msg tea.MouseMotionMsg) {
	if a.drag.Active {
		a.scrollTo(a.drag.Scroll(msg.Y))
		return
	}
	if !a.selection.Pressed || msg.Button != tea.MouseLeft {
		return
	}
	a.selection.End = pointer.Cell{X: msg.X, Y: msg.Y}
	a.selection.Dragged = a.selection.Dragged || pointer.Distance(a.selection.Start, a.selection.End) > 1
	if a.selection.Dragged && a.frozen == "" {
		a.frozen = a.frame()
	}
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
	if held.Dragged {
		text := a.selectedText()
		a.selection, a.frozen = pointer.Selection{}, ""
		if text == "" {
			return nil
		}
		a.lastSelection = text
		return a.copy(selectionUnit, text, true)
	}
	a.selection = pointer.Selection{}
	return a.click(held.Start.X, held.Start.Y, held.Mods)
}

func (a *App) selectedText() string {
	frame := a.frozen
	if frame == "" {
		frame = a.frame()
	}
	return pointer.SelectedText(frame, a.selection, a.pane())
}

func (a *App) click(x, y int, mods pointer.Mods) tea.Cmd {
	if len(a.requirements) > 0 {
		return nil
	}
	ref := pointer.ReferenceAt(a.drawn, x, y)
	collectOnly := mods.Shift || mods.Alt && mods.Ctrl
	if len(a.dialogs) == 0 && (mods.Alt || mods.Shift || mods.Ctrl) && a.insertReference(ref, !collectOnly) {
		return a.view.Focus()
	}
	tab, onTab := a.tabAt(x, y)
	if onTab && tab == settingsHit {
		return a.show(screenSettings)
	}
	if top := a.top(); top != nil {
		return top.click(a, x, y)
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
	if len(a.requirements) > 0 || a.intro.shown {
		return nil
	}
	switch a.current {
	case screenChat:
		turn := session.WheelDown
		if delta < 0 {
			turn = session.WheelUp
		}
		a.view.Scroll(turn)
	case screenAgents:
		a.feed.Wheel(msg.X, msg.Y-bodyTop, delta)
	case screenEdits:
		a.edits.Wheel(delta < 0)
	case screenShells:
		a.shells.Wheel(delta * wheelRows)
	case screenSettings:
		return a.applyIntent(a.settings.Wheel(delta))
	}
	return nil
}

func (a *App) scrollTo(behindNewest int) {
	track, scrolls := a.track()
	if !scrolls {
		return
	}
	rows := behindNewest - (track.Total - track.Visible - track.FromTop)
	switch a.current {
	case screenAgents:
		a.feed.Wheel(0, 0, -rows/wheelRows)
	case screenEdits:
		for ; rows >= wheelRows; rows -= wheelRows {
			a.edits.Wheel(true)
		}
		for ; rows <= -wheelRows; rows += wheelRows {
			a.edits.Wheel(false)
		}
	case screenShells:
		a.shells.Wheel(-rows)
	case screenChat, screenSettings:
	}
}
