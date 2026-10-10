package feed

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"tofu/interface/tui/pointer"
)

func (m *Model) Key(key string) bool {
	switch {
	case key == "right" || key == "tab" && m.railFocused:
		m.railFocused = false
	case key == "left" || (key == "shift+tab" || key == "esc") && !m.railFocused:
		m.railFocused = true
	case key == "pgup":
		m.scrollBy(m.pageHeight())
	case key == "pgdown":
		m.scrollBy(-m.pageHeight())
	case key == "n":
		m.step(1, true)
	case key == "p":
		m.step(-1, true)
	case (key == "up" || key == "k") && m.railFocused:
		m.moveRail(-1)
	case (key == "down" || key == "j") && m.railFocused:
		m.moveRail(1)
	case key == "up" || key == "k":
		m.step(-1, false)
	case key == "down" || key == "j":
		m.step(1, false)
	case (key == "enter" || key == "space") && !m.railFocused && m.selected != "":
		m.expanded[m.selected], m.version = !m.expanded[m.selected], m.version+1
	default:
		return false
	}
	return true
}

func (m *Model) Wheel(delta int) {
	m.scrollBy(-delta * wheelRows)
}

func (m *Model) SetScroll(behindNewest int) {
	if behindNewest >= m.laid(m.depth(), "").rows-m.pageHeight() {
		behindNewest = math.MaxInt32
	}
	m.scrollBy(behindNewest - m.scroll)
}

func (m *Model) scrollBy(rows int) {
	target := max(0, m.scroll+rows)
	total := m.laid(target+2*m.pageHeight(), "").rows
	m.scroll = max(0, min(total-m.pageHeight(), target))
	m.railFocused = false
}

func (m *Model) Click(x, y int) (reference string, handled bool) {
	split := m.Split()
	if x < split {
		_, targets := m.railView()
		for _, t := range targets {
			if t.row == y {
				m.filter, m.scroll, m.railFocused = t.who, 0, true
				return "", true
			}
		}
		return "", false
	}
	if ref := pointer.ReferenceAt(m.View(), x, y); ref != "" {
		if kind, _ := pointer.SplitReference(ref); kind != "" {
			return ref, true
		}
		name, instance, _ := strings.Cut(strings.Trim(ref, "[&]"), " ")
		number, _ := strconv.Atoi(instance)
		if m.SelectAgent(name, number) {
			return "", true
		}
	}
	left := split + panePadding
	if x < left || x >= left+max(cardMinWidth, m.feedWidth()-2) {
		return "", false
	}
	for _, h := range m.page().hits {
		if y-headerRows >= h.start && y-headerRows < h.end {
			m.selected, m.railFocused = h.id, false
			return "", true
		}
	}
	return "", false
}

func (m *Model) Focus(id string) bool {
	i := slices.IndexFunc(m.events, func(e Event) bool { return e.ID == id })
	if i < 0 {
		return false
	}
	if !m.shows(m.events[i]) {
		m.filter = identity{}
	}
	m.reveal(id)
	return true
}

func (m *Model) SelectAgent(name string, instance int) bool {
	who := identity{name, instance}
	if who != (identity{}) && !slices.ContainsFunc(m.entries(), func(e entry) bool { return e.who == who }) {
		return false
	}
	m.filter, m.scroll, m.railFocused = who, 0, true
	return true
}

func (m *Model) moveRail(delta int) {
	order := []identity{{}}
	for _, e := range m.entries() {
		order = append(order, e.who)
	}
	at := max(0, slices.Index(order, m.filter))
	m.filter, m.scroll = order[(at+delta+len(order))%len(order)], 0
}

func (m *Model) step(delta int, wrap bool) {
	events := m.visible()
	if len(events) == 0 {
		return
	}
	at := slices.IndexFunc(events, func(e Event) bool { return e.ID == m.selected })
	if at < 0 && delta < 0 {
		at = len(events)
	}
	next := max(0, min(len(events)-1, at+delta))
	if wrap {
		next = (at + delta + len(events)) % len(events)
	}
	m.reveal(events[next].ID)
}

func (m *Model) reveal(id string) {
	m.selected, m.railFocused = id, false
	s := m.laid(m.depth(), id)
	if i := s.index(id); i >= 0 {
		m.scroll = max(0, min(s.rows-m.pageHeight(), s.rows-s.starts[i]-s.heights[i]))
	}
}
