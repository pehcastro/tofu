package uv

import (
	"bytes"
	"image"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type Position = image.Point

func Pos(x, y int) Position {
	return Position{X: x, Y: y}
}

type Rectangle = image.Rectangle

func Rect(x, y, w, h int) Rectangle {
	return Rectangle{Min: image.Point{X: x, Y: y}, Max: image.Point{X: x + w, Y: y + h}}
}

type Line []Cell

func NewLine(width int) Line {
	l := make(Line, width)
	for i := range l {
		l[i] = EmptyCell
	}
	return l
}

func (l Line) Set(x int, c *Cell) {
	lineWidth := len(l)
	if x < 0 || x >= lineWidth {
		return
	}

	var prev *Cell
	if prev = l.At(x); prev != nil {
		if pw := prev.Width; pw > 1 {

			for j := 0; j < pw && x+j < lineWidth; j++ {
				l[x+j] = *prev
				l[x+j].Empty()
			}
		} else if pw == 0 {

			for j := 1; x-j >= 0; j++ {
				if wide := l.At(x - j); wide != nil {
					if ww := wide.Width; ww > 1 && j < ww {
						for k := range ww {
							l[x-j+k] = *wide
							l[x-j+k].Empty()
						}
						break
					}
				}
			}
		}
	}

	if c == nil {

		l[x] = EmptyCell
		return
	}

	l[x] = *c
	cw := c.Width
	if x+cw > lineWidth {

		for i := 0; i < cw && x+i < lineWidth; i++ {
			l[x+i] = *c
			l[x+i].Empty()
		}
		return
	}

	if cw > 1 {

		for j := 1; j < cw && x+j < lineWidth; j++ {
			l[x+j] = Cell{}
		}
	}
}

func (l Line) At(x int) *Cell {
	if x < 0 || x >= len(l) {
		return nil
	}

	return &l[x]
}

func (l Line) String() string {
	var buf strings.Builder
	var pending bytes.Buffer
	for _, c := range l {
		if c.IsZero() {
			continue
		}
		if c.Equal(&EmptyCell) {
			pending.WriteByte(' ')
			continue
		}
		if pending.Len() > 0 {
			buf.WriteString(pending.String())
			pending.Reset()
		}
		buf.WriteString(c.String())
	}

	return buf.String()
}

func (l Line) Render() string {
	var buf strings.Builder
	renderLine(&buf, l)
	return buf.String()
}

func renderLine(buf io.StringWriter, l Line) {
	var pen Style
	var link Link
	var pending bytes.Buffer

	for _, c := range l {
		if c.IsZero() {
			continue
		}
		if c.Equal(&EmptyCell) {
			if !pen.IsZero() {
				_, _ = buf.WriteString(ansi.ResetStyle)
				pen = Style{}
			}
			if !link.IsZero() {
				_, _ = buf.WriteString(ansi.ResetHyperlink())
				link = Link{}
			}
			pending.WriteByte(' ')
			continue
		}

		if pending.Len() > 0 {
			_, _ = buf.WriteString(pending.String())
			pending.Reset()
		}

		if c.Style.IsZero() && !pen.IsZero() {
			_, _ = buf.WriteString(ansi.ResetStyle)
			pen = Style{}
		}
		if !c.Style.Equal(&pen) {
			seq := c.Style.Diff(&pen)
			_, _ = buf.WriteString(seq)
			pen = c.Style
		}

		if c.Link != link && link.URL != "" {
			_, _ = buf.WriteString(ansi.ResetHyperlink())
			link = Link{}
		}
		if c.Link != link {
			_, _ = buf.WriteString(ansi.SetHyperlink(c.Link.URL, c.Link.Params))
			link = c.Link
		}

		_, _ = buf.WriteString(c.String())
	}

	if pending.Len() > 0 {
		_, _ = buf.WriteString(pending.String())
	}
	if link.URL != "" {
		_, _ = buf.WriteString(ansi.ResetHyperlink())
	}
	if !pen.IsZero() {
		_, _ = buf.WriteString(ansi.ResetStyle)
	}
}

type Lines []Line

func (ls Lines) Height() int {
	return len(ls)
}

func (ls Lines) Width() int {
	maxWidth := 0
	for _, l := range ls {
		maxWidth = max(maxWidth, len(l))
	}
	return maxWidth
}

func (ls Lines) String() string {
	var buf strings.Builder
	for i, l := range ls {
		buf.WriteString(l.String())
		if i < len(ls)-1 {
			_ = buf.WriteByte('\n')
		}
	}
	return buf.String()
}

func (ls Lines) Render() string {
	var buf strings.Builder
	for i, l := range ls {
		renderLine(&buf, l)
		if i < len(ls)-1 {
			_ = buf.WriteByte('\n')
		}
	}
	return buf.String()
}

type Buffer struct {
	Lines []Line
}

var _ Drawable = (*Buffer)(nil)

func NewBuffer(width int, height int) *Buffer {
	b := new(Buffer)
	b.Lines = make([]Line, height)
	for i := range b.Lines {
		b.Lines[i] = make(Line, width)
		for j := range b.Lines[i] {
			b.Lines[i][j] = EmptyCell
		}
	}
	b.Resize(width, height)
	return b
}

func (b *Buffer) String() string {
	return Lines(b.Lines).String()
}

func (b *Buffer) Render() string {
	return Lines(b.Lines).Render()
}

func (b *Buffer) Line(y int) Line {
	if y < 0 || y >= len(b.Lines) {
		return nil
	}
	return b.Lines[y]
}

func (b *Buffer) CellAt(x int, y int) *Cell {
	if y < 0 || y >= len(b.Lines) {
		return nil
	}
	return b.Lines[y].At(x)
}

func (b *Buffer) SetCell(x, y int, c *Cell) {
	if y < 0 || y >= len(b.Lines) {
		return
	}

	b.Lines[y].Set(x, c)
}

func (b *Buffer) Height() int {
	return len(b.Lines)
}

func (b *Buffer) Width() int {
	if len(b.Lines) == 0 {
		return 0
	}
	return len(b.Lines[0])
}

func (b *Buffer) Bounds() Rectangle {
	return Rect(0, 0, b.Width(), b.Height())
}

func (b *Buffer) Resize(width int, height int) {
	curWidth, curHeight := b.Width(), b.Height()
	if curWidth == width && curHeight == height {

		return
	}

	if width > curWidth {
		line := make(Line, width-curWidth)
		for i := range line {
			line[i] = EmptyCell
		}
		for i := range b.Lines {
			b.Lines[i] = append(b.Lines[i], line...)
		}
	} else if width < curWidth {
		for i := range b.Lines {
			b.Lines[i] = b.Lines[i][:width]
		}
	}

	if height > len(b.Lines) {
		for i := len(b.Lines); i < height; i++ {
			line := make(Line, width)
			for j := range line {
				line[j] = EmptyCell
			}
			b.Lines = append(b.Lines, line)
		}
	} else if height < len(b.Lines) {
		b.Lines = b.Lines[:height]
	}
}

func (b *Buffer) Fill(c *Cell) {
	b.FillArea(c, b.Bounds())
}

func (b *Buffer) FillArea(c *Cell, area Rectangle) {
	cellWidth := 1
	if c != nil && c.Width > 1 {
		cellWidth = c.Width
	}
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x += cellWidth {
			b.SetCell(x, y, c)
		}
	}
}

