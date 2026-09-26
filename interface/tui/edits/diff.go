package edits

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/trace"
)

const (
	rowCacheBound   = 256
	wrapFloor       = 12
	wrapGutter      = 2
	gutterRoom      = 3
	numberDigits    = 4
	diffMinWidth    = 24
	diffMinHeight   = 6
	diffChrome      = 6
	diffFloor       = 2
	compactPosition = 90
	dialogMargin    = 8
	dialogMaxInner  = 100
	dialogPadding   = 4
	dialogMinBody   = 20
	dialogChrome    = 12
	dialogMinRows   = 4
	missingWidth    = 62
	dialogTitle     = "File change"
	missingBody     = "This diff is no longer available."
	editKind        = "edit"
)

type diffFrame struct {
	height, scroll, index, count int
	wide                         bool
	path                         string
	op                           Op
	added, removed               int
}

type dialogRows struct {
	id     string
	width  int
	source []string
	rows   map[int]string
}

type diffRowIndex struct {
	id      string
	width   int
	source  []string
	offsets []int
	wrapped map[int][]string
	painted map[int]string
	dialog  *dialogRows
	frame   diffFrame
	view    string
}

func (index *diffRowIndex) matches(edit Edit, width int) bool {
	return index.id == edit.ID && index.width == width && slices.Equal(index.source, edit.lines)
}

func (m Model) indexedDiffRows(edit Edit, width int) []int {
	if m.cache != nil && m.cache.rows.matches(edit, width) {
		return m.cache.rows.offsets
	}
	wrap := max(wrapFloor, width-wrapGutter)
	gutter := gutterRoom + max(numberDigits, len(strconv.Itoa(slices.Max(edit.numbers))))
	offsets := make([]int, len(edit.lines)+1)
	for at, line := range edit.lines {
		control := strings.ContainsAny(line, "\r\x1b")
		if !control && len(line)+gutter <= wrap {
			offsets[at+1] = offsets[at] + 1
			continue
		}
		plain := plainLine(line, edit.numbers[at])
		if control {
			plain = diffLine(line, edit.numbers[at], m.lexer(edit.Path))
		}
		offsets[at+1] = offsets[at] + strings.Count(ansi.Wrap(plain, wrap, ""), "\n") + 1
	}
	if m.cache != nil {
		m.cache.rows = diffRowIndex{id: edit.ID, width: width, source: slices.Clone(edit.lines), offsets: offsets, wrapped: map[int][]string{}, painted: map[int]string{}}
	}
	return offsets
}

type window struct{ total, visible, from int }

func scrolled(total, visible, scroll int) window {
	return window{total: total, visible: visible, from: max(0, total-visible-min(max(0, scroll), max(0, total-visible)))}
}

func (m Model) diffWindow(edit Edit, width, height int) ([]int, window) {
	offsets := m.indexedDiffRows(edit, width)
	return offsets, scrolled(offsets[len(offsets)-1], max(diffFloor, height-diffChrome), m.scroll)
}

