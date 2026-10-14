package uv

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

type Event interface{}

type EventStreamer interface {
	StreamEvents(ctx context.Context, ch chan<- Event) error
}

type UnknownEvent string

func (e UnknownEvent) String() string {
	return fmt.Sprintf("%q", string(e))
}

type UnknownCsiEvent string

func (e UnknownCsiEvent) String() string {
	return fmt.Sprintf("%q", string(e))
}

type UnknownSs3Event string

func (e UnknownSs3Event) String() string {
	return fmt.Sprintf("%q", string(e))
}

type UnknownOscEvent string

func (e UnknownOscEvent) String() string {
	return fmt.Sprintf("%q", string(e))
}

type UnknownDcsEvent string

func (e UnknownDcsEvent) String() string {
	return fmt.Sprintf("%q", string(e))
}

type UnknownSosEvent string

func (e UnknownSosEvent) String() string {
	return fmt.Sprintf("%q", string(e))
}

type UnknownPmEvent string

func (e UnknownPmEvent) String() string {
	return fmt.Sprintf("%q", string(e))
}

type UnknownApcEvent string

func (e UnknownApcEvent) String() string {
	return fmt.Sprintf("%q", string(e))
}

type MultiEvent []Event

func (e MultiEvent) String() string {
	var sb strings.Builder
	for _, ev := range e {
		fmt.Fprintf(&sb, "%v\n", ev)
	}
	return sb.String()
}

type Size struct {
	Width  int
	Height int
}

func (s Size) Bounds() Rectangle {
	return Rectangle{
		Min: image.Point{X: 0, Y: 0},
		Max: image.Point{X: s.Width, Y: s.Height},
	}
}

type WindowSizeEvent Size

func (s WindowSizeEvent) Bounds() Rectangle {
	return Size(s).Bounds()
}

type PixelSizeEvent Size

func (s PixelSizeEvent) Bounds() Rectangle {
	return Size(s).Bounds()
}

type CellSizeEvent Size

func (s CellSizeEvent) Bounds() Rectangle {
	return Size(s).Bounds()
}

type KeyPressEvent Key

func (k KeyPressEvent) MatchString(s ...string) bool {
	return Key(k).MatchString(s...)
}

func (k KeyPressEvent) String() string {
	return Key(k).String()
}

func (k KeyPressEvent) Keystroke() string {
	return Key(k).Keystroke()
}

func (k KeyPressEvent) Key() Key {
	return Key(k)
}

type KeyReleaseEvent Key

func (k KeyReleaseEvent) MatchString(s ...string) bool {
	return Key(k).MatchString(s...)
}

func (k KeyReleaseEvent) String() string {
	return Key(k).String()
}

func (k KeyReleaseEvent) Keystroke() string {
	return Key(k).Keystroke()
}

func (k KeyReleaseEvent) Key() Key {
	return Key(k)
}

type KeyEvent interface {
	fmt.Stringer

	Key() Key
}

type MouseEvent interface {
	fmt.Stringer

	Mouse() Mouse
}

type MouseClickEvent Mouse

func (e MouseClickEvent) String() string {
	return Mouse(e).String()
}

func (e MouseClickEvent) Mouse() Mouse {
	return Mouse(e)
}

type MouseReleaseEvent Mouse

func (e MouseReleaseEvent) String() string {
	return Mouse(e).String()
}

func (e MouseReleaseEvent) Mouse() Mouse {
	return Mouse(e)
}

type MouseWheelEvent Mouse

func (e MouseWheelEvent) String() string {
	return Mouse(e).String()
}

func (e MouseWheelEvent) Mouse() Mouse {
	return Mouse(e)
}

type MouseMotionEvent Mouse

func (e MouseMotionEvent) String() string {
	m := Mouse(e)
	if m.Button != 0 {
		return m.String() + "+motion"
	}
	return m.String() + "motion"
}

func (e MouseMotionEvent) Mouse() Mouse {
	return Mouse(e)
}

type CursorPositionEvent struct {
	X, Y int
}

type FocusEvent struct{}

type BlurEvent struct{}

type DarkColorSchemeEvent struct{}

type LightColorSchemeEvent struct{}

type PasteEvent struct {
	Content string
}

func (e PasteEvent) String() string {
	return e.Content
}

type PasteStartEvent struct{}

type PasteEndEvent struct{}

type TerminalVersionEvent struct {
	Name string
}

func (e TerminalVersionEvent) String() string {
	return e.Name
}

type ModifyOtherKeysEvent struct {
	Mode int
}

type KittyGraphicsEvent struct {
	Options kitty.Options
	Payload []byte
}

type KeyboardEnhancementsEvent struct {
	Flags int
}

func (e KeyboardEnhancementsEvent) Contains(enhancements int) bool {
	return e.Flags&enhancements == enhancements
}

func (e KeyboardEnhancementsEvent) SupportsKeyDisambiguation() bool {
	return e.Flags&ansi.KittyDisambiguateEscapeCodes != 0
}

func (e KeyboardEnhancementsEvent) SupportsKeyReleases() bool {
	return e.Flags&ansi.KittyReportEventTypes != 0
}

func (e KeyboardEnhancementsEvent) SupportsUniformKeyLayout() bool {
	return e.SupportsKeyDisambiguation() &&
		e.Flags&ansi.KittyReportAlternateKeys != 0 &&
		e.Flags&ansi.KittyReportAllKeysAsEscapeCodes != 0
}

type PrimaryDeviceAttributesEvent []int

type SecondaryDeviceAttributesEvent []int

type TertiaryDeviceAttributesEvent string

type ModeReportEvent struct {
	Mode ansi.Mode

	Value ansi.ModeSetting
}

type ForegroundColorEvent struct{ color.Color }

func (e ForegroundColorEvent) String() string {
	return colorToHex(e.Color)
}

func (e ForegroundColorEvent) IsDark() bool {
	return isDarkColor(e.Color)
}

type BackgroundColorEvent struct{ color.Color }

func (e BackgroundColorEvent) String() string {
	return colorToHex(e)
}

func (e BackgroundColorEvent) IsDark() bool {
	return isDarkColor(e.Color)
}

type CursorColorEvent struct{ color.Color }

func (e CursorColorEvent) String() string {
	return colorToHex(e)
}

func (e CursorColorEvent) IsDark() bool {
	return isDarkColor(e)
}

type WindowOpEvent struct {
	Op   int
	Args []int
}

type CapabilityEvent struct {
	Content string
}

func (e CapabilityEvent) String() string {
	return e.Content
}

type ClipboardSelection = byte

const (
	SystemClipboard  ClipboardSelection = ansi.SystemClipboard
	PrimaryClipboard ClipboardSelection = ansi.PrimaryClipboard
)

type ClipboardEvent struct {
	Content   string
	Selection ClipboardSelection
}

func (e ClipboardEvent) String() string {
	return e.Content
}

func (e ClipboardEvent) Clipboard() ClipboardSelection {
	return e.Selection
}

type ignoredEvent string
