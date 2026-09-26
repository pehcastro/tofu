package session

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
	return !m.ChatShowsTools && entry.Kind == Tool && !entry.sticky()
}

func (m Model) FoldedOutOfChat(id string) bool {
	for index, entry := range m.entries {
		if entry.ID == id {
			return m.folds(index)
		}
	}
	return false
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

func (m *Model) stillRunning(index int) bool {
	return m.Busy && m.entries[index].turn == m.turns
}

func (m *Model) tailAnchor(rows int) (anchor, bool) {
	total := 0
	for index := len(m.entries) - 1; index >= 0; {
		start, end := m.blockAt(index)
		total += m.blockRows(start, end)
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
		rendered := m.blockLines(start, end)
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
		start, end := m.blockAt(at.entry - 1)
		at.entry, at.line = start, m.blockRows(start, end)
	}
	for lines > 0 && at.entry < len(m.entries) {
		start, end := m.blockAt(at.entry)
		room := m.blockRows(start, end) - at.line
		if lines < room {
			at.line += lines
			return at
		}
		lines -= room
		at.entry, at.line = end, 0
	}
	return at
}

func (m *Model) offset(at anchor) int {
	lines := 0
	for index := 0; index < at.entry && index < len(m.entries); {
		start, end := m.blockAt(index)
		lines += m.blockRows(start, end)
		index = end
	}
	return lines + at.line
}

func (m *Model) Scroll(key string) (int, bool) {
	_, rows := m.feed()
	tail, scrollable := m.tailAnchor(rows)
	if !scrollable {
		return 0, false
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
		return 0, false
	}
	m.following = !to.before(tail)
	m.top = to
	if m.following {
		m.top = tail
	}
	return m.offset(from) - m.offset(m.top), true
}
