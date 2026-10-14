package uv

import (
	"bytes"
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type StyledString struct {
	Text string

	Wrap bool

	Tail string
}

var _ Drawable = (*StyledString)(nil)

func NewStyledString(str string) *StyledString {
	ss := new(StyledString)
	ss.Text = str
	return ss
}

func (s *StyledString) String() string {
	return s.Text
}

func (s *StyledString) Lines(m ansi.Method) []Line {
	return printString(nil, m, 0, 0, Rectangle{}, s.Text, false, "")
}

func (s *StyledString) Draw(buf Screen, area Rectangle) {

	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			buf.SetCell(x, y, nil)
		}
	}
	str := s.Text

	str = strings.ReplaceAll(str, "\r\n", "\n")
	printString(buf, buf.WidthMethod(), area.Min.X, area.Min.Y, area, str, !s.Wrap, s.Tail)
}

func (s *StyledString) Height() int {
	return strings.Count(s.Text, "\n") + 1
}

func (s *StyledString) UnicodeWidth() int {
	w, _ := s.widthHeight(ansi.GraphemeWidth)
	return w
}

func (s *StyledString) WcWidth() int {
	w, _ := s.widthHeight(ansi.WcWidth)
	return w
}

func (s *StyledString) widthHeight(m ansi.Method) (w, h int) {
	lines := strings.Split(s.Text, "\n")
	h = len(lines)
	for _, l := range lines {
		w = max(w, m.StringWidth(l))
	}
	return
}

func (s *StyledString) Bounds() Rectangle {
	w, h := s.widthHeight(ansi.GraphemeWidth)
	return Rect(0, 0, w, h)
}

func printString[T []byte | string](
	s Screen,
	m WidthMethod,
	x, y int,
	bounds Rectangle, str T,
	truncate bool, tail string,
) (lines []Line) {
	p := ansi.GetParser()
	defer ansi.PutParser(p)

	var tailc Cell
	if truncate && len(tail) > 0 {
		tailc = *NewCell(m, tail)
	}

	decoder, method := ansi.DecodeSequenceWc[T], ansi.WcWidth
	if m == ansi.GraphemeWidth {
		decoder, method = ansi.DecodeSequence[T], ansi.GraphemeWidth
	}

	if s == nil {
		lines = []Line{}
	}

	var cell Cell
	var style Style
	var link Link
	var state byte
	lastX, lastY := -1, -1
	for len(str) > 0 {
		seq, width, n, newState := decoder(str, state, p)

		if n == 1 && seq[0] > 0x1f && seq[0] < 0x7f && len(str) > 1 && str[1] >= 0xc0 {
			if cluster, cw := ansi.FirstGraphemeCluster(str, method); len(cluster) > 1 {
				seq, width, n = cluster, cw, len(cluster)
			}
		}
		switch {

		case width > 0:
			cell.Width = width
			cell.Content = string(seq)
			cell.Style = style
			cell.Link = link

			if s == nil {

				if y >= len(lines) {
					lines = append(lines, Line{})
				}
				lines[y] = append(lines[y], cell)
				lastX, lastY = len(lines[y])-1, y
				x += width
			} else {

				if !truncate && x+cell.Width > bounds.Max.X && y+1 < bounds.Max.Y {

					x = bounds.Min.X
					y++
				}

				pos := Pos(x, y)
				if pos.In(bounds) {
					if truncate && tailc.Width > 0 && x+cell.Width > bounds.Max.X-tailc.Width {

						cell = tailc
						cell.Style = style
						cell.Link = link
						s.SetCell(x, y, &cell)
						lastX, lastY = x, y
						x += tailc.Width
					} else {

						s.SetCell(x, y, &cell)
						lastX, lastY = x, y
						x += width
					}
				}
			}

			cell = Cell{}
		default:

			switch {
			case ansi.HasCsiPrefix(seq) && p.Command() == 'm':

				ReadStyle(p.Params(), &style)
			case ansi.HasOscPrefix(seq) && p.Command() == 8:

				ReadLink(p.Data(), &link)
			case ansi.Equal(seq, T("\n")):
				if s == nil {

					if y >= len(lines) {
						lines = append(lines, Line{})
					}
				}
				y++

				fallthrough
			case ansi.Equal(seq, T("\r")):
				if s == nil {
					x = 0
				} else {
					x = bounds.Min.X
				}
			case len(seq) > 0 && seq[0] >= 0xc0:

				if s == nil {
					if lastY >= 0 && lastY < len(lines) && lastX < len(lines[lastY]) {
						lines[lastY][lastX].Content += string(seq)
					}
				} else if lastX >= 0 {
					if prev := s.CellAt(lastX, lastY); prev != nil {
						folded := *prev
						folded.Content += string(seq)
						s.SetCell(lastX, lastY, &folded)
					}
				}
			default:
				cell.Content += string(seq)
			}
		}

		state = newState
		str = str[n:]

		if y >= bounds.Max.Y {

			break
		}
	}

	if !cell.IsZero() && s != nil {
		s.SetCell(x, y, &cell)
	}

	return lines
}

