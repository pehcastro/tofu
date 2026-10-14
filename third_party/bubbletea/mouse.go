package tea

import (
	"fmt"

	uv "github.com/charmbracelet/ultraviolet"
)

type MouseButton = uv.MouseButton

const (
	MouseNone       = uv.MouseNone
	MouseLeft       = uv.MouseLeft
	MouseMiddle     = uv.MouseMiddle
	MouseRight      = uv.MouseRight
	MouseWheelUp    = uv.MouseWheelUp
	MouseWheelDown  = uv.MouseWheelDown
	MouseWheelLeft  = uv.MouseWheelLeft
	MouseWheelRight = uv.MouseWheelRight
	MouseBackward   = uv.MouseBackward
	MouseForward    = uv.MouseForward
	MouseButton10   = uv.MouseButton10
	MouseButton11   = uv.MouseButton11
)

type MouseMsg interface {
	fmt.Stringer

	Mouse() Mouse
}

type Mouse struct {
	X, Y   int
	Button MouseButton
	Mod    KeyMod
}

func (m Mouse) String() (s string) {
	return uv.Mouse(m).String()
}

type MouseClickMsg Mouse

func (e MouseClickMsg) String() string {
	return Mouse(e).String()
}

func (e MouseClickMsg) Mouse() Mouse {
	return Mouse(e)
}

type MouseReleaseMsg Mouse

func (e MouseReleaseMsg) String() string {
	return Mouse(e).String()
}

func (e MouseReleaseMsg) Mouse() Mouse {
	return Mouse(e)
}

type MouseWheelMsg Mouse

func (e MouseWheelMsg) String() string {
	return Mouse(e).String()
}

func (e MouseWheelMsg) Mouse() Mouse {
	return Mouse(e)
}

type MouseMotionMsg Mouse

func (e MouseMotionMsg) String() string {
	m := Mouse(e)
	if m.Button != 0 {
		return m.String() + "+motion"
	}
	return m.String() + "motion"
}

func (e MouseMotionMsg) Mouse() Mouse {
	return Mouse(e)
}
