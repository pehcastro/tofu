package uv

import (
	"bytes"
	"image/color"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

type TerminalScreen struct {
	win     *Window
	w       io.Writer
	buf     *bytes.Buffer
	rend    *TerminalRenderer
	rbuf    *RenderBuffer
	env     Environ
	profile colorprofile.Profile

	altScreen            bool
	keyboardEnhancements *KeyboardEnhancements
	bracketedPaste       bool
	mouseMode            MouseMode
	mouseEncoding        MouseEncoding
	cursor               *Cursor
	backgroundColor      color.Color
	foregroundColor      color.Color
	progressBar          *ProgressBar
	windowTitle          string
	syncUpdates          bool
	resetTabs            bool

	mu sync.Mutex

	widthMethodOverride bool
}

var _ Screen = (*TerminalScreen)(nil)

func NewTerminalScreen(w io.Writer, env Environ) *TerminalScreen {
	s := &TerminalScreen{}
	s.buf = &bytes.Buffer{}
	s.win = NewWindow(0, 0, nil)
	s.w = w
	s.profile = colorprofile.Detect(w, env)
	s.rend = NewTerminalRenderer(s.buf, env)
	s.rend.SetFullscreen(false)
	s.rend.SetRelativeCursor(true)
	s.rend.SetColorProfile(s.profile)
	s.rbuf = NewRenderBuffer(0, 0)
	s.env = env

	if debugFile := env.Getenv("UV_DEBUG"); debugFile != "" {
		if f, err := os.OpenFile(debugFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			log.SetOutput(f)
			s.rend.SetLogger(log.Default())
		}
	}

	f, ok := w.(term.File)
	if ok {
		state, err := term.GetState(f.Fd())
		if err == nil || isWindows {
			useTabs, useBspace := optimizeMovements(state)
			if useTabs {
				s.rend.SetTabStops(0)
			} else {
				s.rend.SetTabStops(-1)
			}
			s.rend.SetBackspace(useBspace)
			s.resetTabs = useTabs
		}
	}

	s.rend.SetMapNewline(false)
	return s
}

func (s *TerminalScreen) CellAt(x, y int) *Cell {
	return s.win.CellAt(x, y)
}

func (s *TerminalScreen) SetCell(x, y int, cell *Cell) {
	s.win.SetCell(x, y, cell)
}

func (s *TerminalScreen) Bounds() Rectangle {
	return s.win.Bounds()
}

func (s *TerminalScreen) Width() int {
	return s.win.Width()
}

func (s *TerminalScreen) Height() int {
	return s.win.Height()
}

func (s *TerminalScreen) StringWidth(str string) int {
	return s.win.WidthMethod().StringWidth(str)
}

func (s *TerminalScreen) WidthMethod() WidthMethod {
	return s.win.WidthMethod()
}

func (s *TerminalScreen) SetWidthMethod(method ansi.Method) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.widthMethodOverride = true
	s.setWidthMethod(method)
}

func (s *TerminalScreen) setWidthMethod(method ansi.Method) {
	s.win.SetWidthMethod(method)
	s.rend.SetWidthMethod(method)
}

func (s *TerminalScreen) requestGraphemeWidth() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.WriteString(ansi.RequestMode(ansi.ModeUnicodeCore))
}

func (s *TerminalScreen) enableGraphemeWidth() {
	s.mu.Lock()
	if s.widthMethodOverride {
		s.mu.Unlock()
		return
	}
	s.buf.WriteString(ansi.SetMode(ansi.ModeUnicodeCore))
	s.setWidthMethod(ansi.GraphemeWidth)
	s.rend.SetGraphemeWidth(true)
	s.mu.Unlock()
	_ = s.Flush()
}

func (s *TerminalScreen) SetColorProfile(profile colorprofile.Profile) {
	s.profile = profile
	s.rend.SetColorProfile(profile)
}

func (s *TerminalScreen) Resize(width, height int) {
	s.win.Resize(width, height)
	s.rbuf.Resize(width, height)
	s.rend.Resize(width, height)
	s.rend.Erase()
	s.rbuf.Touched = nil
}

func (s *TerminalScreen) Display(d Drawable) error {
	if d != nil {
		s.win.Clear()
		d.Draw(s, s.win.Bounds())
	}
	s.Render()
	return s.Flush()
}

