package tea

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"runtime"
	"strings"
	"sync"

	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

type cursedRenderer struct {
	w             io.Writer
	buf           bytes.Buffer
	scr           *uv.TerminalRenderer
	cellbuf       uv.ScreenBuffer
	lastView      *View
	env           []string
	term          string
	width, height int
	mu            sync.Mutex
	profile       colorprofile.Profile
	logger        uv.Logger
	view          View
	hardTabs      bool
	backspace     bool
	mapnl         bool
	syncdUpdates  bool
	starting      bool
	pendingErase  bool
	noInput       bool
}

var _ renderer = &cursedRenderer{}

func newCursedRenderer(w io.Writer, env []string, width, height int) (s *cursedRenderer) {
	s = new(cursedRenderer)
	s.w = w
	s.env = env
	s.term = uv.Environ(env).Getenv("TERM")
	s.width, s.height = width, height
	s.cellbuf = uv.NewScreenBuffer(s.width, s.height)
	reset(s)
	return
}

func (s *cursedRenderer) setLogger(logger uv.Logger) {
	s.mu.Lock()
	s.logger = logger
	s.mu.Unlock()
}

func (s *cursedRenderer) setNoInput(noInput bool) {
	s.noInput = noInput
}

func (s *cursedRenderer) resetKeyboardEnhancements(buf *bytes.Buffer) {
	if s.noInput {
		return
	}
	_, _ = buf.WriteString(ansi.ResetModifyOtherKeys)
	if s.lastView != nil {
		_, _ = buf.WriteString(ansi.PopKittyKeyboard(1))
	}
}

func (s *cursedRenderer) setOptimizations(hardTabs, backspace, mapnl bool) {
	s.mu.Lock()
	s.hardTabs = hardTabs
	s.backspace = backspace
	s.mapnl = mapnl
	if s.hardTabs {
		s.scr.SetTabStops(s.width)
	} else {
		s.scr.SetTabStops(-1)
	}
	s.scr.SetBackspace(s.backspace)
	s.scr.SetMapNewline(s.mapnl)
	s.mu.Unlock()
}

func (s *cursedRenderer) start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.starting = true

	if s.lastView == nil {
		return
	}

	if s.lastView.AltScreen {
		enableAltScreen(s, true, true)
	}
	enableTextCursor(s, s.lastView.Cursor != nil)
	if s.lastView.Cursor != nil {
		if s.lastView.Cursor.Color != nil {
			col, ok := colorful.MakeColor(s.lastView.Cursor.Color)
			if ok {
				_, _ = s.scr.WriteString(ansi.SetCursorColor(col.Hex()))
			}
		}
		curStyle := encodeCursorStyle(s.lastView.Cursor.Shape, s.lastView.Cursor.Blink)
		if curStyle != 0 && curStyle != 1 {
			_, _ = s.scr.WriteString(ansi.SetCursorStyle(curStyle))
		}
	}
	if s.lastView.ForegroundColor != nil {
		col, ok := colorful.MakeColor(s.lastView.ForegroundColor)
		if ok {
			_, _ = s.scr.WriteString(ansi.SetForegroundColor(col.Hex()))
		}
	}
	if s.lastView.BackgroundColor != nil {
		col, ok := colorful.MakeColor(s.lastView.BackgroundColor)
		if ok {
			_, _ = s.scr.WriteString(ansi.SetBackgroundColor(col.Hex()))
		}
	}
	if !s.lastView.DisableBracketedPasteMode {
		_, _ = s.scr.WriteString(ansi.SetModeBracketedPaste)
	}
	if s.lastView.ReportFocus {
		_, _ = s.scr.WriteString(ansi.SetModeFocusEvent)
	}
	switch s.lastView.MouseMode {
	case MouseModeNone:
	case MouseModeCellMotion:
		_, _ = s.scr.WriteString(ansi.SetModeMouseButtonEvent + ansi.SetModeMouseExtSgr)
	case MouseModeAllMotion:
		_, _ = s.scr.WriteString(ansi.SetModeMouseAnyEvent + ansi.SetModeMouseExtSgr)
	}
	if s.lastView.WindowTitle != "" {
		_, _ = s.scr.WriteString(ansi.SetWindowTitle(s.lastView.WindowTitle))
	}
	if s.lastView.ProgressBar != nil {
		setProgressBar(s, s.lastView.ProgressBar)
	}
	if !s.noInput {

		_, _ = s.scr.WriteString(ansi.SetModifyOtherKeys2)

		kittyFlags := keyboardEnhancementsFlags(s.lastView.KeyboardEnhancements)

		_, _ = s.scr.WriteString(ansi.PushKittyKeyboard(kittyFlags))
	}
}

