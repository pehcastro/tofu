package uv

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (s *TerminalRenderer) scrollOptimize(newbuf *RenderBuffer) {
	height := newbuf.Height()
	if s.oldnum == nil || len(s.oldnum) < height {
		s.oldnum = append(s.oldnum, make([]int, height-len(s.oldnum))...)
	}

	s.updateHashmap(newbuf)
	if len(s.hashtab) < height {
		return
	}

	for i := 0; i < height; {
		for i < height && (s.oldnum[i] == newIndex || s.oldnum[i] <= i) {
			i++
		}
		if i >= height {
			break
		}

		shift := s.oldnum[i] - i
		start := i

		i++
		for i < height && s.oldnum[i] != newIndex && s.oldnum[i]-i == shift {
			i++
		}
		end := i - 1 + shift

		if !s.scrolln(newbuf, shift, start, end, height-1) {
			continue
		}
	}

	for i := height - 1; i >= 0; {
		for i >= 0 && (s.oldnum[i] == newIndex || s.oldnum[i] >= i) {
			i--
		}
		if i < 0 {
			break
		}

		shift := s.oldnum[i] - i
		end := i

		i--
		for i >= 0 && s.oldnum[i] != newIndex && s.oldnum[i]-i == shift {
			i--
		}

		start := i + 1 - (-shift)
		if !s.scrolln(newbuf, shift, start, end, height-1) {
			continue
		}
	}
}

func (s *TerminalRenderer) scrolln(newbuf *RenderBuffer, n, top, bot, maxY int) (v bool) {
	blank := s.clearBlank()
	if n > 0 {

		v = s.scrollUp(newbuf, n, top, bot, 0, maxY, blank)
		if !v {
			s.buf.WriteString(ansi.SetTopBottomMargins(top+1, bot+1))

			s.cur.X, s.cur.Y = -1, -1
			v = s.scrollUp(newbuf, n, top, bot, top, bot, blank)
			s.buf.WriteString(ansi.SetTopBottomMargins(1, maxY+1))
			s.cur.X, s.cur.Y = -1, -1
		}

		if !v {
			v = s.scrollIdl(newbuf, n, top, bot-n+1, blank)
		}
	} else if n < 0 {

		v = s.scrollDown(newbuf, -n, top, bot, 0, maxY, blank)
		if !v {
			s.buf.WriteString(ansi.SetTopBottomMargins(top+1, bot+1))

			s.cur.X, s.cur.Y = -1, -1
			v = s.scrollDown(newbuf, -n, top, bot, top, bot, blank)
			s.buf.WriteString(ansi.SetTopBottomMargins(1, maxY+1))
			s.cur.X, s.cur.Y = -1, -1

			if !v {
				v = s.scrollIdl(newbuf, -n, bot+n+1, top, blank)
			}
		}
	}

	if !v {
		return false
	}

	s.scrollBuffer(s.curbuf, n, top, bot, blank)

	s.scrollOldhash(n, top, bot)

	return true
}

func (s *TerminalRenderer) scrollBuffer(b *RenderBuffer, n, top, bot int, blank *Cell) {
	if top < 0 || bot < top || bot >= b.Height() {

		return
	}

	if n < 0 {

		limit := top - n
		for line := bot; line >= limit && line >= 0 && line >= top; line-- {
			copy(b.Lines[line], b.Lines[line+n])
		}
		for line := top; line < limit && line <= b.Height()-1 && line <= bot; line++ {
			b.FillArea(blank, Rect(0, line, b.Width(), 1))
		}
	}

	if n > 0 {

		limit := bot - n
		for line := top; line <= limit && line <= b.Height()-1 && line <= bot; line++ {
			copy(b.Lines[line], b.Lines[line+n])
		}
		for line := bot; line > limit && line >= 0 && line >= top; line-- {
			b.FillArea(blank, Rect(0, line, b.Width(), 1))
		}
	}

	s.touchLine(b, top, bot-top+1, true)
}

func (s *TerminalRenderer) touchLine(newbuf *RenderBuffer, y, n int, changed bool) {
	height := newbuf.Height()
	if n < 0 || y < 0 || y >= height || newbuf.Touched == nil || len(newbuf.Touched) < height {
		return
	}

	width := newbuf.Width()
	for i := y; i < y+n && i < height && i < len(newbuf.Touched); i++ {
		if changed {
			newbuf.TouchLine(0, i, width)
		} else {
			newbuf.Touched[i] = nil
		}
	}
}

func (s *TerminalRenderer) scrollUp(newbuf *RenderBuffer, n, top, bot, minY, maxY int, blank *Cell) bool {
	if n == 1 && top == minY && bot == maxY {
		s.move(newbuf, 0, bot)
		s.updatePen(blank)
		s.buf.WriteByte('\n')
	} else if n == 1 && bot == maxY {
		s.move(newbuf, 0, top)
		s.updatePen(blank)
		s.buf.WriteString(ansi.DeleteLine(1))
	} else if top == minY && bot == maxY {
		supportsSU := s.caps.Contains(capSU)
		s.move(newbuf, 0, bot)
		s.updatePen(blank)
		if supportsSU {
			s.buf.WriteString(ansi.ScrollUp(n))
		} else {
			s.buf.WriteString(strings.Repeat("\n", n))
		}
	} else if bot == maxY {
		s.move(newbuf, 0, top)
		s.updatePen(blank)
		s.buf.WriteString(ansi.DeleteLine(n))
	} else {
		return false
	}
	return true
}

func (s *TerminalRenderer) scrollDown(newbuf *RenderBuffer, n, top, bot, minY, maxY int, blank *Cell) bool {
	if n == 1 && top == minY && bot == maxY {
		s.move(newbuf, 0, top)
		s.updatePen(blank)
		s.buf.WriteString(ansi.ReverseIndex)
	} else if n == 1 && bot == maxY {
		s.move(newbuf, 0, top)
		s.updatePen(blank)
		s.buf.WriteString(ansi.InsertLine(1))
	} else if top == minY && bot == maxY {
		s.move(newbuf, 0, top)
		s.updatePen(blank)
		if s.caps.Contains(capSD) {
			s.buf.WriteString(ansi.ScrollDown(n))
		} else {
			s.buf.WriteString(strings.Repeat(ansi.ReverseIndex, n))
		}
	} else if bot == maxY {
		s.move(newbuf, 0, top)
		s.updatePen(blank)
		s.buf.WriteString(ansi.InsertLine(n))
	} else {
		return false
	}
	return true
}

func (s *TerminalRenderer) scrollIdl(newbuf *RenderBuffer, n, del, ins int, blank *Cell) bool {
	if n < 0 {
		return false
	}

	s.move(newbuf, 0, del)
	s.updatePen(blank)
	s.buf.WriteString(ansi.DeleteLine(n))

	s.move(newbuf, 0, ins)
	s.updatePen(blank)
	s.buf.WriteString(ansi.InsertLine(n))

	return true
}
