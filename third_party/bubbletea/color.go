package tea

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
)

type backgroundColorMsg struct{}

func RequestBackgroundColor() Msg {
	return backgroundColorMsg{}
}

type foregroundColorMsg struct{}

func RequestForegroundColor() Msg {
	return foregroundColorMsg{}
}

type cursorColorMsg struct{}

func RequestCursorColor() Msg {
	return cursorColorMsg{}
}

type ForegroundColorMsg struct{ color.Color }

func (e ForegroundColorMsg) String() string {
	return uv.ForegroundColorEvent(e).String()
}

func (e ForegroundColorMsg) IsDark() bool {
	return uv.ForegroundColorEvent(e).IsDark()
}

type BackgroundColorMsg struct{ color.Color }

func (e BackgroundColorMsg) String() string {
	return uv.BackgroundColorEvent(e).String()
}

func (e BackgroundColorMsg) IsDark() bool {
	return uv.BackgroundColorEvent(e).IsDark()
}

type CursorColorMsg struct{ color.Color }

func (e CursorColorMsg) String() string {
	return uv.CursorColorEvent(e).String()
}

func (e CursorColorMsg) IsDark() bool {
	return uv.CursorColorEvent(e).IsDark()
}
