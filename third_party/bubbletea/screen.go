package tea

import "github.com/charmbracelet/x/ansi"

type WindowSizeMsg struct {
	Width  int
	Height int
}

func ClearScreen() Msg {
	return clearScreenMsg{}
}

type clearScreenMsg struct{}

type ModeReportMsg struct {
	Mode ansi.Mode

	Value ansi.ModeSetting
}
