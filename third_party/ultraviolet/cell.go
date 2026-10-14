package uv

import (
	"image/color"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

var EmptyCell = Cell{Content: " ", Width: 1}

type Cell struct {
	Content string

	Style Style

	Link Link

	Width int
}

func NewCell(method WidthMethod, gr string) *Cell {
	if len(gr) == 0 {
		return &Cell{}
	}
	if gr == " " {
		return EmptyCell.Clone()
	}
	return &Cell{
		Content: gr,
		Width:   method.StringWidth(gr),
	}
}

func (c *Cell) String() string {
	return c.Content
}

func (c *Cell) Equal(o *Cell) bool {
	return o != nil &&
		c.Width == o.Width &&
		c.Content == o.Content &&
		c.Style.Equal(&o.Style) &&
		c.Link.Equal(&o.Link)
}

func (c *Cell) IsZero() bool {
	return *c == Cell{}
}

func (c *Cell) isWidePlaceholder() bool {
	return c != nil && c.Width == 0
}

func (c *Cell) Clone() (n *Cell) {
	n = new(Cell)
	*n = *c
	return
}

func (c *Cell) Empty() {
	c.Content = " "
	c.Width = 1
}

func NewLink(url string, params ...string) Link {
	return Link{
		URL:    url,
		Params: strings.Join(params, ":"),
	}
}

type Link struct {
	URL    string
	Params string
}

func (h *Link) String() string {
	return h.URL
}

func (h *Link) Equal(o *Link) bool {
	return o != nil && *h == *o
}

func (h *Link) IsZero() bool {
	return *h == Link{}
}

const (
	AttrBold = 1 << iota
	AttrFaint
	AttrItalic
	AttrBlink
	AttrRapidBlink
	AttrReverse
	AttrConceal
	AttrStrikethrough

	AttrReset = 0
)

const (
	AttrSlowBlink = AttrBlink
)

type Underline = ansi.Underline

type UnderlineStyle = ansi.Underline

const (
	UnderlineNone   = ansi.UnderlineNone
	UnderlineSingle = ansi.UnderlineSingle
	UnderlineDouble = ansi.UnderlineDouble
	UnderlineCurly  = ansi.UnderlineCurly
	UnderlineDotted = ansi.UnderlineDotted
	UnderlineDashed = ansi.UnderlineDashed
)

const (
	UnderlineStyleNone   = ansi.UnderlineNone
	UnderlineStyleSingle = ansi.UnderlineSingle
	UnderlineStyleDouble = ansi.UnderlineDouble
	UnderlineStyleCurly  = ansi.UnderlineCurly
	UnderlineStyleDotted = ansi.UnderlineDotted
	UnderlineStyleDashed = ansi.UnderlineDashed
)

type Style struct {
	Fg             color.Color
	Bg             color.Color
	UnderlineColor color.Color
	Underline      Underline
	Attrs          uint8
}

func (s *Style) Equal(o *Style) bool {
	return s.Attrs == o.Attrs &&
		s.Underline == o.Underline &&
		colorEqual(s.Fg, o.Fg) &&
		colorEqual(s.Bg, o.Bg) &&
		colorEqual(s.UnderlineColor, o.UnderlineColor)
}

func (s *Style) Styled(str string) string {
	if s.IsZero() {
		return str
	}
	return s.String() + str + ansi.ResetStyle
}

func (s *Style) String() string {
	if s.IsZero() {
		return ansi.ResetStyle
	}

	var b ansi.Style

	if s.Attrs != 0 {
		if s.Attrs&AttrBold != 0 {
			b = b.Bold()
		}
		if s.Attrs&AttrFaint != 0 {
			b = b.Faint()
		}
		if s.Attrs&AttrItalic != 0 {
			b = b.Italic(true)
		}
		if s.Attrs&AttrBlink != 0 {
			b = b.Blink(true)
		}
		if s.Attrs&AttrRapidBlink != 0 {
			b = b.RapidBlink(true)
		}
		if s.Attrs&AttrReverse != 0 {
			b = b.Reverse(true)
		}
		if s.Attrs&AttrConceal != 0 {
			b = b.Conceal(true)
		}
		if s.Attrs&AttrStrikethrough != 0 {
			b = b.Strikethrough(true)
		}
	}
	if s.Underline != UnderlineStyleNone {
		switch s.Underline {
		case UnderlineStyleSingle:
			b = b.Underline(true)
		case UnderlineStyleDouble:
			b = b.UnderlineStyle(UnderlineStyleDouble)
		case UnderlineStyleCurly:
			b = b.UnderlineStyle(UnderlineStyleCurly)
		case UnderlineStyleDotted:
			b = b.UnderlineStyle(UnderlineStyleDotted)
		case UnderlineStyleDashed:
			b = b.UnderlineStyle(UnderlineStyleDashed)
		}
	}
	if s.Fg != nil {
		b = b.ForegroundColor(s.Fg)
	}
	if s.Bg != nil {
		b = b.BackgroundColor(s.Bg)
	}
	if s.UnderlineColor != nil {
		b = b.UnderlineColor(s.UnderlineColor)
	}

	return b.String()
}

func (s *Style) Diff(from *Style) string {
	return StyleDiff(from, s)
}

func StyleDiff(from, to *Style) string {
	if from == nil && to == nil {
		return ""
	}
	if from != nil && to != nil && from.Equal(to) {
		return ""
	}
	if from == nil {
		return to.String()
	}
	if to == nil || to.IsZero() {

		return ansi.ResetStyle
	}

	var b ansi.Style

	if !colorEqual(from.Fg, to.Fg) {
		b = b.ForegroundColor(to.Fg)
	}

	if !colorEqual(from.Bg, to.Bg) {
		b = b.BackgroundColor(to.Bg)
	}

	if !colorEqual(from.UnderlineColor, to.UnderlineColor) {

		b = b.UnderlineColor(to.UnderlineColor)
	}

	fromBold := from.Attrs&AttrBold != 0
	fromFaint := from.Attrs&AttrFaint != 0
	fromItalic := from.Attrs&AttrItalic != 0
	fromUnderline := from.Underline != UnderlineStyleNone
	fromBlink := from.Attrs&AttrBlink != 0
	fromRapidBlink := from.Attrs&AttrRapidBlink != 0
	fromReverse := from.Attrs&AttrReverse != 0
	fromConceal := from.Attrs&AttrConceal != 0
	fromStrikethrough := from.Attrs&AttrStrikethrough != 0
	toBold := to.Attrs&AttrBold != 0
	toFaint := to.Attrs&AttrFaint != 0
	toItalic := to.Attrs&AttrItalic != 0
	toUnderline := to.Underline != UnderlineStyleNone
	toBlink := to.Attrs&AttrBlink != 0
	toRapidBlink := to.Attrs&AttrRapidBlink != 0
	toReverse := to.Attrs&AttrReverse != 0
	toConceal := to.Attrs&AttrConceal != 0
	toStrikethrough := to.Attrs&AttrStrikethrough != 0

	boldChanged := fromBold != toBold
	faintChanged := fromFaint != toFaint
	if boldChanged || faintChanged {
		if fromBold && !toBold || fromFaint && !toFaint {
			b = b.Normal()
			boldChanged = true
			faintChanged = true
		}
	}

	italicChanged := fromItalic != toItalic
	if italicChanged && !toItalic {
		b = b.Italic(false)
	}

	underlineChanged := fromUnderline != toUnderline || from.Underline != to.Underline
	if underlineChanged && !toUnderline {
		b = b.Underline(false)
	}

	blinkChanged := fromBlink != toBlink
	rapidBlinkChanged := fromRapidBlink != toRapidBlink
	if blinkChanged || rapidBlinkChanged {
		if fromBlink && !toBlink || fromRapidBlink && !toRapidBlink {
			b = b.Blink(false)
			blinkChanged = true
			rapidBlinkChanged = true
		}
	}

	reverseChanged := fromReverse != toReverse
	if reverseChanged && !toReverse {
		b = b.Reverse(false)
	}

	concealChanged := fromConceal != toConceal
	if concealChanged && !toConceal {
		b = b.Conceal(false)
	}

	strikethroughChanged := fromStrikethrough != toStrikethrough
	if strikethroughChanged && !toStrikethrough {
		b = b.Strikethrough(false)
	}

	if boldChanged && toBold {
		b = b.Bold()
	}

	if faintChanged && toFaint {
		b = b.Faint()
	}

	if italicChanged && toItalic {
		b = b.Italic(true)
	}

	if underlineChanged && toUnderline && to.Underline == UnderlineStyleSingle {

		b = b.Underline(true)
	}

	if blinkChanged && toBlink {
		b = b.Blink(true)
	}

	if rapidBlinkChanged && toRapidBlink {
		b = b.RapidBlink(true)
	}

	if reverseChanged && toReverse {
		b = b.Reverse(true)
	}

	if concealChanged && toConceal {
		b = b.Conceal(true)
	}

	if strikethroughChanged && toStrikethrough {
		b = b.Strikethrough(true)
	}

	if underlineChanged && toUnderline && to.Underline > UnderlineStyleSingle {
		b = b.UnderlineStyle(to.Underline)
	}

	return b.String()
}

func colorEqual(c, o color.Color) bool {
	if c == nil && o == nil {
		return true
	}
	if c == nil || o == nil {
		return false
	}
	cr, cg, cb, ca := c.RGBA()
	or, og, ob, oa := o.RGBA()
	return cr == or && cg == og && cb == ob && ca == oa
}

func (s *Style) IsZero() bool {
	return *s == Style{}
}

func ConvertStyle(s Style, p colorprofile.Profile) Style {
	switch p {
	case colorprofile.TrueColor:
		return s
	case colorprofile.ANSI, colorprofile.ANSI256:
	case colorprofile.Ascii:
		s.Fg = nil
		s.Bg = nil
		s.UnderlineColor = nil
	case colorprofile.NoTTY:
		return Style{}
	}

	if s.Fg != nil {
		s.Fg = p.Convert(s.Fg)
	}
	if s.Bg != nil {
		s.Bg = p.Convert(s.Bg)
	}
	if s.UnderlineColor != nil {
		s.UnderlineColor = p.Convert(s.UnderlineColor)
	}
	return s
}

func ConvertLink(h Link, p colorprofile.Profile) Link {
	if p == colorprofile.NoTTY {
		return Link{}
	}
	return h
}