func (s *cursedRenderer) close() (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if lv := s.lastView; lv != nil { //nolint:nestif

		if !s.noInput {
			_, _ = s.buf.WriteString(ansi.ResetModifyOtherKeys)
			_, _ = s.buf.WriteString(ansi.PopKittyKeyboard(1))
		}

		s.scr.MoveTo(0, s.cellbuf.Height()-1)
		_ = s.scr.Flush()
		if lv.AltScreen {
			enableAltScreen(s, false, true)
		} else {
			_, _ = s.scr.WriteString(ansi.EraseScreenBelow)
		}
		if lv.Cursor == nil {
			enableTextCursor(s, true)
		}
		if !lv.DisableBracketedPasteMode {
			_, _ = s.scr.WriteString(ansi.ResetModeBracketedPaste)
		}
		if lv.ReportFocus {
			_, _ = s.scr.WriteString(ansi.ResetModeFocusEvent)
		}
		switch lv.MouseMode {
		case MouseModeNone:
		case MouseModeCellMotion, MouseModeAllMotion:
			_, _ = s.scr.WriteString(ansi.ResetModeMouseButtonEvent +
				ansi.ResetModeMouseAnyEvent +
				ansi.ResetModeMouseExtSgr)
		}

		if lv.WindowTitle != "" {

			_, _ = s.scr.WriteString(ansi.SetWindowTitle(""))
		}
		if lc := lv.Cursor; lc != nil {
			curShape := encodeCursorStyle(lc.Shape, lc.Blink)
			if curShape != 0 && curShape != 1 {

				_, _ = s.scr.WriteString(ansi.SetCursorStyle(0))
			}

			if lc.Color != nil {
				_, _ = s.scr.WriteString(ansi.ResetCursorColor)
			}
		}

		if lv.BackgroundColor != nil {
			_, _ = s.scr.WriteString(ansi.ResetBackgroundColor)
		}
		if lv.ForegroundColor != nil {
			_, _ = s.scr.WriteString(ansi.ResetForegroundColor)
		}
		if lv.ProgressBar != nil && lv.ProgressBar.State != ProgressBarNone {
			_, _ = s.scr.WriteString(ansi.ResetProgressBar)
		}
	}

	if s.cellbuf.Method == ansi.GraphemeWidth {

		_, _ = s.scr.WriteString(ansi.ResetModeUnicodeCore)
	}

	if err := s.scr.Flush(); err != nil {
		return fmt.Errorf("bubbletea: error closing screen writer: %w", err)
	}

	if s.buf.Len() > 0 {
		if s.logger != nil {
			s.logger.Printf("output: %q", s.buf.String())
		}
		if _, err := io.Copy(s.w, &s.buf); err != nil {
			return fmt.Errorf("bubbletea: error writing to screen: %w", err)
		}
		s.buf.Reset()
	}

	x, y := s.scr.Position()

	reset(s)
	s.scr.SetPosition(x, y)

	return nil
}

func (s *cursedRenderer) writeString(str string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.scr.WriteString(str) //nolint:wrapcheck
}

