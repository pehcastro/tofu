package tui

import (
	"math/rand/v2"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/cover"
)

const poses = 2

type intro struct {
	shown, animated, settings bool
	identity                  cover.Identity
}

func newIntro(shown, animated bool, pose string) intro {
	identity := cover.NewIdentity(false)
	if pose == "" && rand.IntN(poses) == 1 || pose != "" && pose != identity.PoseName() {
		identity.TogglePose()
	}
	if pose != "" && pose != identity.PoseName() {
		panic("tui: the cover has no pose named " + pose)
	}
	return intro{shown: shown, animated: animated, identity: identity}
}

func (i *intro) start() tea.Cmd {
	if !i.shown || !i.animated {
		return nil
	}
	return i.identity.Init()
}

func (a *App) returnToCover() tea.Cmd { return a.show(screenChat) }