func (s *TerminalScreen) Render() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for y := 0; y < s.win.Height(); y++ {
		for x := 0; x < s.win.Width(); {
			cell := s.win.CellAt(x, y)
			if cell == nil || cell.IsZero() {
				x++
				continue
			}
			s.rbuf.SetCell(x, y, cell)
			width := cell.Width
			if width <= 0 {
				width = 1
			}
			x += width
		}
	}
	s.rend.Render(s.rbuf)
	_ = s.rend.Flush()
}

func (s *TerminalScreen) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cursor != nil && !s.cursor.Hidden && s.cursor.X >= 0 && s.cursor.Y >= 0 {
		s.rend.MoveTo(s.cursor.X, s.cursor.Y)
	} else if !s.altScreen {

		x, y := s.rend.Position()
		if x >= s.win.Width()-1 {
			s.rend.MoveTo(0, y)
		}
	}

	var buf bytes.Buffer
	buf.Grow(s.buf.Len())

	if s.buf.Len() > 0 {
		if s.syncUpdates {
			buf.Grow(len(ansi.SetModeSynchronizedOutput) + len(ansi.ResetModeSynchronizedOutput))

			buf.WriteString(ansi.SetModeSynchronizedOutput)
		} else if s.cursor != nil && !s.cursor.Hidden {
			buf.Grow(len(ansi.HideCursor) + len(ansi.ShowCursor))

			buf.WriteString(ansi.HideCursor)
		}

		buf.Write(s.buf.Bytes())

		if s.syncUpdates {
			buf.WriteString(ansi.ResetModeSynchronizedOutput)
		} else if s.cursor != nil && !s.cursor.Hidden {
			buf.WriteString(ansi.ShowCursor)
		}
	}

	_, err := s.w.Write(buf.Bytes())
	if err != nil {
		return err
	}
	s.buf.Reset()
	return nil
}

func (s *TerminalScreen) EnterAltScreen() {
	var sb strings.Builder
	sb.WriteString(ansi.SetModeAltScreenSaveCursor)
	if s.cursor == nil || s.cursor.Hidden {
		sb.WriteString(ansi.HideCursor)
	} else if s.cursor != nil && !s.cursor.Hidden {
		sb.WriteString(ansi.ShowCursor)
	}
	if s.keyboardEnhancements != nil {
		EncodeKeyboardEnhancements(&sb, s.keyboardEnhancements)
	}
	s.buf.WriteString(sb.String())

	if !s.altScreen {
		s.rend.SaveCursor()
		s.rend.Erase()
		s.rend.SetFullscreen(true)
		s.rend.SetRelativeCursor(false)
		s.altScreen = true
	}
}

func (s *TerminalScreen) ExitAltScreen() {
	var sb strings.Builder
	sb.WriteString(ansi.ResetModeAltScreenSaveCursor)
	if s.cursor == nil || s.cursor.Hidden {
		sb.WriteString(ansi.HideCursor)
	} else if s.cursor != nil && !s.cursor.Hidden {
		sb.WriteString(ansi.ShowCursor)
	}
	if s.keyboardEnhancements != nil {
		EncodeKeyboardEnhancements(&sb, s.keyboardEnhancements)
	}
	s.buf.WriteString(sb.String())

	if s.altScreen {
		s.rend.RestoreCursor()
		s.rend.Erase()
		s.rend.SetFullscreen(false)
		s.rend.SetRelativeCursor(true)
		s.altScreen = false
	}
}

func (s *TerminalScreen) AltScreen() bool {
	return s.altScreen
}

func (s *TerminalScreen) HideCursor() {
	s.buf.WriteString(ansi.HideCursor)
	if s.cursor != nil {
		s.cursor.Hidden = true
	}
}

func (s *TerminalScreen) ShowCursor() {
	s.buf.WriteString(ansi.ShowCursor)
	if s.cursor != nil {
		s.cursor.Hidden = false
	} else {
		s.cursor = NewCursor(-1, -1)
	}
}

func (s *TerminalScreen) CursorVisible() bool {
	return s.cursor != nil && !s.cursor.Hidden
}

func (s *TerminalScreen) SetCursorPosition(x, y int) {
	if s.cursor == nil {
		s.cursor = NewCursor(x, y)
		s.cursor.Hidden = true
	} else {
		s.cursor.X = x
		s.cursor.Y = y
	}
}