func (b *Buffer) Clear() {
	area := b.Bounds()
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			b.Lines[y][x] = EmptyCell
		}
	}
}

func (b *Buffer) ClearArea(area Rectangle) {
	b.FillArea(nil, area)
}

func (b *Buffer) CloneArea(area Rectangle) *Buffer {
	bounds := b.Bounds()
	if !area.In(bounds) {
		return nil
	}
	n := NewBuffer(area.Dx(), area.Dy())
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; {
			c := b.CellAt(x, y)
			if c == nil || c.IsZero() {
				x++
				continue
			}
			n.SetCell(x-area.Min.X, y-area.Min.Y, c)
			x += max(c.Width, 1)
		}
	}
	return n
}

func (b *Buffer) Clone() *Buffer {
	return b.CloneArea(b.Bounds())
}

func (b *Buffer) Draw(scr Screen, area Rectangle) {
	if area.Empty() {
		return
	}

	bounds := scr.Bounds()
	if !area.Overlaps(bounds) {
		return
	}

	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; {
			c := b.CellAt(x-area.Min.X, y-area.Min.Y)
			if c == nil || c.IsZero() {
				x++
				continue
			}
			scr.SetCell(x, y, c)
			width := c.Width
			if width <= 0 {
				width = 1
			}
			x += width
		}
	}
}

