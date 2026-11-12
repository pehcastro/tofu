package session

import (
	"strings"

	"tofu/internal/ui/tui/input/pointer"
)

const (
	WheelUp    = "wheel up"
	WheelDown  = "wheel down"
	wheelLines = 3
)

type anchor struct {
	entry int
	line  int
}

func (a anchor) before(other anchor) bool {
	return a.entry < other.entry || (a.entry == other.entry && a.line < other.line)
}

func (m *Model) folds(index int) bool {
	entry := &m.entries[index]
	return !m.ChatShowsTools && entry.Kind == Tool && !entry.Promoted
}

func (m *Model) blockAt(index int) (int, int) {
	if !m.folds(index) {
		return index, index + 1
	}
	start, end := index, index+1
	for start > 0 && m.folds(start-1) {
		start--
	}
	for end < len(m.entries) && m.folds(end) {
		end++
	}
	return start, end
}

func (m *Model) tailAnchor(rows int) (anchor, bool) {
	total := 0
	for index := len(m.entries) - 1; index >= 0; {
		start, _ := m.blockAt(index)
		total += len(m.blockLines(start))
		if total >= rows {
			return anchor{entry: start, line: total - rows}, total > rows || start > 0
		}
		index = start - 1
	}
	return anchor{}, false
}

func (m *Model) linesFrom(at anchor, rows int) []string {
	lines := make([]string, 0, rows)
	skip := at.line
	for index := at.entry; index < len(m.entries) && len(lines) < rows; {
		start, end := m.blockAt(index)
		rendered := m.blockLines(start)
		index = end
		if skip >= len(rendered) {
			skip -= len(rendered)
			continue
		}
		lines = append(lines, rendered[skip:]...)
		skip = 0
	}
	if len(lines) > rows {
		return lines[:rows]
	}
	return lines
}

func (m *Model) move(at anchor, lines int) anchor {
	for lines < 0 {
		if at.line > 0 {
			step := min(-lines, at.line)
			at.line, lines = at.line-step, lines+step
			continue
		}
		if at.entry == 0 {
			return anchor{}
		}
		start, _ := m.blockAt(at.entry - 1)
		at.entry, at.line = start, len(m.blockLines(start))
	}
	for lines > 0 && at.entry < len(m.entries) {
		start, end := m.blockAt(at.entry)
		room := len(m.blockLines(start)) - at.line
		if lines < room {
			at.line += lines
			return at
		}
		lines -= room
		at.entry, at.line = end, 0
	}
	return at
}

type rowKey struct {
	revision, width, turns, entries, lo int
	showsTools                          bool
}

type rowTable struct {
	key    rowKey
	before []int
}

func (m *Model) rowKey() rowKey {
	return rowKey{m.revision, m.width, m.turns, len(m.entries), m.lo, m.ChatShowsTools}
}

func (m *Model) offset(at anchor) int {
	lo, hi := m.window()
	if m.rows.before == nil || m.rows.key != m.rowKey() {
		before := make([]int, hi-lo+1)
		for index := lo; index < hi; {
			start, end := m.blockAt(index)
			start, end = max(start, lo), min(end, hi)
			rows := m.lead(start) + 1 + len(m.entries[start].displayLines())
			if m.wrapped(&m.entries[start]) {
				rows = len(m.blockLines(start))
			}
			for inside := start + 1; inside <= end; inside++ {
				before[inside-lo] = before[start-lo] + rows
			}
			index = end
		}
		m.rows = rowTable{key: m.rowKey(), before: before}
	}
	return m.rows.before[min(max(at.entry, lo), hi)-lo] + at.line
}

func (m *Model) Scroll(key string) bool {
	rows := m.transcriptRows()
	tail, scrollable := m.tailAnchor(rows)
	if !scrollable {
		return false
	}
	from := m.top
	if m.following {
		from = tail
	}
	editing := m.composer.Value() != ""
	var to anchor
	switch {
	case key == "pgup":
		to = m.move(from, -rows)
	case key == "pgdown":
		to = m.move(from, rows)
	case key == WheelUp:
		to = m.move(from, -wheelLines)
	case key == WheelDown:
		to = m.move(from, wheelLines)
	case key == "home" && !editing:
		to = anchor{}
	case key == "end" && !editing:
		to = tail
	default:
		return false
	}
	m.landAt(to, tail)
	return true
}

func (m *Model) landAt(to, tail anchor) {
	m.following = !to.before(tail)
	m.top = to
	if m.following {
		m.top = tail
	}
	m.frameAround(m.top.entry)
}

func (m *Model) SetScroll(behindNewest int) {
	rows := m.transcriptRows()
	if tail, scrollable := m.tailAnchor(rows); scrollable {
		m.landAt(m.move(tail, -max(0, behindNewest)), tail)
	}
}

type Point struct {
	at anchor
	x  int
}

func (m *Model) PointAt(x, row int) Point {
	rows := m.transcriptRows()
	_, _, top := m.scrollMetrics(rows)
	return Point{at: m.move(top, max(0, min(row, rows-1))), x: x}
}

func ordered(from, to Point) (Point, Point) {
	if to.at.before(from.at) || to.at == from.at && to.x < from.x {
		return to, from
	}
	return from, to
}

func (m *Model) Shown(from, to Point) pointer.Selection {
	from, to = ordered(from, to)
	rows := m.transcriptRows()
	_, fromTop, _ := m.scrollMetrics(rows)
	start, end := pointer.Cell{X: from.x, Y: m.offset(from.at) - fromTop}, pointer.Cell{X: to.x, Y: m.offset(to.at) - fromTop}
	if end.Y < 0 || start.Y >= rows {
		return pointer.Selection{}
	}
	if start.Y < 0 {
		start = pointer.Cell{}
	}
	if end.Y >= rows {
		end = pointer.Cell{X: m.width, Y: rows - 1}
	}
	return pointer.Selection{Start: start, End: end, Dragged: true}
}

func (m *Model) Between(from, to Point) string {
	from, to = ordered(from, to)
	var lines []string
	for index := from.at.entry; index < len(m.entries) && index <= to.at.entry; {
		start, end := m.blockAt(index)
		rendered := m.blockLines(start)
		low, high := 0, len(rendered)
		if start == to.at.entry {
			high = min(high, to.at.line+1)
		}
		if start == from.at.entry {
			low = min(high, from.at.line)
		}
		lines = append(lines, rendered[low:high]...)
		index = end
	}
	return pointer.SelectedText(strings.Join(lines, "\n"), pointer.Selection{Start: pointer.Cell{X: from.x}, End: pointer.Cell{X: to.x, Y: len(lines) - 1}, Dragged: true}, pointer.Pane{Width: m.width})
}

func (m *Model) Track() pointer.Track {
	rows := m.transcriptRows()
	total, fromTop, _ := m.scrollMetrics(rows)
	return pointer.Track{Height: rows, Total: total, Visible: rows, FromTop: fromTop}
}