func (s *TerminalScreen) CursorPosition() (x, y int) {
	if s.cursor != nil {
		return s.cursor.X, s.cursor.Y
	}
	return -1, -1
}

func (s *TerminalScreen) SetCursorStyle(shape CursorShape, blink bool) {
	EncodeCursorStyle(s.buf, shape, blink)
	if s.cursor == nil {
		s.cursor = NewCursor(-1, -1)
	}
	s.cursor.Shape = shape
	s.cursor.Blink = blink
}

func (s *TerminalScreen) CursorStyle() (shape CursorShape, blink bool) {
	if s.cursor != nil {
		return s.cursor.Shape, s.cursor.Blink
	}
	return CursorBlock, true
}

func (s *TerminalScreen) SetCursorColor(c color.Color) {
	EncodeCursorColor(s.buf, c)
	if s.cursor == nil {
		s.cursor = NewCursor(-1, -1)
	}
	s.cursor.Color = c
}

func (s *TerminalScreen) CursorColor() color.Color {
	if s.cursor != nil {
		return s.cursor.Color
	}
	return nil
}

func (s *TerminalScreen) SetBackgroundColor(c color.Color) {
	EncodeBackgroundColor(s.buf, c)
	s.backgroundColor = c
}

func (s *TerminalScreen) BackgroundColor() color.Color {
	return s.backgroundColor
}

func (s *TerminalScreen) SetForegroundColor(c color.Color) {
	EncodeForegroundColor(s.buf, c)
	s.foregroundColor = c
}

func (s *TerminalScreen) ForegroundColor() color.Color {
	return s.foregroundColor
}

func (s *TerminalScreen) EnableBracketedPaste() {
	s.buf.WriteString(ansi.SetModeBracketedPaste)
	s.bracketedPaste = true
}

func (s *TerminalScreen) DisableBracketedPaste() {
	s.buf.WriteString(ansi.ResetModeBracketedPaste)
	s.bracketedPaste = false
}

func (s *TerminalScreen) BracketedPaste() bool {
	return s.bracketedPaste
}

func (s *TerminalScreen) SetSynchronizedUpdates(enabled bool) {
	s.syncUpdates = enabled
}

func (s *TerminalScreen) SynchronizedUpdates() bool {
	return s.syncUpdates
}

func (s *TerminalScreen) SetMouseMode(mode MouseMode) {
	EncodeMouseMode(s.buf, mode)
	s.mouseMode = mode
}

func (s *TerminalScreen) MouseMode() MouseMode {
	return s.mouseMode
}

func (s *TerminalScreen) SetMouseEncoding(enc MouseEncoding) {
	EncodeMouseEncoding(s.buf, enc)
	s.mouseEncoding = enc
}

func (s *TerminalScreen) MouseEncoding() MouseEncoding {
	return s.mouseEncoding
}

func (s *TerminalScreen) SetWindowTitle(title string) {
	EncodeWindowTitle(s.buf, title)
	s.windowTitle = title
}

func (s *TerminalScreen) WindowTitle() string {
	return s.windowTitle
}

func (s *TerminalScreen) SetKeyboardEnhancements(enh *KeyboardEnhancements) {
	EncodeKeyboardEnhancements(s.buf, enh)
	s.keyboardEnhancements = enh
}

func (s *TerminalScreen) KeyboardEnhancements() *KeyboardEnhancements {
	return s.keyboardEnhancements
}

func (s *TerminalScreen) SetProgressBar(pb *ProgressBar) {
	EncodeProgressBar(s.buf, pb)
	s.progressBar = pb
}

func (s *TerminalScreen) ProgressBar() *ProgressBar {
	return s.progressBar
}

