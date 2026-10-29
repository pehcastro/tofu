package session

import "tofu/interface/tui/pointer"

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

func (m *Model) Track() pointer.Track {
	rows := m.transcriptRows()
	total, fromTop, _ := m.scrollMetrics(rows)
	return pointer.Track{Height: rows, Total: total, Visible: rows, FromTop: fromTop}
}
