package feed

import (
	"slices"
)

type sheetKey struct {
	version, width, gap int
	filter              identity
	thinkingHidden      bool
	retention           Retention
}

type sheet struct {
	key             sheetKey
	about           []Event
	kept            int
	unheld          [][]int64
	unheldAll       [][]int64
	drafts          []draft
	heights, starts []int
	frontier        int
	measured        int
	rows            int
}

func (m Model) sheet() *sheet {
	about, lead := m.aboutCards()
	key := sheetKey{m.version, m.feedWidth(), m.gap, m.filter, m.thinkingHidden, m.retention}
	prior := m.cards.sheet
	if prior != nil && prior.key == key && slices.EqualFunc(prior.about, about, func(a, b Event) bool { return sameEvent(&a, &b) }) {
		return prior
	}
	kept, s := m.retained(), &sheet{key: key, about: about}
	s.unheld, s.unheldAll = m.unheldShown()
	var earlier []draft
	if prior != nil {
		earlier, s.drafts = prior.drafts, make([]draft, 0, len(prior.drafts))
	}
	add := func(e *Event, lines func(*cardCache, Event, cardKey) []string) {
		d := draft{event: e, id: e.ID, lines: lines}
		if at := len(s.drafts); at < len(earlier) && earlier[at].id == d.id {
			d.held = earlier[at].held
		}
		s.drafts = append(s.drafts, d)
	}
	for index := range about[:lead] {
		add(&about[index], agentCardLines)
	}
	for index := range kept {
		e := &kept[index]
		if m.hidden(e) {
			continue
		}
		s.kept++
		if m.shows(e) {
			add(e, (*cardCache).cardLines)
		}
	}
	for index := range about[lead:] {
		add(&about[lead+index], agentCardLines)
	}
	s.heights, s.starts, s.frontier = make([]int, len(s.drafts)), make([]int, len(s.drafts)), len(s.drafts)
	m.cards.sheet = s
	return s
}

func (s *sheet) index(id string) int {
	if id == "" {
		return -1
	}
	return slices.IndexFunc(s.drafts, func(d draft) bool { return d.id == id })
}

func (m Model) laid(depth int, through string) *sheet {
	s := m.sheet()
	stop := len(s.drafts)
	if at := s.index(through); at >= 0 {
		stop = at
	}
	moved, fresh := s.frontier, 0
	for s.frontier > 0 && (s.measured+(len(s.drafts)-s.frontier)*s.key.gap < depth || s.frontier > stop || fresh < measureBatch) {
		s.frontier--
		height, wrapped := m.sized(s.key.width, &s.drafts[s.frontier])
		if wrapped {
			fresh++
		}
		s.heights[s.frontier] = height
		s.measured += height
	}
	if moved == s.frontier && s.rows > 0 {
		return s
	}
	estimate := s.measured / max(1, len(s.drafts)-s.frontier)
	s.rows = 0
	for index := range s.drafts {
		if index > 0 {
			s.rows += s.key.gap
		}
		if index < s.frontier {
			s.heights[index] = estimate
		}
		s.starts[index] = s.rows
		s.rows += s.heights[index]
	}
	return s
}

func (m Model) depth() int { return m.scroll + 2*m.pageHeight() }