func (s *cursedRenderer) flush(closing bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	view := s.view
	frameArea := uv.Rect(0, 0, s.width, s.height)
	if len(view.Content) == 0 {

		frameArea.Max.Y = 0
	}

	content := uv.NewStyledString(view.Content)
	if !view.AltScreen {

		frameHeight := content.Height()
		if frameHeight != frameArea.Dy() {
			frameArea.Max.Y = frameHeight
		}
	}

	if s.starting && s.hardTabs {
		_, _ = s.scr.WriteString(ansi.SetTabEvery8Columns)
	}

	if !s.starting && !closing && !s.pendingErase && s.lastView != nil && viewEquals(s.lastView, &view) && frameArea == s.cellbuf.Bounds() {

		return nil
	}

	s.starting = false
	s.pendingErase = false

	if frameArea != s.cellbuf.Bounds() {
		s.scr.Erase()

		s.cellbuf.Touched = nil

		s.cellbuf.Resize(frameArea.Dx(), frameArea.Dy())
	}

	s.cellbuf.Clear()
	content.Draw(s.cellbuf, s.cellbuf.Bounds())

	if frameHeight := frameArea.Dy(); frameHeight > s.height {
		s.cellbuf.Lines = s.cellbuf.Lines[frameHeight-s.height:]
	}

	shouldUpdateAltScreen := (s.lastView == nil && view.AltScreen) || (s.lastView != nil && s.lastView.AltScreen != view.AltScreen)
	if shouldUpdateAltScreen {

		enableAltScreen(s, view.AltScreen, false)
	}

	if s.lastView == nil || view.DisableBracketedPasteMode != s.lastView.DisableBracketedPasteMode {
		if !view.DisableBracketedPasteMode {
			_, _ = s.scr.WriteString(ansi.SetModeBracketedPaste)
		} else if s.lastView != nil {
			_, _ = s.scr.WriteString(ansi.ResetModeBracketedPaste)
		}
	}

	if s.lastView == nil || s.lastView.ReportFocus != view.ReportFocus {
		if view.ReportFocus {
			_, _ = s.scr.WriteString(ansi.SetModeFocusEvent)
		} else if s.lastView != nil {
			_, _ = s.scr.WriteString(ansi.ResetModeFocusEvent)
		}
	}

	if s.lastView == nil || view.MouseMode != s.lastView.MouseMode {
		switch view.MouseMode {
		case MouseModeNone:
			if s.lastView != nil && s.lastView.MouseMode != MouseModeNone {
				_, _ = s.scr.WriteString(ansi.ResetModeMouseButtonEvent +
					ansi.ResetModeMouseAnyEvent +
					ansi.ResetModeMouseExtSgr)
			}
		case MouseModeCellMotion:
			if s.lastView != nil && s.lastView.MouseMode == MouseModeAllMotion {
				_, _ = s.scr.WriteString(ansi.ResetModeMouseAnyEvent)
			}
			_, _ = s.scr.WriteString(ansi.SetModeMouseButtonEvent + ansi.SetModeMouseExtSgr)
		case MouseModeAllMotion:
			if s.lastView != nil && s.lastView.MouseMode == MouseModeCellMotion {
				_, _ = s.scr.WriteString(ansi.ResetModeMouseButtonEvent)
			}
			_, _ = s.scr.WriteString(ansi.SetModeMouseAnyEvent + ansi.SetModeMouseExtSgr)
		}
	}

	if s.lastView == nil || view.WindowTitle != s.lastView.WindowTitle {
		if s.lastView != nil || view.WindowTitle != "" {
			_, _ = s.scr.WriteString(ansi.SetWindowTitle(view.WindowTitle))
		}
	}

	if !s.noInput && (s.lastView == nil || view.KeyboardEnhancements != s.lastView.KeyboardEnhancements ||
		view.AltScreen != s.lastView.AltScreen) {

		_, _ = s.scr.WriteString(ansi.SetModifyOtherKeys2)

		kittyFlags := keyboardEnhancementsFlags(view.KeyboardEnhancements)
		if s.lastView == nil || view.AltScreen != s.lastView.AltScreen {

			_, _ = s.scr.WriteString(ansi.PushKittyKeyboard(kittyFlags))
		} else {

			_, _ = s.scr.WriteString(ansi.KittyKeyboard(kittyFlags, 1))
		}
		if !closing {

			_, _ = s.scr.WriteString(ansi.RequestKittyKeyboard)
		}
	}

	var (
		cc, lcc  color.Color
		lfg, lbg color.Color
	)
	if view.Cursor != nil {
		cc = view.Cursor.Color
	}
	if s.lastView != nil {
		if s.lastView.Cursor != nil {
			lcc = s.lastView.Cursor.Color
		}
		lfg = s.lastView.ForegroundColor
		lbg = s.lastView.BackgroundColor
	}
	for _, c := range []struct {
		newColor color.Color
		oldColor color.Color
		reset    string
		setter   func(string) string
	}{
		{newColor: cc, oldColor: lcc, reset: ansi.ResetCursorColor, setter: ansi.SetCursorColor},
		{newColor: view.ForegroundColor, oldColor: lfg, reset: ansi.ResetForegroundColor, setter: ansi.SetForegroundColor},
		{newColor: view.BackgroundColor, oldColor: lbg, reset: ansi.ResetBackgroundColor, setter: ansi.SetBackgroundColor},
	} {
		if c.newColor != c.oldColor {
			if c.newColor == nil {

				_, _ = s.scr.WriteString(c.reset)
			} else {

				col, ok := colorful.MakeColor(c.newColor)
				if ok {
					_, _ = s.scr.WriteString(c.setter(col.Hex()))
				}
			}
		}
	}

	var ccStyle, lcStyle int
	var lcur *Cursor
	ccur := view.Cursor
	if lv := s.lastView; lv != nil {
		lcur = lv.Cursor
	}
	if ccur != nil {
		ccStyle = encodeCursorStyle(ccur.Shape, ccur.Blink)
	}
	if lcur != nil {
		lcStyle = encodeCursorStyle(lcur.Shape, lcur.Blink)
	}
	if ccStyle != lcStyle {
		_, _ = s.scr.WriteString(ansi.SetCursorStyle(ccStyle))
	}

	if (s.lastView == nil && view.ProgressBar != nil && view.ProgressBar.State != ProgressBarNone) ||
		(s.lastView != nil && (s.lastView.ProgressBar == nil) != (view.ProgressBar == nil)) ||
		(s.lastView != nil && s.lastView.ProgressBar != nil && view.ProgressBar != nil && *s.lastView.ProgressBar != *view.ProgressBar) {

		setProgressBar(s, view.ProgressBar)
	}

	s.scr.Render(s.cellbuf.RenderBuffer)

	if cur := view.Cursor; cur != nil {

		s.scr.MoveTo(view.Cursor.X, view.Cursor.Y)
	} else if !view.AltScreen {

		x, y := s.scr.Position()
		if x >= s.width-1 {
			s.scr.MoveTo(0, y)
		}
	}

	if err := s.scr.Flush(); err != nil {
		return fmt.Errorf("bubbletea: error flushing screen writer: %w", err)
	}

	hasUpdates := s.buf.Len() > 0

	didShowCursor := s.lastView != nil && s.lastView.Cursor != nil
	showCursor := view.Cursor != nil
	hideCursor := !showCursor
	shouldUpdateCursorVis := (s.lastView == nil || didShowCursor != showCursor) || shouldUpdateAltScreen

	var buf bytes.Buffer
	if shouldUpdateAltScreen {

		s.resetKeyboardEnhancements(&buf)
		if view.AltScreen {

			buf.WriteString(ansi.SetModeAltScreenSaveCursor)
		} else {

			buf.WriteString(ansi.ResetModeAltScreenSaveCursor)
		}
	}

	if s.syncdUpdates {
		if hasUpdates {

			buf.WriteString(ansi.SetModeSynchronizedOutput)
		}
		if shouldUpdateCursorVis && hideCursor {

			_, _ = buf.WriteString(ansi.ResetModeTextCursorEnable)
		}
	} else if (shouldUpdateCursorVis && hideCursor) || (hasUpdates && showCursor && didShowCursor) {
		_, _ = buf.WriteString(ansi.ResetModeTextCursorEnable)
	}

	if hasUpdates {
		buf.Write(s.buf.Bytes())
	}

	if s.syncdUpdates {
		if shouldUpdateCursorVis && showCursor {

			_, _ = buf.WriteString(ansi.SetModeTextCursorEnable)
		}
		if hasUpdates {

			buf.WriteString(ansi.ResetModeSynchronizedOutput)
		}
	} else if (shouldUpdateCursorVis && showCursor) || (hasUpdates && showCursor && didShowCursor) {
		_, _ = buf.WriteString(ansi.SetModeTextCursorEnable)
	}

	s.buf.Reset()

	if buf.Len() > 0 {
		if s.logger != nil {
			s.logger.Printf("output: %q", buf.String())
		}
		if _, err := io.Copy(s.w, &buf); err != nil {
			return fmt.Errorf("bubbletea: error flushing update to the writer: %w", err)
		}
	}

	s.lastView = &view

	return nil
}

