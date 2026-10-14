package uv

import (
	"github.com/charmbracelet/x/ansi"
)

type MouseMode uint8

const (
	MouseModeNone MouseMode = iota
	MouseModePress
	MouseModeClick
	MouseModeDrag
	MouseModeMotion
)

type MouseEncoding uint8

const (
	MouseEncodingLegacy MouseEncoding = iota
	MouseEncodingSGR
	MouseEncodingSGRPixel
)

type MouseButton = ansi.MouseButton

const (
	MouseNone       = ansi.MouseNone
	MouseLeft       = ansi.MouseLeft
	MouseMiddle     = ansi.MouseMiddle
	MouseRight      = ansi.MouseRight
	MouseWheelUp    = ansi.MouseWheelUp
	MouseWheelDown  = ansi.MouseWheelDown
	MouseWheelLeft  = ansi.MouseWheelLeft
	MouseWheelRight = ansi.MouseWheelRight
	MouseBackward   = ansi.MouseBackward
	MouseForward    = ansi.MouseForward
	MouseButton10   = ansi.MouseButton10
	MouseButton11   = ansi.MouseButton11
)

type Mouse struct {
	X, Y   int
	Button MouseButton
	Mod    KeyMod
}

func (m Mouse) String() (s string) {
	if m.Mod.Contains(ModCtrl) {
		s += "ctrl+"
	}
	if m.Mod.Contains(ModAlt) {
		s += "alt+"
	}
	if m.Mod.Contains(ModShift) {
		s += "shift+"
	}

	str := m.Button.String()
	if str == "" {
		s += "unknown"
	} else if str != "none" {
		s += str
	}

	return s
}

func MousePixelToCell(m Mouse, ws *Winsize) Mouse {
	var col, row int
	if ws.Xpixel > 0 {
		col = m.X * int(ws.Col) / int(ws.Xpixel)
	}
	if ws.Ypixel > 0 {
		row = m.Y * int(ws.Row) / int(ws.Ypixel)
	}

	return Mouse{
		X:      col,
		Y:      row,
		Button: m.Button,
		Mod:    m.Mod,
	}
}
