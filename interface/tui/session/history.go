package session

import "strings"

func (m *Model) Remember(task string) []Chip {
	m.sent = append(m.sent, task)
	m.histAt = len(m.sent)
	kept := make([]Chip, 0, len(m.chips))
	for _, chip := range m.chips {
		if strings.Contains(task, chip.Token) {
			kept = append(kept, chip)
		}
	}
	m.chips = nil
	return kept
}

func (m *Model) recallable() bool { return m.composer.LineCount() <= 1 }

func (m *Model) HistoryUp() bool {
	if !m.recallable() || m.histAt == 0 {
		return false
	}
	if m.histAt == len(m.sent) {
		m.draft = m.composer.Value()
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

func (m *Model) recall(text string) {
	m.composer.SetValue(text)
	m.composer.CursorEnd()
	m.closed, m.picked = false, 0
}