func (s *cursedRenderer) render(v View) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.view = v
}

func (s *cursedRenderer) reset() {
	s.mu.Lock()
	reset(s)
	s.mu.Unlock()
}

func reset(s *cursedRenderer) {
	s.buf.Reset()
	scr := uv.NewTerminalRenderer(&s.buf, s.env)
	scr.SetColorProfile(s.profile)
	scr.SetRelativeCursor(true)
	scr.SetFullscreen(false)
	if s.hardTabs {
		scr.SetTabStops(s.width)
	} else {
		scr.SetTabStops(-1)
	}
	scr.SetBackspace(s.backspace)
	scr.SetMapNewline(s.mapnl)
	scr.SetScrollOptim(runtime.GOOS != "windows")
	s.scr = scr
}

func (s *cursedRenderer) setColorProfile(p colorprofile.Profile) {
	s.mu.Lock()
	s.profile = p
	s.scr.SetColorProfile(p)
	s.mu.Unlock()
}

func (s *cursedRenderer) resize(w, h int) {
	s.mu.Lock()

	s.scr.Erase()
	s.width, s.height = w, h
	s.scr.Resize(s.width, s.height)
	s.pendingErase = true
	s.mu.Unlock()
}

func (s *cursedRenderer) clearScreen() {
	s.mu.Lock()

	s.scr.MoveTo(0, 0)
	s.scr.Erase()
	s.pendingErase = true
	s.mu.Unlock()
}

