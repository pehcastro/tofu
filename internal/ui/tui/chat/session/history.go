package session

import (
	"slices"
	"strings"
)

type sentEntry struct {
	text  string
	chips []Chip
}

func (m *Model) Remember(task string) []Chip {
	kept := make([]Chip, 0, len(m.chips))
	for _, chip := range m.chips {
		if strings.Contains(task, chip.Token) {
			kept = append(kept, chip)
		}
	}
	m.sent = append(m.sent, sentEntry{text: task, chips: kept})
	m.histAt = len(m.sent)
	m.chips = nil
	return kept
}

func (m *Model) recallable() bool { return m.composer.LineCount() <= 1 }

func (m *Model) HistoryUp() bool {
	if !m.recallable() || m.histAt == 0 {
		return false
	}
	if m.histAt == len(m.sent) {
		m.draft = sentEntry{text: m.composer.Value(), chips: m.chips}
	}
	m.histAt--
	m.recall(m.sent[m.histAt])
	return true
}

func (m *Model) HistoryDown() bool {
	if !m.recallable() || m.histAt >= len(m.sent) {
		return false
	}
	m.histAt++
	if m.histAt == len(m.sent) {
		m.recall(m.draft)
		return true
	}
	m.recall(m.sent[m.histAt])
	return true
}

func (m *Model) recall(entry sentEntry) {
	m.composer.SetValue(entry.text)
	m.composer.CursorEnd()
	m.chips = slices.Clone(entry.chips)
	m.closed, m.picked = false, 0
}
