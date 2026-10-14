package uv

import (
	"bytes"
	"errors"
	"hash/maphash"
	"io"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

var ErrInvalidDimensions = errors.New("invalid dimensions")

type capabilities uint

const (
	capVPA capabilities = 1 << iota

	capHPA

	capCHA

	capCHT

	capCBT

	capREP

	capECH

	capICH

	capSD

	capSU

	capHT

	capBS

	noCaps  capabilities = 0
	allCaps              = capVPA | capHPA | capCHA | capCHT | capCBT | capREP | capECH | capICH | capSD | capSU
)

func (v *capabilities) Set(c capabilities) {
	*v |= c
}

func (v *capabilities) Reset(c capabilities) {
	*v &^= c
}

func (v capabilities) Contains(c capabilities) bool {
	return v&c == c
}

type cursor struct {
	Cell
	Position
}

type LineData struct {
	FirstCell, LastCell int

	oldIndex int
}

type tFlag uint

const (
	tRelativeCursor tFlag = 1 << iota
	tFullscreen
	tMapNewline
	tScrollOptim

	tGraphemeWidth
)

func (v *tFlag) Set(c tFlag) {
	*v |= c
}

func (v *tFlag) Reset(c tFlag) {
	*v &^= c
}

func (v tFlag) Contains(c tFlag) bool {
	return v&c == c
}

type TerminalRenderer struct {
	w                io.Writer
	buf              *bytes.Buffer
	curbuf           *RenderBuffer
	tabs             *TabStops
	hasher           maphash.Hash
	oldhash, newhash []uint64
	hashtab          []hashmap
	oldnum           []int
	cur, saved       cursor
	flags            tFlag
	method           ansi.Method
	term             string
	clear            bool
	caps             capabilities
	atPhantom        bool
	lineHadWide      bool
	logger           Logger

	profile colorprofile.Profile
}

func NewTerminalRenderer(w io.Writer, env []string) (s *TerminalRenderer) {
	s = new(TerminalRenderer)
	s.w = w
	s.profile = colorprofile.Detect(w, env)
	s.buf = new(bytes.Buffer)
	s.term = Environ(env).Getenv("TERM")
	s.caps = xtermCaps(s.term)
	s.cur = cursor{Cell: EmptyCell, Position: Pos(-1, -1)}
	s.saved = s.cur
	s.oldhash, s.newhash = nil, nil
	return
}

func (s *TerminalRenderer) SetLogger(logger Logger) {
	s.logger = logger
}

func (s *TerminalRenderer) SetColorProfile(profile colorprofile.Profile) {
	s.profile = profile
}

func (s *TerminalRenderer) SetScrollOptim(v bool) {
	if v {
		s.flags.Set(tScrollOptim)
	} else {
		s.flags.Reset(tScrollOptim)
	}
}

func (s *TerminalRenderer) SetMapNewline(v bool) {
	if v {
		s.flags.Set(tMapNewline)
	} else {
		s.flags.Reset(tMapNewline)
	}
}

func (s *TerminalRenderer) SetWidthMethod(method ansi.Method) {
	s.method = method
}

func (s *TerminalRenderer) SetGraphemeWidth(v bool) {
	if v {
		s.flags.Set(tGraphemeWidth)
		s.method = ansi.GraphemeWidth
	} else {
		s.flags.Reset(tGraphemeWidth)
		s.method = ansi.WcWidth
	}
}

func (s *TerminalRenderer) SetBackspace(v bool) {
	if v {
		s.caps.Set(capBS)
	} else {
		s.caps.Reset(capBS)
	}
}

func (s *TerminalRenderer) SetTabStops(width int) {
	if width < 0 || strings.HasPrefix(s.term, "linux") {

		s.caps.Reset(capHT)
	} else {
		s.caps.Set(capHT)
		s.tabs = DefaultTabStops(width)
	}
}

func (s *TerminalRenderer) SetFullscreen(v bool) {
	if v {
		s.flags.Set(tFullscreen)
	} else {
		s.flags.Reset(tFullscreen)
	}
}

func (s *TerminalRenderer) Fullscreen() bool {
	return s.flags.Contains(tFullscreen)
}

func (s *TerminalRenderer) SetRelativeCursor(v bool) {
	if v {
		s.flags.Set(tRelativeCursor)
	} else {
		s.flags.Reset(tRelativeCursor)
	}
}

func (s *TerminalRenderer) SaveCursor() {
	s.saved = s.cur
}

func (s *TerminalRenderer) RestoreCursor() {
	s.cur = s.saved
}

func (s *TerminalRenderer) EnterAltScreen() {
	s.SaveCursor()
	s.buf.WriteString(ansi.SetModeAltScreenSaveCursor)
	s.SetFullscreen(true)
	s.SetRelativeCursor(false)
	s.Erase()
}

func (s *TerminalRenderer) ExitAltScreen() {
	s.Erase()
	s.SetRelativeCursor(true)
	s.SetFullscreen(false)
	s.buf.WriteString(ansi.ResetModeAltScreenSaveCursor)
	s.RestoreCursor()
}

func (s *TerminalRenderer) PrependString(newbuf *RenderBuffer, str string) {
	if len(str) == 0 {
		return
	}

	w, h := newbuf.Width(), newbuf.Height()
	s.move(newbuf, 0, h-1)
	lines := strings.Split(str, "\n")
	offset := 0
	for _, line := range lines {
		lineWidth := s.method.StringWidth(line)
		if w > 0 && lineWidth > w {
			offset += (lineWidth / w)
		}
		if lineWidth == 0 || lineWidth%w != 0 {
			offset++
		}
	}

	s.buf.WriteString(strings.Repeat("\n", offset))
	s.cur.Y += offset

	s.moveCursor(newbuf, 0, 0, false)
	s.buf.WriteString(ansi.InsertLine(offset))
	for _, line := range lines {
		s.buf.WriteString(line)
		s.buf.WriteString("\r\n")
	}
}

func (s *TerminalRenderer) moveCursor(newbuf *RenderBuffer, x, y int, overwrite bool) {
	if !s.flags.Contains(tFullscreen) && s.flags.Contains(tRelativeCursor) &&
		s.cur.X == -1 && s.cur.Y == -1 {

		_ = s.buf.WriteByte('\r')
		s.cur.X, s.cur.Y = 0, 0
	}
	seq := moveCursor(s, newbuf, x, y, overwrite)
	_, _ = s.buf.WriteString(seq)
	s.cur.X, s.cur.Y = x, y
}

func (s *TerminalRenderer) move(newbuf *RenderBuffer, x, y int) {

	var width, height int
	if s.curbuf != nil {
		width, height = s.curbuf.Width(), s.curbuf.Height()
	}
	if newbuf != nil {
		width = max(newbuf.Width(), width)
		height = max(newbuf.Height(), height)
	}

	if width > 0 && x >= width {

		y += (x / width)
		x %= width
	}

	blank := s.clearBlank()
	resetPen := y != s.cur.Y && !blank.Equal(&EmptyCell)
	if resetPen {
		s.updatePen(nil)
	}

	if s.atPhantom {
		s.cur.X = 0
		_ = s.buf.WriteByte('\r')
		s.atPhantom = false
	}

	if height > 0 {
		if s.cur.Y > height-1 {
			s.cur.Y = height - 1
		}
		if y > height-1 {
			y = height - 1
		}
	}

	if x == s.cur.X && y == s.cur.Y {

		return
	}

	s.moveCursor(newbuf, x, y, true)
}

func cellEqual(a, b *Cell) bool {
	if a == b {
		return true
	}

	if a == nil || b == nil {
		return false
	}
	return a.Equal(b)
}

func (s *TerminalRenderer) putCell(newbuf *RenderBuffer, cell *Cell) {
	width, height := newbuf.Width(), newbuf.Height()
	if s.flags.Contains(tFullscreen) && s.cur.X == width-1 && s.cur.Y == height-1 {
		s.putCellLR(newbuf, cell)
	} else {
		s.putAttrCell(newbuf, cell)
	}
}

func (s *TerminalRenderer) wrapCursor() {
	const autoRightMargin = true
	if autoRightMargin {

		s.cur.X = 0
		s.cur.Y++
	} else {
		s.cur.X--
	}
}

func (s *TerminalRenderer) putAttrCell(newbuf *RenderBuffer, cell *Cell) {
	if cell != nil && cell.Width == 0 {

		return
	}

	if s.atPhantom {
		s.wrapCursor()
		s.atPhantom = false
	}

	s.updatePen(cell)
	cellWidth := 1
	if cell == nil {
		_ = s.buf.WriteByte(' ')
	} else {
		_, _ = s.buf.WriteString(cell.Content)
		cellWidth = cell.Width
	}

	s.cur.X += cellWidth
	if s.cur.X >= newbuf.Width() {
		s.atPhantom = true
	}

	if cellWidth > 1 {
		s.lineHadWide = true
	}
}

func (s *TerminalRenderer) putCellLR(newbuf *RenderBuffer, cell *Cell) {

	curX := s.cur.X
	if !cell.isWidePlaceholder() {
		_, _ = s.buf.WriteString(ansi.ResetModeAutoWrap)
		s.putAttrCell(newbuf, cell)

		s.atPhantom = false
		s.cur.X = curX
		_, _ = s.buf.WriteString(ansi.SetModeAutoWrap)
	}
}

func (s *TerminalRenderer) updatePen(cell *Cell) {
	if cell == nil {
		if !s.cur.Style.IsZero() {
			_, _ = s.buf.WriteString(ansi.ResetStyle)
			s.cur.Style = Style{}
		}
		if !s.cur.Link.IsZero() {
			_, _ = s.buf.WriteString(ansi.ResetHyperlink())
		}
		return
	}

	newStyle := ConvertStyle(cell.Style, s.profile)
	newLink := ConvertLink(cell.Link, s.profile)
	oldStyle := ConvertStyle(s.cur.Style, s.profile)
	oldLink := ConvertLink(s.cur.Link, s.profile)

	if !newStyle.Equal(&oldStyle) {
		seq := newStyle.Diff(&oldStyle)
		if newStyle.IsZero() && len(seq) > len(ansi.ResetStyle) {
			seq = ansi.ResetStyle
		}
		_, _ = s.buf.WriteString(seq)
		s.cur.Style = cell.Style
	}
	if !newLink.Equal(&oldLink) {
		_, _ = s.buf.WriteString(ansi.SetHyperlink(newLink.URL, newLink.Params))
		s.cur.Link = cell.Link
	}
}

func canClearWith(c *Cell) bool {
	if c == nil {
		return true
	}
	if c.Width != 1 || len(c.Content) != 1 || c.Content != " " {
		return false
	}

	return c.Style.Underline == UnderlineNone &&
		c.Style.Attrs&^(AttrBold|AttrFaint|AttrItalic|AttrBlink|AttrRapidBlink) == 0 &&
		c.Link.IsZero()
}

func (s *TerminalRenderer) emitRange(newbuf *RenderBuffer, line Line, n int) (eoi bool) {
	hasECH := s.caps.Contains(capECH)
	hasREP := s.caps.Contains(capREP)
	if hasECH || hasREP {
		for n > 0 {
			var count int
			for n > 1 && !cellEqual(line.At(0), line.At(1)) {
				s.putCell(newbuf, line.At(0))
				line = line[1:]
				n--
			}

			cell0 := line[0]
			if n == 1 {
				s.putCell(newbuf, &cell0)
				return false
			}

			count = 2
			for count < n && cellEqual(line.At(count), &cell0) {
				count++
			}

			ech := ansi.EraseCharacter(count)
			cup := ansi.CursorPosition(s.cur.X+count, s.cur.Y)
			rep := ansi.RepeatPreviousCharacter(count)
			if hasECH && count > len(ech)+len(cup) && canClearWith(&cell0) {
				s.updatePen(&cell0)
				_, _ = s.buf.WriteString(ech)

				if count < n {
					s.move(newbuf, s.cur.X+count, s.cur.Y)
				} else {
					return true
				}
			} else if hasREP && count > len(rep) &&
				(len(cell0.Content) == 1 && cell0.Content[0] >= ansi.US && cell0.Content[0] < ansi.DEL) {

				wrapPossible := s.cur.X+count >= newbuf.Width()
				repCount := count
				if wrapPossible {
					repCount--
				}

				s.updatePen(&cell0)
				s.putCell(newbuf, &cell0)
				repCount--

				_, _ = s.buf.WriteString(ansi.RepeatPreviousCharacter(repCount))
				s.cur.X += repCount
				if wrapPossible {
					s.putCell(newbuf, &cell0)
				}
			} else {
				for i := 0; i < count; i++ {
					s.putCell(newbuf, line.At(i))
				}
			}

			line = line[clamp(count, 0, len(line)):]
			n -= count
		}

		return false
	}

	for i := 0; i < n; i++ {
		s.putCell(newbuf, line.At(i))
	}

	return false
}

func (s *TerminalRenderer) putRange(newbuf *RenderBuffer, oldLine, newLine Line, y, start, end int) (eoi bool) {
	inline := min(len(ansi.CursorPosition(start+1, y+1)),
		min(len(ansi.HorizontalPositionAbsolute(start+1)),
			len(ansi.CursorForward(start+1))))
	if (end - start + 1) > inline {
		var j, same int
		for j, same = start, 0; j <= end; j++ {
			oldCell, newCell := oldLine.At(j), newLine.At(j)
			if same == 0 && oldCell.isWidePlaceholder() && newCell.isWidePlaceholder() {
				continue
			}
			if cellEqual(oldCell, newCell) {
				same++
			} else {
				if same > end-start {
					s.emitRange(newbuf, newLine[start:], j-same-start)
					s.move(newbuf, j, y)
					start = j
				}
				same = 0
			}
		}

		i := s.emitRange(newbuf, newLine[start:], j-same-start)

		if same == 0 {
			return i
		}
		return true
	}

	return s.emitRange(newbuf, newLine[start:], end-start+1)
}

func (s *TerminalRenderer) clearToEnd(newbuf *RenderBuffer, blank *Cell, force bool) {
	if s.cur.Y >= 0 {
		curline := s.curbuf.Line(s.cur.Y)

		for j := s.cur.X; j < newbuf.Width(); j++ {
			if j >= 0 {
				c := curline.At(j)
				if !cellEqual(c, blank) {
					curline.Set(j, blank)
					force = true
				}
			}
		}
	}

	if force {
		s.updatePen(blank)
		count := newbuf.Width() - s.cur.X
		if s.el0Cost() <= count {
			_, _ = s.buf.WriteString(ansi.EraseLineRight)
		} else {
			for i := 0; i < count; i++ {
				s.putCell(newbuf, blank)
			}
		}
	}
}

func (s *TerminalRenderer) clearBlank() *Cell {
	return &s.cur.Cell
}

func (s *TerminalRenderer) insertCells(newbuf *RenderBuffer, line Line, count int) {
	supportsICH := s.caps.Contains(capICH)
	if supportsICH {

		_, _ = s.buf.WriteString(ansi.InsertCharacter(count))
	} else {

		_, _ = s.buf.WriteString(ansi.SetModeInsertReplace)
	}

	for i := 0; count > 0; i++ {
		s.putAttrCell(newbuf, line.At(i))
		count--
	}

	if !supportsICH {
		_, _ = s.buf.WriteString(ansi.ResetModeInsertReplace)
	}
}

func (s *TerminalRenderer) el0Cost() int {
	if s.caps != noCaps {
		return 0
	}
	return len(ansi.EraseLineRight)
}

func lineHasDrift(m ansi.Method, line Line) bool {
	for i := 0; i < len(line); i++ {
		c := line.At(i)
		if c == nil || c.Width == 0 || len(c.Content) == 0 {
			continue
		}
		if c.Width > 1 || m.StringWidth(c.Content) != ansi.StringWidth(c.Content) {
			return true
		}
	}
	return false
}

func (s *TerminalRenderer) repaintLine(newbuf *RenderBuffer, y int) {
	oldLine := s.curbuf.Line(y)
	newLine := newbuf.Line(y)

	s.move(newbuf, 0, y)
	blank := s.clearBlank()
	s.updatePen(blank)
	_, _ = s.buf.WriteString(ansi.EraseLineRight)
	s.cur.X = 0

	last := -1
	for x := 0; x < newbuf.Width(); x++ {
		if c := newLine.At(x); c != nil && !c.isWidePlaceholder() && !cellEqual(c, blank) {
			last = x
		}
	}
	for x := 0; x <= last; x++ {
		s.putCell(newbuf, newLine.At(x))
	}

	if len(oldLine) == len(newLine) {
		copy(oldLine, newLine)
	}
}

func (s *TerminalRenderer) transformLine(newbuf *RenderBuffer, y int) {
	var firstCell, oLastCell, nLastCell int
	oldLine := s.curbuf.Line(y)
	newLine := newbuf.Line(y)

	s.lineHadWide = false
	defer s.reanchorWideLine(newbuf)

	if lineHasDrift(s.method, oldLine) || lineHasDrift(s.method, newLine) {
		s.repaintLine(newbuf, y)
		return
	}

	blank := newLine.At(0)

	if canClearWith(blank) {
		var oFirstCell, nFirstCell int
		for oFirstCell = 0; oFirstCell < s.curbuf.Width(); oFirstCell++ {
			if !cellEqual(oldLine.At(oFirstCell), blank) {
				break
			}
		}
		for nFirstCell = 0; nFirstCell < newbuf.Width(); nFirstCell++ {
			if !cellEqual(newLine.At(nFirstCell), blank) {
				break
			}
		}

		if nFirstCell == oFirstCell {
			firstCell = nFirstCell

			for firstCell < newbuf.Width() &&
				cellEqual(oldLine.At(firstCell), newLine.At(firstCell)) {
				firstCell++
			}
		} else if oFirstCell > nFirstCell {
			firstCell = nFirstCell
		} else if oFirstCell < nFirstCell {
			firstCell = oFirstCell
			el1Cost := len(ansi.EraseLineLeft)
			if el1Cost < nFirstCell-oFirstCell {
				if nFirstCell >= newbuf.Width() {
					s.move(newbuf, 0, y)
					s.updatePen(blank)
					_, _ = s.buf.WriteString(ansi.EraseLineRight)
				} else {
					s.move(newbuf, nFirstCell-1, y)
					s.updatePen(blank)
					_, _ = s.buf.WriteString(ansi.EraseLineLeft)
				}

				for firstCell < nFirstCell {
					oldLine.Set(firstCell, blank)
					firstCell++
				}
			}
		}
	} else {

		for firstCell < newbuf.Width() && cellEqual(newLine.At(firstCell), oldLine.At(firstCell)) {
			firstCell++
		}
	}

	if firstCell >= newbuf.Width() {
		return
	}

	blank = newLine.At(newbuf.Width() - 1)
	if blank != nil && !canClearWith(blank) {

		nLastCell = newbuf.Width() - 1
		for nLastCell > firstCell && cellEqual(newLine.At(nLastCell), oldLine.At(nLastCell)) {
			nLastCell--
		}

		if nLastCell >= firstCell {
			s.move(newbuf, firstCell, y)
			s.putRange(newbuf, oldLine, newLine, y, firstCell, nLastCell)
			if firstCell < len(oldLine) && firstCell < len(newLine) {
				copy(oldLine[firstCell:], newLine[firstCell:])
			} else {
				copy(oldLine, newLine)
			}
		}

		return
	}

	oLastCell = newbuf.Width() - 1
	for oLastCell > firstCell && cellEqual(oldLine.At(oLastCell), blank) {
		oLastCell--
	}

	nLastCell = newbuf.Width() - 1
	for nLastCell > firstCell && cellEqual(newLine.At(nLastCell), blank) {
		nLastCell--
	}

	if nLastCell == firstCell && s.el0Cost() < oLastCell-nLastCell {
		s.move(newbuf, firstCell, y)
		if !cellEqual(newLine.At(firstCell), blank) {
			s.putCell(newbuf, newLine.At(firstCell))
		}
		s.clearToEnd(newbuf, blank, false)
	} else if nLastCell != oLastCell &&
		!cellEqual(newLine.At(nLastCell), oldLine.At(oLastCell)) {
		s.move(newbuf, firstCell, y)
		if oLastCell-nLastCell > s.el0Cost() {
			if s.putRange(newbuf, oldLine, newLine, y, firstCell, nLastCell) {
				s.move(newbuf, nLastCell+1, y)
			}
			s.clearToEnd(newbuf, blank, false)
		} else {
			n := max(nLastCell, oLastCell)
			s.putRange(newbuf, oldLine, newLine, y, firstCell, n)
		}
	} else {
		nLastNonBlank := nLastCell
		oLastNonBlank := oLastCell

		for cellEqual(newLine.At(nLastCell), oldLine.At(oLastCell)) {
			if !cellEqual(newLine.At(nLastCell-1), oldLine.At(oLastCell-1)) {
				break
			}
			nLastCell--
			oLastCell--
			if nLastCell == -1 || oLastCell == -1 {
				break
			}
		}

		n := min(oLastCell, nLastCell)
		if n >= firstCell {
			s.move(newbuf, firstCell, y)
			s.putRange(newbuf, oldLine, newLine, y, firstCell, n)
		}

		if oLastCell < nLastCell {
			m := max(nLastNonBlank, oLastNonBlank)
			if n != 0 {
				for n > 0 {
					wide := newLine.At(n + 1)
					if !wide.isWidePlaceholder() {
						break
					}
					n--
					oLastCell--
				}
			} else if n >= firstCell && newLine.At(n) != nil && newLine.At(n).Width > 1 {
				next := newLine.At(n + 1)
				for next.isWidePlaceholder() {
					n++
					oLastCell++
					next = newLine.At(n + 1)
				}
			}

			s.move(newbuf, n+1, y)
			ichCost := 3 + nLastCell - oLastCell
			if s.caps.Contains(capICH) && (nLastCell < nLastNonBlank || ichCost > (m-n)) {
				s.putRange(newbuf, oldLine, newLine, y, n+1, m)
			} else {
				s.insertCells(newbuf, newLine[n+1:], nLastCell-oLastCell)
			}
		} else if oLastCell > nLastCell {
			s.move(newbuf, n+1, y)
			dchCost := 3 + oLastCell - nLastCell
			if dchCost > len(ansi.EraseLineRight)+nLastNonBlank-(n+1) {
				if s.putRange(newbuf, oldLine, newLine, y, n+1, nLastNonBlank) {
					s.move(newbuf, nLastNonBlank+1, y)
				}
				s.clearToEnd(newbuf, blank, false)
			} else {
				s.updatePen(blank)
				s.deleteCells(oLastCell - nLastCell)
			}
		}
	}

	if firstCell < len(oldLine) && firstCell < len(newLine) {
		copy(oldLine[firstCell:], newLine[firstCell:])
	} else {
		copy(oldLine, newLine)
	}
}

func (s *TerminalRenderer) reanchorWideLine(newbuf *RenderBuffer) {
	if !s.lineHadWide || s.flags.Contains(tGraphemeWidth) {
		return
	}
	s.lineHadWide = false
	if s.atPhantom || s.cur.X < 0 || s.cur.X >= newbuf.Width() {
		return
	}
	_, _ = s.buf.WriteString(ansi.CursorHorizontalAbsolute(s.cur.X + 1))
}

func (s *TerminalRenderer) deleteCells(count int) {

	_, _ = s.buf.WriteString(ansi.DeleteCharacter(count))
}

func (s *TerminalRenderer) clearToBottom(blank *Cell) {
	row, col := s.cur.Y, s.cur.X
	if row < 0 {
		row = 0
	}

	s.updatePen(blank)
	_, _ = s.buf.WriteString(ansi.EraseScreenBelow)

	s.curbuf.ClearArea(Rect(col, row, s.curbuf.Width()-col, 1))

	s.curbuf.ClearArea(Rect(0, row+1, s.curbuf.Width(), s.curbuf.Height()-row-1))
}

func (s *TerminalRenderer) clearBottom(newbuf *RenderBuffer, total int) (top int) {
	if total <= 0 {
		return 0
	}

	top = total
	last := min(s.curbuf.Width(), newbuf.Width())
	blank := s.clearBlank()
	canClearWithBlank := canClearWith(blank)

	if canClearWithBlank {
		var row int
		for row = total - 1; row >= 0; row-- {
			oldLine := s.curbuf.Line(row)
			newLine := newbuf.Line(row)

			var col int
			ok := true
			for col = 0; ok && col < last; col++ {
				ok = cellEqual(newLine.At(col), blank)
			}
			if !ok {
				break
			}

			for col = 0; ok && col < last; col++ {
				ok = cellEqual(oldLine.At(col), blank)
			}
			if !ok {
				top = row
			}
		}

		if top < total {
			s.move(newbuf, 0, max(0, top-1))
			s.clearToBottom(blank)
			if s.oldhash != nil && s.newhash != nil &&
				row < len(s.oldhash) && row < len(s.newhash) {
				for row := top; row < newbuf.Height(); row++ {
					s.oldhash[row] = s.newhash[row]
				}
			}
		}
	}

	return top
}

func (s *TerminalRenderer) clearScreen(blank *Cell) {
	s.updatePen(blank)
	_, _ = s.buf.WriteString(ansi.CursorHomePosition)
	_, _ = s.buf.WriteString(ansi.EraseEntireScreen)
	s.cur.X, s.cur.Y = 0, 0
	s.curbuf.Fill(blank)
}

func (s *TerminalRenderer) clearBelow(newbuf *RenderBuffer, blank *Cell, row int) {
	s.move(newbuf, 0, row)
	s.clearToBottom(blank)
}

func (s *TerminalRenderer) clearUpdate(newbuf *RenderBuffer) {
	blank := s.clearBlank()
	var nonEmpty int
	if s.flags.Contains(tFullscreen) {

		nonEmpty = max(s.curbuf.Height(), newbuf.Height())
		s.clearScreen(blank)
	} else {
		nonEmpty = newbuf.Height()

		s.clearBelow(newbuf, blank, 0)
	}
	nonEmpty = s.clearBottom(newbuf, nonEmpty)
	for i := 0; i < nonEmpty && i < newbuf.Height(); i++ {
		s.transformLine(newbuf, i)
	}
}

func (s *TerminalRenderer) logf(format string, args ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Printf(format, args...)
}

func (s *TerminalRenderer) Buffered() int {
	return s.buf.Len()
}

func (s *TerminalRenderer) Flush() (err error) {

	if n := s.buf.Len(); n > 0 {
		bts := s.buf.Bytes()
		if s.logger != nil {
			s.logf("output: %q", bts)
		}
		_, err = s.w.Write(bts)
		s.buf.Reset()
	}
	return
}

func (s *TerminalRenderer) Redraw(newbuf *RenderBuffer) {
	s.clear = true
	s.Render(newbuf)
}

func (s *TerminalRenderer) Render(newbuf *RenderBuffer) {

	touchedLines := newbuf.TouchedLines()
	if !s.clear && touchedLines == 0 {
		return
	}

	if s.curbuf == nil || s.curbuf.Bounds().Empty() {

		s.curbuf = NewRenderBuffer(newbuf.Width(), newbuf.Height())
	}

	newWidth, newHeight := newbuf.Width(), newbuf.Height()
	curWidth, curHeight := s.curbuf.Width(), s.curbuf.Height()

	if curWidth != newWidth || curHeight != newHeight {
		s.oldhash, s.newhash = nil, nil

		if s.flags.Contains(tFullscreen) && (newWidth < curWidth || newHeight < curHeight) {
			s.clear = true
		}
	}

	var nonEmpty int

	partialClear := !s.flags.Contains(tFullscreen) && s.cur.X != -1 && s.cur.Y != -1 &&
		curWidth == newWidth &&
		curHeight > 0 &&
		curHeight > newHeight

	if !s.clear && partialClear {
		s.clearBelow(newbuf, nil, newHeight-1)
	}

	if curWidth != newWidth || curHeight != newHeight {
		s.curbuf.Resize(newWidth, newHeight)
	}

	if s.clear {
		s.clearUpdate(newbuf)
		s.clear = false
	} else if touchedLines > 0 {

		if s.flags.Contains(tScrollOptim) && s.flags.Contains(tFullscreen) {

			s.scrollOptimize(newbuf)
		}

		var changedLines int
		var i int

		nonEmpty = newHeight

		nonEmpty = s.clearBottom(newbuf, nonEmpty)
		for i = 0; i < nonEmpty; i++ {
			if newbuf.Touched == nil || i >= len(newbuf.Touched) || (newbuf.Touched[i] != nil &&
				(newbuf.Touched[i].FirstCell != -1 || newbuf.Touched[i].LastCell != -1)) {
				s.transformLine(newbuf, i)
				changedLines++
			}

			if i < len(newbuf.Touched) && i <= newbuf.Height()-1 {
				newbuf.Touched[i] = &LineData{
					FirstCell: -1, LastCell: -1,
				}
			}
			if i < len(s.curbuf.Touched) && i < s.curbuf.Height()-1 {
				s.curbuf.Touched[i] = &LineData{
					FirstCell: -1, LastCell: -1,
				}
			}
		}
	}

	if !s.flags.Contains(tFullscreen) && (curWidth != newWidth || curHeight != newHeight) {
		s.move(newbuf, 0, newHeight-1)
	}

	newbuf.Touched = make([]*LineData, newHeight)
	for i := range newbuf.Touched {
		newbuf.Touched[i] = &LineData{
			FirstCell: -1, LastCell: -1,
		}
	}
	for i := range s.curbuf.Touched {
		s.curbuf.Touched[i] = &LineData{
			FirstCell: -1, LastCell: -1,
		}
	}

	s.updatePen(nil)
}

func (s *TerminalRenderer) Erase() {
	s.clear = true
}

func (s *TerminalRenderer) Resize(width, _ int) {
	if s.tabs != nil {
		s.tabs.Resize(width)
	}
	if !s.flags.Contains(tRelativeCursor) {
		s.cur.X, s.cur.Y = -1, -1
	}
}

func (s *TerminalRenderer) Position() (x, y int) {
	return s.cur.X, s.cur.Y
}

func (s *TerminalRenderer) SetPosition(x, y int) {
	s.cur.X, s.cur.Y = x, y
}

func (s *TerminalRenderer) WriteString(str string) (int, error) {
	return s.buf.WriteString(str)
}

func (s *TerminalRenderer) Write(b []byte) (int, error) {
	return s.buf.Write(b)
}

func (s *TerminalRenderer) MoveTo(x, y int) {
	s.move(nil, x, y)
}

func notLocal(cols, fx, fy, tx, ty int) bool {

	const longDist = 8 - 1
	return (tx > longDist) &&
		(tx < cols-1-longDist) &&
		(abs(ty-fy)+abs(tx-fx) > longDist)
}

func relativeCursorMove(s *TerminalRenderer, newbuf *RenderBuffer, fx, fy, tx, ty int, overwrite, useTabs, useBackspace bool) string {
	var seq strings.Builder
	if newbuf == nil {
		overwrite = false
	}

	if ty != fy {
		var yseq string
		if s.caps.Contains(capVPA) && !s.flags.Contains(tRelativeCursor) {
			yseq = ansi.VerticalPositionAbsolute(ty + 1)
		}

		if ty > fy {
			n := ty - fy
			if cud := ansi.CursorDown(n); yseq == "" || len(cud) < len(yseq) {
				yseq = cud
			}
			if !s.flags.Contains(tFullscreen) || n < len(yseq) {
				yseq = strings.Repeat("\n", n)
				if s.flags.Contains(tMapNewline) {
					fx = 0
				}
			}
		} else if ty < fy {
			n := fy - ty
			if cuu := ansi.CursorUp(n); yseq == "" || len(cuu) < len(yseq) {
				yseq = cuu
			}
			if n == 1 && fy-1 > 0 {

				yseq = ansi.ReverseIndex
			}
		}

		seq.WriteString(yseq)
	}

	if tx != fx {
		var xseq string
		if !s.flags.Contains(tRelativeCursor) {
			if s.caps.Contains(capHPA) {
				xseq = ansi.HorizontalPositionAbsolute(tx + 1)
			} else if s.caps.Contains(capCHA) {
				xseq = ansi.CursorHorizontalAbsolute(tx + 1)
			}
		}

		if tx > fx {
			n := tx - fx
			if useTabs && s.tabs != nil {
				var tabs int
				var col int
				for col = fx; s.tabs.Next(col) <= tx; col = s.tabs.Next(col) {
					tabs++
					if col == s.tabs.Next(col) || col >= s.tabs.Width()-1 {
						break
					}
				}

				if tabs > 0 {
					cht := ansi.CursorHorizontalForwardTab(tabs)
					tab := strings.Repeat("\t", tabs)
					if false && s.caps.Contains(capCHT) && len(cht) < len(tab) {

						seq.WriteString(cht)
					} else {
						seq.WriteString(tab)
					}

					n = tx - col
					fx = col
				}
			}

			if cuf := ansi.CursorForward(n); xseq == "" || len(cuf) < len(xseq) {
				xseq = cuf
			}

			var ovw string
			if overwrite && ty >= 0 {
				for i := 0; i < n; i++ {
					cell := newbuf.CellAt(fx+i, ty)
					if cell != nil && cell.Width > 0 {
						i += cell.Width - 1
						if !cell.Style.Equal(&s.cur.Style) || !cell.Link.Equal(&s.cur.Link) {
							overwrite = false
							break
						}
					}
				}
			}

			if overwrite && ty >= 0 {
				for i := 0; i < n; i++ {
					cell := newbuf.CellAt(fx+i, ty)
					if cell != nil && cell.Width > 0 {
						ovw += cell.String()
						i += cell.Width - 1
					}
				}
			}

			if overwrite && len(ovw) < len(xseq) {
				xseq = ovw
			}
		} else if tx < fx {
			n := fx - tx
			if useTabs && s.tabs != nil && s.caps.Contains(capCBT) {

				col := fx

				var cbt int
				for s.tabs.Prev(col) >= tx {
					col = s.tabs.Prev(col)
					cbt++
					if col == s.tabs.Prev(col) || col <= 0 {
						break
					}
				}

				if cbt > 0 {
					seq.WriteString(ansi.CursorBackwardTab(cbt))
					n = col - tx
				}
			}

			if cub := ansi.CursorBackward(n); xseq == "" || len(cub) < len(xseq) {
				xseq = cub
			}

			if useBackspace && n < len(xseq) {
				xseq = strings.Repeat("\b", n)
			}
		}

		seq.WriteString(xseq)
	}

	return seq.String()
}

func moveCursor(s *TerminalRenderer, newbuf *RenderBuffer, x, y int, overwrite bool) (seq string) {
	fx, fy := s.cur.X, s.cur.Y

	if !s.flags.Contains(tRelativeCursor) {
		width := -1
		if s.tabs != nil {
			width = s.tabs.Width()
		}
		if newbuf != nil && width == -1 {

			width = newbuf.Width()
		}

		seq = ansi.CursorPosition(x+1, y+1)
		if fx == -1 || fy == -1 || width == -1 || notLocal(width, fx, fy, x, y) {
			return seq
		}
	}

	trials := 0
	if s.caps.Contains(capHT) {
		trials |= 2
	}
	if s.caps.Contains(capBS) {
		trials |= 1
	}

	for i := 0; i <= trials; i++ {

		if i & ^trials != 0 {
			continue
		}

		useHardTabs := i&2 != 0
		useBackspace := i&1 != 0

		nseq1 := relativeCursorMove(s, newbuf, fx, fy, x, y, overwrite, useHardTabs, useBackspace)
		if (i == 0 && len(seq) == 0) || len(nseq1) < len(seq) {
			seq = nseq1
		}

		nseq2 := relativeCursorMove(s, newbuf, 0, fy, x, y, overwrite, useHardTabs, useBackspace)
		nseq2 = "\r" + nseq2
		if len(nseq2) < len(seq) {
			seq = nseq2
		}

		if !s.flags.Contains(tRelativeCursor) {

			nseq3 := relativeCursorMove(s, newbuf, 0, 0, x, y, overwrite, useHardTabs, useBackspace)
			nseq3 = ansi.CursorHomePosition + nseq3
			if len(nseq3) < len(seq) {
				seq = nseq3
			}
		}
	}

	return seq
}

func xtermCaps(termtype string) (v capabilities) {
	parts := strings.Split(termtype, "-")
	if len(parts) == 0 {
		return v
	}

	switch parts[0] {
	case
		"contour",
		"foot",
		"ghostty",
		"kitty",
		"rio",
		"st",
		"tmux",
		"wezterm":
		v = allCaps
	case "xterm":
		switch {
		case len(parts) > 1 && parts[1] == "ghostty":
			fallthrough
		case len(parts) > 1 && parts[1] == "kitty":
			fallthrough
		case len(parts) > 1 && parts[1] == "rio":

			v = allCaps
		default:

			v = allCaps
			v &^= capHPA
			v &^= capCHT
			v &^= capREP
		}
	case "alacritty":
		v = allCaps
		v &^= capCHT
	case "screen":

		v = allCaps
		v &^= capREP
	case "linux":

		v = capVPA | capCHA | capHPA | capECH | capICH
	}

	return v
}
