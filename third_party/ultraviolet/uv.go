package uv

import (
	"fmt"
	"image/color"
	"io"

	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

var (
	ErrNotTerminal = fmt.Errorf("not a terminal")

	ErrPlatformNotSupported = fmt.Errorf("platform not supported")
)

type Drawable interface {
	Draw(scr Screen, area Rectangle)
}

type DrawableFunc func(scr Screen, rect Rectangle)

var _ Drawable = (DrawableFunc)(nil)

func (f DrawableFunc) Draw(scr Screen, rect Rectangle) {
	f(scr, rect)
}

type WidthMethod interface {
	StringWidth(s string) int
}

type Screen interface {
	Bounds() Rectangle

	CellAt(x, y int) *Cell

	SetCell(x, y int, c *Cell)

	WidthMethod() WidthMethod
}

type Cursor struct {
	Position

	Color color.Color

	Shape CursorShape

	Blink bool

	Hidden bool
}

func NewCursor(x, y int) *Cursor {
	return &Cursor{
		Position: Position{X: x, Y: y},
		Color:    nil,
		Shape:    CursorBlock,
		Blink:    true,
	}
}

type ProgressBarState int

const (
	ProgressBarNone ProgressBarState = iota
	ProgressBarDefault
	ProgressBarError
	ProgressBarIndeterminate
	ProgressBarWarning
)

func (s ProgressBarState) String() string {
	return [...]string{
		"None",
		"Default",
		"Error",
		"Indeterminate",
		"Warning",
	}[s]
}

type ProgressBar struct {
	State ProgressBarState

	Value int
}

func NewProgressBar(state ProgressBarState, value int) *ProgressBar {
	return &ProgressBar{
		State: state,
		Value: min(max(value, 0), 100),
	}
}

type KeyboardEnhancements struct {
	DisambiguateEscapeCodes bool

	ReportEventTypes bool

	ReportAlternateKeys bool

	ReportAllKeysAsEscapeCodes bool

	ReportAssociatedText bool
}

func NewKeyboardEnhancements(flags int) *KeyboardEnhancements {
	if flags <= 0 {
		return &KeyboardEnhancements{}
	}

	return &KeyboardEnhancements{
		DisambiguateEscapeCodes: flags&ansi.KittyDisambiguateEscapeCodes != 0,
		ReportEventTypes:        flags&ansi.KittyReportEventTypes != 0,
	}
}

func (ke KeyboardEnhancements) Flags() int {
	bits := 0
	if ke.DisambiguateEscapeCodes {
		bits |= ansi.KittyDisambiguateEscapeCodes
	}
	if ke.ReportEventTypes {
		bits |= ansi.KittyReportEventTypes
	}
	if ke.ReportAlternateKeys {
		bits |= ansi.KittyReportAlternateKeys
	}
	if ke.ReportAllKeysAsEscapeCodes {
		bits |= ansi.KittyReportAllKeysAsEscapeCodes
	}
	if ke.ReportAssociatedText {
		bits |= ansi.KittyReportAssociatedKeys
	}

	return bits
}

func EncodeBackgroundColor(w io.Writer, c color.Color) error {
	var seq string
	if c == nil {
		seq = ansi.ResetBackgroundColor
	} else if col, ok := colorful.MakeColor(c); ok {
		seq = ansi.SetBackgroundColor(col.Hex())
	} else {
		return fmt.Errorf("invalid color: %v", c)
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set background color: %w", err)
	}

	return nil
}

func EncodeForegroundColor(w io.Writer, c color.Color) error {
	var seq string
	if c == nil {
		seq = ansi.ResetForegroundColor
	} else if col, ok := colorful.MakeColor(c); ok {
		seq = ansi.SetForegroundColor(col.Hex())
	} else {
		return fmt.Errorf("invalid color: %v", c)
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set foreground color: %w", err)
	}

	return nil
}

func EncodeCursorColor(w io.Writer, c color.Color) error {
	var seq string
	if c == nil {
		seq = ansi.ResetCursorColor
	} else if col, ok := colorful.MakeColor(c); ok {
		seq = ansi.SetCursorColor(col.Hex())
	} else {
		return fmt.Errorf("invalid color: %v", c)
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set cursor color: %w", err)
	}

	return nil
}

func EncodeCursorStyle(w io.Writer, shape CursorShape, blink bool) error {
	seq := ansi.SetCursorStyle(shape.Encode(blink))
	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set cursor style: %w", err)
	}

	return nil
}

func EncodeBracketedPaste(w io.Writer, enable bool) error {
	var seq string
	if enable {
		seq = ansi.SetModeBracketedPaste
	} else {
		seq = ansi.ResetModeBracketedPaste
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set bracketed paste mode: %w", err)
	}

	return nil
}

func EncodeMouseMode(w io.Writer, mode MouseMode) error {
	var seq string
	switch mode {
	case MouseModeNone:
		seq = ansi.ResetModeMouseX10 +
			ansi.ResetModeMouseNormal +
			ansi.ResetModeMouseButtonEvent +
			ansi.ResetModeMouseAnyEvent
	case MouseModePress:
		seq = ansi.SetModeMouseX10
	case MouseModeClick:
		seq = ansi.SetModeMouseNormal
	case MouseModeDrag:
		seq = ansi.SetModeMouseButtonEvent
	case MouseModeMotion:
		seq = ansi.SetModeMouseAnyEvent
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set mouse mode: %w", err)
	}

	return nil
}

func EncodeMouseEncoding(w io.Writer, enc MouseEncoding) error {
	var seq string
	switch enc {
	case MouseEncodingLegacy:
		seq = ansi.ResetModeMouseExtSgr +
			ansi.ResetModeMouseExtUrxvt +
			ansi.ResetModeMouseExtSgrPixel
	case MouseEncodingSGR:
		seq = ansi.SetModeMouseExtSgr
	case MouseEncodingSGRPixel:
		seq = ansi.SetModeMouseExtSgrPixel
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set mouse encoding: %w", err)
	}

	return nil
}

func EncodeProgressBar(w io.Writer, pb *ProgressBar) error {
	if pb == nil {
		pb = &ProgressBar{State: ProgressBarNone}
	}

	var seq string
	percent := clamp(pb.Value, 0, 100)
	switch pb.State {
	case ProgressBarNone:
		seq = ansi.ResetProgressBar
	case ProgressBarDefault:
		seq = ansi.SetProgressBar(percent)
	case ProgressBarError:
		seq = ansi.SetErrorProgressBar(percent)
	case ProgressBarIndeterminate:
		seq = ansi.SetIndeterminateProgressBar
	case ProgressBarWarning:
		seq = ansi.SetWarningProgressBar(percent)
	default:
		return fmt.Errorf("invalid progress bar state: %d", pb.State)
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set progress bar: %w", err)
	}

	return nil
}

func EncodeKeyboardEnhancements(w io.Writer, ke *KeyboardEnhancements) error {
	var flags int
	if ke != nil {
		flags = ke.Flags()
	}
	_, err := io.WriteString(w, ansi.KittyKeyboard(flags, 1))
	if err != nil {
		return fmt.Errorf("failed to set keyboard enhancements: %w", err)
	}
	return nil
}

func EncodeWindowTitle(w io.Writer, title string) error {
	_, err := io.WriteString(w, ansi.SetWindowTitle(title))
	if err != nil {
		return fmt.Errorf("failed to set window title: %w", err)
	}
	return nil
}
