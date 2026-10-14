package tea

import (
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

type nilRenderer struct{}

var _ renderer = nilRenderer{}

func (n nilRenderer) start() {}

func (n nilRenderer) clearScreen() {}

func (n nilRenderer) insertAbove(string) error { return nil }

func (n nilRenderer) resize(int, int) {}

func (n nilRenderer) setColorProfile(colorprofile.Profile) {}

func (nilRenderer) flush(bool) error { return nil }

func (nilRenderer) close() error { return nil }

func (nilRenderer) render(View) {}

func (nilRenderer) reset() {}

func (nilRenderer) writeString(string) (int, error) { return 0, nil }

func (n nilRenderer) setSyncdUpdates(bool) {}

func (n nilRenderer) setWidthMethod(ansi.Method) {}

func (n nilRenderer) onMouse(MouseMsg) Cmd {
	return nil
}
