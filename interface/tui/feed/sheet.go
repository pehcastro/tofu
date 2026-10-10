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
	drafts          []draft
	heights, starts []int
	frontier        int
	measured        int
	rows            int
}

func (m Model) sheet() *sheet {
	about, lead := m.aboutCards()
	key := sheetKey{m.version, m.feedWidth(), m.gap, m.filter, m.thinkingHidden, m.retention}
	if s := m.cards.sheet; s != nil && s.key == key && slices.EqualFunc(s.about, about, sameEvent) {
		return s
	}
	kept := m.retained()
	s := &sheet{key: key, about: about, kept: len(kept)}
	for index := range about[:lead] {
		s.drafts = append(s.drafts, draft{&about[index], agentCardLines})
	}
	for index := range kept {
		if m.shows(kept[index]) {
			s.drafts = append(s.drafts, draft{&kept[index], (*cardCache).cardLines})
		}
	}
	for index := range about[lead:] {
		s.drafts = append(s.drafts, draft{&about[lead+index], agentCardLines})
	}
	s.heights, s.starts, s.frontier = make([]int, len(s.drafts)), make([]int, len(s.drafts)), len(s.drafts)
	m.cards.sheet = s
	return s
}

func (s *sheet) index(id string) int {
	if id == "" {
		return -1
	}
	return slices.IndexFunc(s.drafts, func(d draft) bool { return d.event.ID == id })
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
		height, wrapped := m.sized(s.key.width, s.drafts[s.frontier])
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