func (s *TerminalScreen) Reset() {
	var sb strings.Builder

	hasKeyboardEnhancements := s.keyboardEnhancements != nil

	if s.altScreen {
		if hasKeyboardEnhancements {
			sb.WriteString(ansi.KittyKeyboard(0, 1))
		}
		sb.WriteString(ansi.ResetModeAltScreenSaveCursor)
	}
	if hasKeyboardEnhancements {
		sb.WriteString(ansi.KittyKeyboard(0, 1))
	}
	if s.mouseMode != MouseModeNone {
		EncodeMouseMode(&sb, MouseModeNone)
	}
	if s.mouseEncoding != MouseEncodingLegacy {
		EncodeMouseEncoding(&sb, MouseEncodingLegacy)
	}

	if s.cursor == nil || !s.cursor.Hidden {
		sb.WriteString(ansi.ShowCursor)
	}
	if s.cursor != nil {
		if s.cursor.Shape != CursorBlock || !s.cursor.Blink {
			sb.WriteString(ansi.SetCursorStyle(0))
		}
		if s.cursor.Color != nil {
			sb.WriteString(ansi.ResetCursorColor)
		}
	}
	if s.backgroundColor != nil {
		sb.WriteString(ansi.ResetBackgroundColor)
	}
	if s.foregroundColor != nil {
		sb.WriteString(ansi.ResetForegroundColor)
	}
	if s.bracketedPaste {
		sb.WriteString(ansi.ResetModeBracketedPaste)
	}
	if s.windowTitle != "" {
		sb.WriteString(ansi.SetWindowTitle(""))
	}
	if s.progressBar != nil && s.progressBar.State != ProgressBarNone {
		sb.WriteString(ansi.ResetProgressBar)
	}

	s.buf.WriteString(sb.String())

	s.rend.MoveTo(0, s.win.Height()-1)
}

func (s *TerminalScreen) Restore() {
	var sb strings.Builder

	if s.resetTabs {
		sb.WriteString(ansi.SetTabEvery8Columns)
	}
	if s.altScreen {
		sb.WriteString(ansi.SetModeAltScreenSaveCursor)
	}
	if s.cursor != nil && !s.cursor.Hidden {
		sb.WriteString(ansi.ShowCursor)
	} else {

		sb.WriteString(ansi.HideCursor)
	}
	if s.keyboardEnhancements != nil {
		EncodeKeyboardEnhancements(&sb, s.keyboardEnhancements)
	}
	if s.mouseMode != MouseModeNone {
		EncodeMouseMode(&sb, s.mouseMode)
	}
	if s.mouseEncoding != MouseEncodingLegacy {
		EncodeMouseEncoding(&sb, s.mouseEncoding)
	}
	if s.cursor != nil {
		if s.cursor.Shape != CursorBlock || !s.cursor.Blink {
			EncodeCursorStyle(&sb, s.cursor.Shape, s.cursor.Blink)
		}
		if s.cursor.Color != nil {
			EncodeCursorColor(&sb, s.cursor.Color)
		}
	}
	if s.backgroundColor != nil {
		EncodeBackgroundColor(&sb, s.backgroundColor)
	}
	if s.foregroundColor != nil {
		EncodeForegroundColor(&sb, s.foregroundColor)
	}
	if s.bracketedPaste {
		sb.WriteString(ansi.SetModeBracketedPaste)
	}
	if s.windowTitle != "" {
		EncodeWindowTitle(&sb, s.windowTitle)
	}
	if s.progressBar != nil && s.progressBar.State != ProgressBarNone {
		EncodeProgressBar(&sb, s.progressBar)
	}

	s.buf.WriteString(sb.String())

	s.Render()

}

func (s *TerminalScreen) Write(p []byte) (n int, err error) {
	return s.buf.Write(p)
}

func (s *TerminalScreen) WriteString(str string) (n int, err error) {
	return s.buf.WriteString(str)
}

func (s *TerminalScreen) InsertAbove(content string) error {
	if len(content) == 0 {
		return nil
	}

	var sb strings.Builder
	w, h := s.win.Width(), s.win.Height()
	_, y := s.rend.Position()

	sb.WriteByte('\r')
	down := h - y - 1
	if down > 0 {
		sb.WriteString(ansi.CursorDown(down))
	}

	lines := strings.Split(content, "\n")
	offset := len(lines)
	for _, line := range lines {
		lineWidth := s.win.WidthMethod().StringWidth(line)
		if w > 0 && lineWidth > w {
			offset += (lineWidth / w)
		}
	}

	sb.WriteString(strings.Repeat("\n", offset))

	up := offset + h - 1
	sb.WriteString(ansi.CursorUp(up))
	sb.WriteString(ansi.InsertLine(offset))
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteString(ansi.EraseLineRight)
		sb.WriteString("\r\n")
	}

	s.rend.SetPosition(0, 0)

	_, err := io.WriteString(s.w, sb.String())
	return err
}