func ReadStyle(params ansi.Params, pen *Style) {
	if len(params) == 0 {
		*pen = Style{}
		return
	}

	for i := 0; i < len(params); i++ {
		param, hasMore, _ := params.Param(i, 0)
		switch param {
		case 0:
			*pen = Style{}
		case 1:
			pen.Attrs |= AttrBold
		case 2:
			pen.Attrs |= AttrFaint
		case 3:
			pen.Attrs |= AttrItalic
		case 4:
			nextParam, _, ok := params.Param(i+1, 0)
			if hasMore && ok {
				switch nextParam {
				case 0, 1, 2, 3, 4, 5:
					i++
					switch nextParam {
					case 0:
						pen.Underline = UnderlineStyleNone
					case 1:
						pen.Underline = UnderlineStyleSingle
					case 2:
						pen.Underline = UnderlineStyleDouble
					case 3:
						pen.Underline = UnderlineStyleCurly
					case 4:
						pen.Underline = UnderlineStyleDotted
					case 5:
						pen.Underline = UnderlineStyleDashed
					}
				}
			} else {

				pen.Underline = UnderlineStyleSingle
			}
		case 5:
			pen.Attrs |= AttrBlink
		case 6:
			pen.Attrs |= AttrRapidBlink
		case 7:
			pen.Attrs |= AttrReverse
		case 8:
			pen.Attrs |= AttrConceal
		case 9:
			pen.Attrs |= AttrStrikethrough
		case 22:
			pen.Attrs &^= (AttrBold | AttrFaint)
		case 23:
			pen.Attrs &^= AttrItalic
		case 24:
			pen.Underline = UnderlineStyleNone
		case 25:
			pen.Attrs &^= (AttrBlink | AttrRapidBlink)
		case 27:
			pen.Attrs &^= AttrReverse
		case 28:
			pen.Attrs &^= AttrConceal
		case 29:
			pen.Attrs &^= AttrStrikethrough
		case 30, 31, 32, 33, 34, 35, 36, 37:
			pen.Fg = ansi.Black + ansi.BasicColor(param-30)
		case 38:
			var c color.Color
			n := ansi.ReadStyleColor(params[i:], &c)
			if n > 0 {
				pen.Fg = c
				i += n - 1
			}
		case 39:
			pen.Fg = nil
		case 40, 41, 42, 43, 44, 45, 46, 47:
			pen.Bg = ansi.Black + ansi.BasicColor(param-40)
		case 48:
			var c color.Color
			n := ansi.ReadStyleColor(params[i:], &c)
			if n > 0 {
				pen.Bg = c
				i += n - 1
			}
		case 49:
			pen.Bg = nil
		case 58:
			var c color.Color
			n := ansi.ReadStyleColor(params[i:], &c)
			if n > 0 {
				pen.UnderlineColor = c
				i += n - 1
			}
		case 59:
			pen.UnderlineColor = nil
		case 90, 91, 92, 93, 94, 95, 96, 97:
			pen.Fg = ansi.BrightBlack + ansi.BasicColor(param-90)
		case 100, 101, 102, 103, 104, 105, 106, 107:
			pen.Bg = ansi.BrightBlack + ansi.BasicColor(param-100)
		}
	}
}

func ReadLink(p []byte, link *Link) {
	params := bytes.Split(p, []byte{';'})
	if len(params) != 3 {
		return
	}
	link.Params = string(params[1])
	link.URL = string(params[2])
}
