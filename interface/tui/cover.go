package tui

import (
	"math/rand/v2"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/cover"
	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
	isettings "tofu/internal/settings"
)

const (
	coverPlaceholder = "Type something to start"
	coverMinRows     = 1
	coverMaxRows     = 5
	coverStageMax    = 68
	coverStageMin    = 28
	coverStageInset  = 4
	coverEditorInset = 8
	coverSeparator   = "  ·  "
	coverModelMin    = 10
	coverPathMin     = 8
	ellipsis         = "..."
	settingsLink     = "[settings]"
	settingsCommand  = "/settings"
	poses            = 2
)

type intro struct {
	shown, animated, settings, pressed bool
	identity                           cover.Identity
	input                              textarea.Model
}

func newIntro(shown, animated bool, pose string) intro {
	input := textarea.New()
	input.Placeholder, input.Prompt, input.ShowLineNumbers = coverPlaceholder, "", false
	input.DynamicHeight, input.MinHeight, input.MaxHeight = true, coverMinRows, coverMaxRows
	input.SetVirtualCursor(true)
	styles := textarea.DefaultDarkStyles()
	styles.Focused.Placeholder = styles.Focused.Placeholder.Foreground(lipgloss.Color(string(look.MutedColor)))
	styles.Cursor.Color = lipgloss.Color(string(look.Mint))
	input.SetStyles(styles)
	input.Focus()
	identity := cover.NewIdentity(false)
	if pose == "" && rand.IntN(poses) == 1 || pose != "" && pose != identity.PoseName() {
		identity.TogglePose()
	}
	if pose != "" && pose != identity.PoseName() {
		panic("tui: the cover has no pose named " + pose)
	}
	return intro{shown: shown, animated: animated, identity: identity, input: input}
}

func (i *intro) start() tea.Cmd {
	if !i.shown || !i.animated {
		return nil
	}
	return tea.Batch(i.identity.Init(), textarea.Blink)
}

func (i *intro) resize(width int) {
	i.input.SetWidth(min(coverStageMax, max(coverStageMin, width-coverStageInset)) - coverEditorInset)
}

func (i *intro) animate(msg tea.Msg) tea.Cmd {
	var moved, typed tea.Cmd
	i.identity, moved = i.identity.Update(msg)
	i.input, typed = i.input.Update(msg)
	return tea.Batch(moved, typed)
}

func (a *App) coverDetails() string {
	stage := min(coverStageMax, max(coverStageMin, a.width-coverStageInset))
	model := ansi.Truncate(a.slug(), max(coverModelMin, stage/2), ellipsis)
	room := max(coverPathMin, stage-ansi.StringWidth(model)-2*ansi.StringWidth(coverSeparator)-ansi.StringWidth(settingsLink))
	path := "./" + a.options.Repo
	if over := ansi.StringWidth(path) - room; over > 0 {
		path = ansi.TruncateLeft(path, over+len(ellipsis), ellipsis)
	}
	return look.Muted(path) + look.Faint(coverSeparator) + look.Muted(model) + look.Faint(coverSeparator) + look.Style(look.Blue).Render(settingsLink)
}

func (a *App) onSettingsLink(x, y int) bool {
	hint := ansi.Strip(a.coverDetails())
	lines := strings.Split(ansi.Strip(a.drawn), "\n")
	return y >= 0 && y < len(lines) && strings.Contains(lines[y], hint) && pointer.TextHit(lines[y], settingsLink, x)
}

func (a *App) coverInput(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.Key().Keystroke() {
		case "ctrl+c":
			return tea.Quit, true
		case "alt+s":
			a.coverSettings()
			return nil, true
		case "enter":
			draft := strings.TrimSpace(a.intro.input.Value())
			a.intro.input.Reset()
			if draft == settingsCommand {
				a.coverSettings()
				return nil, true
			}
			focus := a.leaveCover()
			if draft == "" {
				return focus, true
			}
			a.view.Insert(draft)
			return tea.Batch(focus, a.submit()), true
		case "ctrl+j", "shift+enter":
			a.intro.input.InsertString("\n")
			return nil, true
		}
		var cmd tea.Cmd
		a.intro.input, cmd = a.intro.input.Update(msg)
		return cmd, true
	case tea.PasteMsg:
		var cmd tea.Cmd
		a.intro.input, cmd = a.intro.input.Update(msg)
		return cmd, true
	case tea.MouseClickMsg:
		a.intro.pressed = msg.Button == tea.MouseLeft && a.onSettingsLink(msg.X, msg.Y)
		return nil, true
	case tea.MouseReleaseMsg:
		pressed := a.intro.pressed
		a.intro.pressed = false
		if pressed && a.onSettingsLink(msg.X, msg.Y) {
			a.coverSettings()
		}
		return nil, true
	case tea.MouseMotionMsg, tea.MouseWheelMsg:
		return nil, true
	}
	return nil, false
}

func (a *App) leaveCover() tea.Cmd {
	a.intro.shown = false
	a.intro.identity.Pause()
	a.intro.input.Blur()
	return a.show(screenChat)
}

func (a *App) coverSettings() {
	a.intro.shown = false
	a.intro.identity.Pause()
	a.show(screenSettings)
	a.intro.settings = true
}

func (a *App) returnToCover() tea.Cmd {
	a.intro.settings = false
	if a.flag(isettings.HideIntroduction) {
		focus := a.leaveCover()
		a.view.Insert(a.intro.input.Value())
		return focus
	}
	pose := a.intro.identity.PoseName()
	a.intro.identity = cover.NewIdentity(false)
	if a.intro.identity.PoseName() != pose {
		a.intro.identity.TogglePose()
	}
	a.intro.shown, a.current = true, screenChat
	a.intro.input.Focus()
	return a.intro.start()
}