func (b *Buffer) InsertLine(y, n int, c *Cell) {
	b.InsertLineArea(y, n, c, b.Bounds())
}

func (b *Buffer) InsertLineArea(y, n int, c *Cell, area Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() {
		return
	}

	if y+n > area.Max.Y {
		n = area.Max.Y - y
	}

	for i := area.Max.Y - 1; i >= y+n; i-- {
		for x := area.Min.X; x < area.Max.X; x++ {

			b.Lines[i][x] = b.Lines[i-n][x]
		}
	}

	for i := y; i < y+n; i++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			b.SetCell(x, i, c)
		}
	}
}

func (b *Buffer) DeleteLineArea(y, n int, c *Cell, area Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() {
		return
	}

	if n > area.Max.Y-y {
		n = area.Max.Y - y
	}

	for dst := y; dst < area.Max.Y-n; dst++ {
		src := dst + n
		for x := area.Min.X; x < area.Max.X; x++ {

			b.Lines[dst][x] = b.Lines[src][x]
		}
	}

	for i := area.Max.Y - n; i < area.Max.Y; i++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			b.SetCell(x, i, c)
		}
	}
}

func (b *Buffer) DeleteLine(y, n int, c *Cell) {
	b.DeleteLineArea(y, n, c, b.Bounds())
}

func (b *Buffer) InsertCell(x, y, n int, c *Cell) {
	b.InsertCellArea(x, y, n, c, b.Bounds())
}

func (b *Buffer) InsertCellArea(x, y, n int, c *Cell, area Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() ||
		x < area.Min.X || x >= area.Max.X || x >= b.Width() {
		return
	}

	if x+n > area.Max.X {
		n = area.Max.X - x
	}

	for i := area.Max.X - 1; i >= x+n && i-n >= area.Min.X; i-- {

		b.Lines[y][i] = b.Lines[y][i-n]
	}

	for i := x; i < x+n && i < area.Max.X; i++ {
		b.SetCell(i, y, c)
	}
}

func (b *Buffer) DeleteCell(x, y, n int, c *Cell) {
	b.DeleteCellArea(x, y, n, c, b.Bounds())
}

func (b *Buffer) DeleteCellArea(x, y, n int, c *Cell, area Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() ||
		x < area.Min.X || x >= area.Max.X || x >= b.Width() {
		return
	}

	remainingCells := area.Max.X - x
	if n > remainingCells {
		n = remainingCells
	}

	for i := x; i < area.Max.X-n; i++ {
		if i+n < area.Max.X {

			b.SetCell(i, y, b.CellAt(i+n, y))
		}
	}

	for i := area.Max.X - n; i < area.Max.X; i++ {
		b.SetCell(i, y, c)
	}
}

type ScreenBuffer struct {
	*RenderBuffer
	Method ansi.Method
}

var (
	_ Screen   = ScreenBuffer{}
	_ Drawable = ScreenBuffer{}
)

func NewScreenBuffer(width, height int) ScreenBuffer {
	return ScreenBuffer{
		RenderBuffer: NewRenderBuffer(width, height),
		Method:       ansi.WcWidth,
	}
}

func (s ScreenBuffer) WidthMethod() WidthMethod {
	return s.Method
}

