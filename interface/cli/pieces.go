package cli

import (
	"strconv"
	"strings"

	"tofu/interface/tui/look"
	"tofu/internal/konst"
	"tofu/internal/widget"
)

const (
	Gap          = "  "
	WarnFraction = 0.5
	FullFraction = 0.8
)

const (
	indent          = "  "
	nameCells       = 16
	factLabelCells  = 10
	detailSeparator = " · "
	cardTop         = "┌ "
	cardSide        = "│ "
	cardFoot        = "└"
	cardRule        = "─"
)

type Mark int

const (
	None Mark = iota
	Done
	Active
	Idle
	Warn
	Fail
	Added
	Removed
	Changed
)

type Verdict struct {
	Mark Mark
	Text string
}

type Fact struct {
	Label string
	Text  string
}

type Row struct {
	Mark   Mark
	Cells  []string
	Detail string
	Hint   string
}

func (m Mark) look() (string, look.Color) {
	switch m {
	case None:
		return " ", look.Text
	case Done:
		return "✓", look.Mint
	case Active:
		return "●", look.Mint
	case Idle:
		return "○", look.MutedColor
	case Warn:
		return "⚠", look.Amber
	case Fail:
		return "✗", look.Red
	case Added:
		return "+", look.Mint
	case Removed:
		return "-", look.Red
	case Changed:
		return "~", look.Amber
	}
	panic("cli: unknown mark " + strconv.Itoa(int(m)))
}

func (p Page) Glyph(m Mark) string {
	glyph, colour := m.look()
	return p.paint(look.Style(colour), glyph)
}

func (p Page) verdict(v Verdict) string {
	if v.Mark == None {
		return v.Text
	}
	glyph, colour := v.Mark.look()
	return p.paint(look.Style(colour), glyph+" "+v.Text)
}

func (p Page) Title(subject string, facts []string, v Verdict) []string {
	left := p.Subject(subject)
	if len(facts) > 0 {
		left += p.Label(detailSeparator + strings.Join(facts, detailSeparator))
	}
	right := p.verdict(v)
	if room := p.Width - widget.Cells(left) - widget.Cells(right); room >= len(Gap) {
		return []string{left + strings.Repeat(" ", room) + right}
	}
	return []string{left, right}
}

func (p Page) Section(name string, v Verdict) string {
	if v.Mark == None {
		return p.Subject(name)
	}
	return widget.Pad(p.Subject(name), max(nameCells, widget.Cells(name)+len(Gap))) + p.verdict(v)
}

func (p Page) Status(name string, v Verdict) string {
	return indent + widget.Pad(p.Label(name), max(nameCells-len(indent), widget.Cells(name)+len(Gap))) + p.verdict(v)
}

func (p Page) Facts(facts []Fact) []string {
	column := factLabelCells
	for _, fact := range facts {
		column = max(column, widget.Cells(fact.Label)+len(Gap))
	}
	var lines []string
	for _, fact := range facts {
		if fact.Text != "" {
			lines = append(lines, widget.Pad(p.Label(fact.Label), column)+fact.Text)
		}
	}
	return lines
}

func (p Page) Card(head string, v Verdict, body []string) []string {
	width := p.Width - len(indent)
	fill := width - widget.Cells(cardTop) - widget.Cells(head) - 1
	right := ""
	if v.Mark != None {
		right = " " + p.verdict(v)
		fill -= widget.Cells(right)
	}
	lines := []string{indent + p.rule(cardTop) + head + " " + p.rule(strings.Repeat(cardRule, max(fill, 1))) + right}
	for _, line := range body {
		lines = append(lines, indent+p.rule(cardSide)+widget.Fit(line, width-widget.Cells(cardSide)))
	}
	return append(lines, indent+p.rule(cardFoot+strings.Repeat(cardRule, width-1)))
}

func (p Page) rule(text string) string { return p.paint(look.Style(look.FaintColor), text) }

func (p Page) Bar(fraction float64) string {
	colour := look.Mint
	switch {
	case fraction >= FullFraction:
		colour = look.Red
	case fraction >= WarnFraction:
		colour = look.Amber
	}
	filled := min(max(int(fraction*konst.MeterBarWidthChars+0.5), 0), konst.MeterBarWidthChars)
	return p.paint(look.Style(colour), widget.Bar(1, filled)) + p.rule(widget.Bar(0, konst.MeterBarWidthChars-filled)) +
		" " + widget.Lead(widget.Percent(fraction), len("100%"))
}

func (p Page) Rows(rows []Row) []string {
	var widths []int
	for _, row := range rows {
		for i, cell := range row.Cells {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], widget.Cells(cell))
		}
	}
	var lines []string
	for _, row := range rows {
		line := p.Glyph(row.Mark) + " "
		for i, cell := range row.Cells {
			if i > 0 {
				line += Gap
			}
			line += widget.Pad(cell, widths[i])
		}
		switch {
		case row.Hint != "":
			line += Gap + p.Hint(row.Hint)
		case row.Detail != "":
			line += Gap + p.Label(row.Detail)
		}
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

func (p Page) Steps(steps []string) []string {
	lines := make([]string, len(steps))
	for i, step := range steps {
		lines[i] = indent + p.Label(strconv.Itoa(i+1)) + Gap + step
	}
	return lines
}

func (p Page) Hint(text string) string { return p.paint(look.Style(look.Blue), "→ "+text) }

func (p Page) ErrorLine(what, hint string) []string {
	lines := []string{p.verdict(Verdict{Fail, what})}
	if hint != "" {
		lines = append(lines, indent+p.Hint(hint))
	}
	return lines
}

func (p Page) Receipt(m Mark, text, path string) string {
	return p.Glyph(m) + " " + text + Gap + p.Label(p.Path(path))
}

func Indent(lines ...string) []string {
	indented := make([]string, len(lines))
	for i, line := range lines {
		indented[i] = indent + line
	}
	return indented
}