func (m Model) completeFileDiff(width, height int, edit Edit, index, count int) string {
	width, height = max(diffMinWidth, width), max(diffMinHeight, height)
	frame := diffFrame{height: height, scroll: m.scroll, index: index, count: count, wide: m.wide, path: edit.Path, op: edit.op, added: edit.added, removed: edit.removed}
	if m.cache != nil && m.cache.rows.matches(edit, width) && m.cache.rows.view != "" && m.cache.rows.frame == frame {
		return m.cache.rows.view
	}
	offsets, shown := m.diffWindow(edit, width, height)
	to := min(shown.total, shown.from+shown.visible)
	first, last := 0, 0
	if shown.from < to {
		first = sort.Search(len(edit.lines), func(at int) bool { return offsets[at+1] > shown.from }) + 1
		last = sort.Search(len(edit.lines), func(at int) bool { return offsets[at+1] >= to }) + 1
	}
	rows := make([]string, 0, shown.visible)
	for at := max(0, first-1); at < len(edit.lines) && offsets[at] < to; at++ {
		for sub, row := range m.wrappedDiffRows(edit, at, width) {
			if visual := offsets[at] + sub; visual >= shown.from && visual < to {
				rows = append(rows, m.paintedDiffRow(edit.lines[at], row, visual, width))
			}
		}
	}
	word, _, colour := edit.op.mark()
	header := look.Sides(look.Title(edit.Path), look.TypedID(editKind, trace.Short(edit.ID)), width-1)
	sub := look.Style(colour).Bold(true).Render(strings.ToUpper(word)) + look.Faint(fmt.Sprintf("  ·  %d/%d files  ·  ", index+1, count)) + edit.meta()
	action := "f full width"
	if m.wide {
		action = "f sidebar"
	}
	position := fmt.Sprintf("lines %d-%d / %d  ·  PgUp/PgDn scroll  ·  n/p file  ·  %s", first, last, len(edit.lines), action)
	if width < compactPosition {
		position = fmt.Sprintf("%d-%d/%d  ·  PgUp/PgDn · n/p", first, last, len(edit.lines))
	}
	content := strings.Join(append(rows, make([]string, shown.visible-len(rows))...), "\n")
	viewport, fast := look.PlainSurfaceFast(width-1, shown.visible, 0, content)
	if !fast {
		viewport = lipgloss.NewStyle().Width(width - 1).Height(shown.visible).Render(content)
	}
	view := header + "\n" + sub + "\n" + look.Faint(position) + "\n\n" + viewport
	if m.cache != nil {
		m.cache.rows.frame, m.cache.rows.view = frame, view
	}
	return view
}

func (m Model) wrappedDiffRows(edit Edit, at, width int) []string {
	if m.cache != nil {
		if rows, ok := m.cache.rows.wrapped[at]; ok {
			return rows
		}
	}
	rows := strings.Split(lipgloss.Wrap(diffLine(edit.lines[at], edit.numbers[at], m.lexer(edit.Path)), max(wrapFloor, width-wrapGutter), ""), "\n")
	if m.cache != nil {
		if len(m.cache.rows.wrapped) >= rowCacheBound {
			clear(m.cache.rows.wrapped)
		}
		m.cache.rows.wrapped[at] = rows
	}
	return rows
}

func (m Model) paintedDiffRow(line, row string, visual, width int) string {
	if m.cache != nil {
		if painted, ok := m.cache.rows.painted[visual]; ok {
			return painted
		}
	}
	background := look.Color("")
	switch {
	case strings.HasPrefix(line, addedMark):
		background = look.DiffAddBackground
	case strings.HasPrefix(line, removedMark):
		background = look.DiffDeleteBackground
	}
	if background != "" {
		row = lipgloss.NewStyle().Width(width - 1).Background(lipgloss.Color(string(background))).Render(look.KeepSurfaceBackground(row, background))
	}
	if m.cache != nil {
		if len(m.cache.rows.painted) >= rowCacheBound {
			clear(m.cache.rows.painted)
		}
		m.cache.rows.painted[visual] = row
	}
	return row
}