func enableAltScreen(s *cursedRenderer, enable bool, write bool) {
	if enable {
		enterAltScreen(s, write)
	} else {
		exitAltScreen(s, write)
	}
}

func enterAltScreen(s *cursedRenderer, write bool) {
	s.scr.SaveCursor()
	if write {
		s.buf.WriteString(ansi.SetModeAltScreenSaveCursor)
	}
	s.scr.SetFullscreen(true)
	s.scr.SetRelativeCursor(false)
	s.scr.Erase()
}

func exitAltScreen(s *cursedRenderer, write bool) {
	s.scr.Erase()
	s.scr.SetRelativeCursor(true)
	s.scr.SetFullscreen(false)
	if write {
		s.buf.WriteString(ansi.ResetModeAltScreenSaveCursor)
	}
	s.scr.RestoreCursor()
}

func enableTextCursor(s *cursedRenderer, enable bool) {
	if enable {
		_, _ = s.scr.WriteString(ansi.SetModeTextCursorEnable)
	} else {
		_, _ = s.scr.WriteString(ansi.ResetModeTextCursorEnable)
	}
}

func (s *cursedRenderer) setSyncdUpdates(syncd bool) {
	s.mu.Lock()
	s.syncdUpdates = syncd
	s.mu.Unlock()
}

func (s *cursedRenderer) setWidthMethod(method ansi.Method) {
	s.mu.Lock()
	if method == ansi.GraphemeWidth {

		_, _ = s.scr.WriteString(ansi.SetModeUnicodeCore)
	} else if s.cellbuf.Method == ansi.GraphemeWidth {

		_, _ = s.scr.WriteString(ansi.ResetModeUnicodeCore)
	}
	s.cellbuf.Method = method
	s.mu.Unlock()
}

