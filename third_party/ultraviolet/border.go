package uv

func NormalBorder() Border {
	return Border{
		Top:         Side{Content: "─"},
		Bottom:      Side{Content: "─"},
		Left:        Side{Content: "│"},
		Right:       Side{Content: "│"},
		TopLeft:     Side{Content: "┌"},
		TopRight:    Side{Content: "┐"},
		BottomLeft:  Side{Content: "└"},
		BottomRight: Side{Content: "┘"},
	}
}

func RoundedBorder() Border {
	return Border{
		Top:         Side{Content: "─"},
		Bottom:      Side{Content: "─"},
		Left:        Side{Content: "│"},
		Right:       Side{Content: "│"},
		TopLeft:     Side{Content: "╭"},
		TopRight:    Side{Content: "╮"},
		BottomLeft:  Side{Content: "╰"},
		BottomRight: Side{Content: "╯"},
	}
}

func BlockBorder() Border {
	return Border{
		Top:         Side{Content: "█"},
		Bottom:      Side{Content: "█"},
		Left:        Side{Content: "█"},
		Right:       Side{Content: "█"},
		TopLeft:     Side{Content: "█"},
		TopRight:    Side{Content: "█"},
		BottomLeft:  Side{Content: "█"},
		BottomRight: Side{Content: "█"},
	}
}

func OuterHalfBlockBorder() Border {
	return Border{
		Top:         Side{Content: "▀"},
		Bottom:      Side{Content: "▄"},
		Left:        Side{Content: "▌"},
		Right:       Side{Content: "▐"},
		TopLeft:     Side{Content: "▛"},
		TopRight:    Side{Content: "▜"},
		BottomLeft:  Side{Content: "▙"},
		BottomRight: Side{Content: "▟"},
	}
}

func InnerHalfBlockBorder() Border {
	return Border{
		Top:         Side{Content: "▄"},
		Bottom:      Side{Content: "▀"},
		Left:        Side{Content: "▐"},
		Right:       Side{Content: "▌"},
		TopLeft:     Side{Content: "▗"},
		TopRight:    Side{Content: "▖"},
		BottomLeft:  Side{Content: "▝"},
		BottomRight: Side{Content: "▘"},
	}
}

func ThickBorder() Border {
	return Border{
		Top:         Side{Content: "━"},
		Bottom:      Side{Content: "━"},
		Left:        Side{Content: "┃"},
		Right:       Side{Content: "┃"},
		TopLeft:     Side{Content: "┏"},
		TopRight:    Side{Content: "┓"},
		BottomLeft:  Side{Content: "┗"},
		BottomRight: Side{Content: "┛"},
	}
}

func DoubleBorder() Border {
	return Border{
		Top:         Side{Content: "═"},
		Bottom:      Side{Content: "═"},
		Left:        Side{Content: "║"},
		Right:       Side{Content: "║"},
		TopLeft:     Side{Content: "╔"},
		TopRight:    Side{Content: "╗"},
		BottomLeft:  Side{Content: "╚"},
		BottomRight: Side{Content: "╝"},
	}
}

func HiddenBorder() Border {
	return Border{
		Top:         Side{Content: " "},
		Bottom:      Side{Content: " "},
		Left:        Side{Content: " "},
		Right:       Side{Content: " "},
		TopLeft:     Side{Content: " "},
		TopRight:    Side{Content: " "},
		BottomLeft:  Side{Content: " "},
		BottomRight: Side{Content: " "},
	}
}

func MarkdownBorder() Border {
	return Border{
		Left:        Side{Content: "|"},
		Right:       Side{Content: "|"},
		TopLeft:     Side{Content: "|"},
		TopRight:    Side{Content: "|"},
		BottomLeft:  Side{Content: "|"},
		BottomRight: Side{Content: "|"},
	}
}

func ASCIIBorder() Border {
	return Border{
		Top:         Side{Content: "-"},
		Bottom:      Side{Content: "-"},
		Left:        Side{Content: "|"},
		Right:       Side{Content: "|"},
		TopLeft:     Side{Content: "+"},
		TopRight:    Side{Content: "+"},
		BottomLeft:  Side{Content: "+"},
		BottomRight: Side{Content: "+"},
	}
}

type Side struct {
	Content string
	Style
	Link
}

type Border struct {
	Top         Side
	Bottom      Side
	Left        Side
	Right       Side
	TopLeft     Side
	TopRight    Side
	BottomLeft  Side
	BottomRight Side
}

func (b Border) Style(style Style) Border {
	b.Top.Style = style
	b.Bottom.Style = style
	b.Left.Style = style
	b.Right.Style = style
	b.TopLeft.Style = style
	b.TopRight.Style = style
	b.BottomLeft.Style = style
	b.BottomRight.Style = style
	return b
}

func (b Border) Link(link Link) Border {
	b.Top.Link = link
	b.Bottom.Link = link
	b.Left.Link = link
	b.Right.Link = link
	b.TopLeft.Link = link
	b.TopRight.Link = link
	b.BottomLeft.Link = link
	b.BottomRight.Link = link
	return b
}

func (b *Border) Draw(scr Screen, area Rectangle) {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			var cell *Cell
			switch {
			case y == area.Min.Y && x == area.Min.X:
				cell = borderCell(scr, &b.TopLeft)
			case y == area.Min.Y && x == area.Max.X-1:
				cell = borderCell(scr, &b.TopRight)
			case y == area.Max.Y-1 && x == area.Min.X:
				cell = borderCell(scr, &b.BottomLeft)
			case y == area.Max.Y-1 && x == area.Max.X-1:
				cell = borderCell(scr, &b.BottomRight)
			case y == area.Min.Y:
				cell = borderCell(scr, &b.Top)
			case y == area.Max.Y-1:
				cell = borderCell(scr, &b.Bottom)
			case x == area.Min.X:
				cell = borderCell(scr, &b.Left)
			case x == area.Max.X-1:
				cell = borderCell(scr, &b.Right)
			default:
				continue
			}
			if cell == nil {
				continue
			}
			scr.SetCell(x, y, cell)
		}
	}
}

func borderCell(scr Screen, b *Side) *Cell {
	c := NewCell(scr.WidthMethod(), b.Content)
	if c != nil {
		c.Style = b.Style
		c.Link = b.Link
	}
	return c
}