func (m Model) Panel(width, height int, id string, scroll int) string {
	at := slices.IndexFunc(m.edits, func(edit Edit) bool { return edit.ID == id || trace.Short(edit.ID) == id })
	if at < 0 {
		return look.DialogPanel(min(missingWidth, width-dialogMargin), dialogTitle, "", look.Muted(missingBody), "esc close")
	}
	edit := m.shown(m.edits[at])
	inner := max(diffMinWidth, min(dialogMaxInner, width-dialogMargin))
	content, rows := max(dialogMinBody, inner-dialogPadding), max(dialogMinRows, height-dialogChrome)
	start := min(max(0, scroll), max(0, len(edit.lines)-rows))
	ending := min(len(edit.lines), start+rows)
	lines := m.dialogRows(edit, content, start, ending)
	view := strings.Join(append(lines, make([]string, rows-len(lines))...), "\n")
	word, _, colour := edit.op.mark()
	heading := look.Sides(look.Title(dialogTitle), look.TypedID(editKind, trace.Short(edit.ID)), content)
	meta := look.Style(colour).Bold(true).Render(strings.ToUpper(word)) + "  " + look.Muted(edit.Path) + "\n" + deltas(edit.added, edit.removed) + look.Muted(" lines")
	footer := look.Faint(fmt.Sprintf("lines %d-%d/%d · PgUp/PgDn scroll · ", start+1, ending, len(edit.lines))) + look.Accent("Close")
	return look.TintedSurface(inner, look.Panel, heading+"\n"+meta+"\n\n"+view+"\n"+footer)
}

func (m Model) dialogRows(edit Edit, width, start, ending int) []string {
	var cache *dialogRows
	if m.cache != nil {
		cache = m.cache.rows.dialog
		if cache == nil || cache.id != edit.ID || cache.width != width || !slices.Equal(cache.source, edit.lines) {
			cache = &dialogRows{id: edit.ID, width: width, source: slices.Clone(edit.lines), rows: map[int]string{}}
			m.cache.rows.dialog = cache
		}
	}
	lines := make([]string, 0, ending-start)
	for at := start; at < ending; at++ {
		if cache != nil {
			if row, ok := cache.rows[at]; ok {
				lines = append(lines, row)
				continue
			}
		}
		row := ansi.Truncate(diffLine(edit.lines[at], edit.numbers[at], m.lexer(edit.Path)), width, "…")
		if cache != nil {
			if len(cache.rows) >= rowCacheBound {
				clear(cache.rows)
			}
			cache.rows[at] = row
		}
		lines = append(lines, row)
	}
	return lines
}

func plainLine(line string, number int) string {
	if !numbered(line) {
		return line
	}
	return fmt.Sprintf("%s %4d  %s", line[:1], number, line[1:])
}

func numbered(line string) bool {
	return strings.HasPrefix(line, addedMark) || strings.HasPrefix(line, removedMark) || strings.HasPrefix(line, contextMark)
}

func diffLine(line string, number int, lexer chroma.Lexer) string {
	if !numbered(line) {
		return look.Faint(line)
	}
	sign, base := line[:1], look.MutedColor
	switch sign {
	case addedMark:
		sign, base = look.Accent(sign), look.Text
	case removedMark:
		sign, base = look.Style(look.Red).Render(sign), look.Text
	}
	return sign + " " + look.Faint(fmt.Sprintf("%4d", number)) + "  " + syntax(line[1:], base, lexer)
}

func syntax(code string, base look.Color, lexer chroma.Lexer) string {
	tokens, err := lexer.Tokenise(nil, code)
	if err != nil {
		return look.Style(base).Render(code)
	}
	var out, run strings.Builder
	current := base
	for token := tokens(); token != chroma.EOF; token = tokens() {
		colour := role(token.Type, base)
		if colour != current && run.Len() > 0 {
			out.WriteString(look.Style(current).Render(run.String()))
			run.Reset()
		}
		current = colour
		run.WriteString(strings.ReplaceAll(token.Value, "\n", ""))
	}
	out.WriteString(look.Style(current).Render(run.String()))
	return out.String()
}

func role(token chroma.TokenType, base look.Color) look.Color {
	switch {
	case token.InCategory(chroma.Keyword):
		return look.SyntaxKeyword
	case token.InCategory(chroma.Comment):
		return look.SyntaxComment
	case token.InSubCategory(chroma.LiteralString):
		return look.SyntaxString
	case token.InSubCategory(chroma.LiteralNumber):
		return look.SyntaxNumber
	case token == chroma.NameFunction || token == chroma.NameFunctionMagic:
		return look.SyntaxFunction
	}
	return base
}