func TrimSpace(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {

		hasCR := strings.HasSuffix(line, "\r")
		if hasCR {
			line = strings.TrimSuffix(line, "\r")
		}
		line = strings.TrimRight(line, " ")
		if hasCR {
			line = line + "\r"
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

type RenderBuffer struct {
	*Buffer
	Touched []*LineData
}

func NewRenderBuffer(width, height int) *RenderBuffer {
	return &RenderBuffer{
		Buffer:  NewBuffer(width, height),
		Touched: make([]*LineData, height),
	}
}

func (b *RenderBuffer) TouchLine(x, y, n int) {
	if y < 0 || y >= len(b.Lines) {
		return
	}

	if y >= len(b.Touched) {
		b.Touched = append(b.Touched, make([]*LineData, y-len(b.Touched)+1)...)
	}

	if y >= len(b.Touched) {
		return
	}

	ch := b.Touched[y]
	if ch == nil {
		ch = &LineData{FirstCell: x, LastCell: x + n}
	} else {
		ch.FirstCell = min(ch.FirstCell, x)
		ch.LastCell = max(ch.LastCell, x+n)
	}
	b.Touched[y] = ch
}

func (b *RenderBuffer) Touch(x, y int) {
	b.TouchLine(x, y, 0)
}

func (b *RenderBuffer) TouchedLines() int {
	if b.Touched == nil {
		return 0
	}
	count := 0
	for _, t := range b.Touched {
		if t != nil {
			count++
		}
	}
	return count
}

func (b *RenderBuffer) SetCell(x, y int, c *Cell) {
	if p := b.CellAt(x, y); !cellEqual(p, c) {
		width := 1
		if c != nil && c.Width > 0 {
			width = c.Width
		}
		if p != nil && p.Width > 0 {
			width = max(width, p.Width)
		}
		b.TouchLine(x, y, width)
	}
	b.Buffer.SetCell(x, y, c)
}

func (b *RenderBuffer) InsertLine(y, n int, c *Cell) {
	b.InsertLineArea(y, n, c, b.Bounds())
}

func (b *RenderBuffer) InsertLineArea(y, n int, c *Cell, area Rectangle) {
	b.Buffer.InsertLineArea(y, n, c, area)
	for i := area.Min.Y; i < area.Max.Y; i++ {
		b.TouchLine(area.Min.X, i, area.Max.X-area.Min.X)
		b.TouchLine(area.Min.X, i-n, area.Max.X-area.Min.X)
	}
}

func (b *RenderBuffer) DeleteLine(y, n int, c *Cell) {
	b.DeleteLineArea(y, n, c, b.Bounds())
}

func (b *RenderBuffer) DeleteLineArea(y, n int, c *Cell, area Rectangle) {
	b.Buffer.DeleteLineArea(y, n, c, area)
	for i := area.Min.Y; i < area.Max.Y; i++ {
		b.TouchLine(area.Min.X, i, area.Max.X-area.Min.X)
		b.TouchLine(area.Min.X, i+n, area.Max.X-area.Min.X)
	}
}

func (b *RenderBuffer) InsertCell(x, y, n int, c *Cell) {
	b.InsertCellArea(x, y, n, c, b.Bounds())
}

func (b *RenderBuffer) InsertCellArea(x, y, n int, c *Cell, area Rectangle) {
	b.Buffer.InsertCellArea(x, y, n, c, area)
	if x+n > area.Max.X {
		n = area.Max.X - x
	}
	b.TouchLine(x, y, n)
}

func (b *RenderBuffer) DeleteCell(x, y, n int, c *Cell) {
	b.DeleteCellArea(x, y, n, c, b.Bounds())
}

func (b *RenderBuffer) DeleteCellArea(x, y, n int, c *Cell, area Rectangle) {
	b.Buffer.DeleteCellArea(x, y, n, c, area)
	remainingCells := area.Max.X - x
	if n > remainingCells {
		n = remainingCells
	}
	b.TouchLine(x, y, n)
}

func (b *RenderBuffer) Clear() {
	b.Buffer.Clear()
	w := b.Width()
	for y := range b.Lines {
		b.TouchLine(0, y, w)
	}
}

func (b *RenderBuffer) ClearArea(area Rectangle) {
	b.Buffer.ClearArea(area)
	w := area.Max.X - area.Min.X
	for y := area.Min.Y; y < area.Max.Y; y++ {
		b.TouchLine(area.Min.X, y, w)
	}
}

func (b *RenderBuffer) Fill(c *Cell) {
	b.FillArea(c, b.Bounds())
}

func (b *RenderBuffer) FillArea(c *Cell, area Rectangle) {
	b.Buffer.FillArea(c, area)
	w := area.Max.X - area.Min.X
	for y := area.Min.Y; y < area.Max.Y; y++ {
		b.TouchLine(area.Min.X, y, w)
	}
}