func (s *cursedRenderer) insertAbove(str string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(str) == 0 {
		return nil
	}

	var sb strings.Builder
	w, h := s.cellbuf.Width(), s.cellbuf.Height()
	_, y := s.scr.Position()

	sb.WriteByte('\r')
	down := h - y - 1
	if down > 0 {
		sb.WriteString(ansi.CursorDown(down))
	}

	lines := strings.Split(str, "\n")
	offset := len(lines)
	for _, line := range lines {
		lineWidth := ansi.StringWidth(line)
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

	s.scr.SetPosition(0, 0)

	if s.logger != nil {
		s.logger.Printf("insert above: %q", sb.String())
	}

	_, err := io.WriteString(s.w, sb.String())
	if err != nil {
		return fmt.Errorf("bubbletea: error writing insert above to the writer: %w", err)
	}

	return nil
}

func (s *cursedRenderer) onMouse(m MouseMsg) Cmd {
	var onMouse func(MouseMsg) Cmd
	s.mu.Lock()
	if s.lastView != nil {
		onMouse = s.lastView.OnMouse
	}
	s.mu.Unlock()
	if onMouse != nil {
		return onMouse(m)
	}
	return nil
}

func setProgressBar(s *cursedRenderer, pb *ProgressBar) {
	if pb == nil {
		_, _ = s.scr.WriteString(ansi.ResetProgressBar)
		return
	}

	var seq string
	switch pb.State {
	case ProgressBarNone:
		seq = ansi.ResetProgressBar
	case ProgressBarDefault:
		seq = ansi.SetProgressBar(pb.Value)
	case ProgressBarError:
		seq = ansi.SetErrorProgressBar(pb.Value)
	case ProgressBarIndeterminate:
		seq = ansi.SetIndeterminateProgressBar
	case ProgressBarWarning:
		seq = ansi.SetWarningProgressBar(pb.Value)
	}
	if seq != "" {
		_, _ = s.scr.WriteString(seq)
	}
}

func viewEquals(a, b *View) bool {
	if a == nil || b == nil {
		return false
	}

	if a.Content != b.Content ||
		a.AltScreen != b.AltScreen ||
		a.DisableBracketedPasteMode != b.DisableBracketedPasteMode ||
		a.ReportFocus != b.ReportFocus ||
		a.MouseMode != b.MouseMode ||
		a.WindowTitle != b.WindowTitle ||
		a.ForegroundColor != b.ForegroundColor ||
		a.BackgroundColor != b.BackgroundColor ||
		a.KeyboardEnhancements != b.KeyboardEnhancements {
		return false
	}

	if (a.Cursor == nil) != (b.Cursor == nil) {
		return false
	}
	if a.Cursor != nil && b.Cursor != nil {
		if a.Cursor.X != b.Cursor.X ||
			a.Cursor.Y != b.Cursor.Y ||
			a.Cursor.Shape != b.Cursor.Shape ||
			a.Cursor.Blink != b.Cursor.Blink ||
			a.Cursor.Color != b.Cursor.Color {
			return false
		}
	}

	if (a.ProgressBar == nil) != (b.ProgressBar == nil) {
		return false
	}
	if a.ProgressBar != nil && b.ProgressBar != nil {
		if *a.ProgressBar != *b.ProgressBar {
			return false
		}
	}

	return true
}

func keyboardEnhancementsFlags(ke KeyboardEnhancements) int {
	flags := 1
	if ke.ReportEventTypes {
		flags |= ansi.KittyReportEventTypes
	}
	if ke.ReportAlternateKeys {
		flags |= ansi.KittyReportAlternateKeys
	}
	if ke.ReportAllKeysAsEscapeCodes {
		flags |= ansi.KittyReportAllKeysAsEscapeCodes
	}
	if ke.ReportAssociatedText {
		flags |= ansi.KittyReportAssociatedKeys
	}
	return flags
}
