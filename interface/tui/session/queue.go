package session

import (
	"slices"
	"strconv"
	"strings"

	"tofu/interface/tui/look"
	"tofu/internal/widget"
)

const (
	queueShownRows = 4
	queueHead      = "queued "
	queueHeadTail  = " · the lead reads them as one message at its next step"
	takenJoin      = "\n\n"
)

type pending struct {
	task  string
	shown string
	chips []Chip
}

func (m *Model) Queue(shown, whole string, chips []Chip) {
	m.queue = append(m.queue, pending{task: whole, shown: shown, chips: chips})
	m.pick = len(m.queue) - 1
	m.revision++
}

func (m *Model) Queued() []string {
	tasks := make([]string, 0, len(m.queue))
	for _, row := range m.queue {
		tasks = append(tasks, row.task)
	}
	return tasks
}

func (m *Model) Release() (string, bool) {
	if len(m.queue) == 0 {
		return "", false
	}
	tasks := m.Queued()
	m.Append(Entry{Kind: User})
	for _, row := range m.queue {
		m.joinTaken(&m.entries[len(m.entries)-1], row)
	}
	m.queue, m.pick = nil, 0
	return strings.Join(tasks, takenJoin), true
}

func (m *Model) Delivered(task, read string) {
	at := slices.IndexFunc(m.queue, func(row pending) bool { return row.task == task })
	if at < 0 {
		return
	}
	row := m.queue[at]
	m.queue = slices.Delete(m.queue, at, at+1)
	if at < m.pick {
		m.pick--
	}
	m.pick = min(m.pick, max(len(m.queue)-1, 0))
	last := len(m.entries) - 1
	if last < 1 || !m.entries[last-1].taken || m.entries[last].Kind != Note || m.entries[last].Body != read {
		m.Append(Entry{Kind: User, taken: true})
		m.Append(Entry{Kind: Note, Body: read})
		last = len(m.entries) - 1
	}
	m.joinTaken(&m.entries[last-1], row)
}

func (m *Model) joinTaken(block *Entry, row pending) {
	if block.Body != "" {
		block.Body += takenJoin
	}
	block.Body += row.shown
	block.Chips = append(block.Chips, row.chips...)
	m.revision++
}

func (m *Model) PickedQueued() (string, bool) {
	if len(m.queue) == 0 {
		return "", false
	}
	return m.queue[m.pick].task, true
}

func (m *Model) Unqueue() {
	if len(m.queue) == 0 {
		return
	}
	m.queue = slices.Delete(m.queue, m.pick, m.pick+1)
	m.pick = min(m.pick, max(len(m.queue)-1, 0))
	m.revision++
}

func (m *Model) PickQueued(by int) {
	if len(m.queue) == 0 {
		return
	}
	m.pick = (m.pick + by + len(m.queue)) % len(m.queue)
	m.revision++
}

func (m *Model) queueLines() []string {
	if len(m.queue) == 0 {
		return nil
	}
	lines := []string{margin + look.Faint(queueHead+strconv.Itoa(len(m.queue))+queueHeadTail)}
	start := min(max(len(m.queue)-queueShownRows, 0), m.pick)
	room := max(m.width-len(margin)-widget.Cells(pickedMarker), 1)
	for index, row := range m.queue[start:min(start+queueShownRows, len(m.queue))] {
		marker, text := unpickedMarker, look.Muted(widget.Fit(oneLine(row.shown), room))
		if start+index == m.pick {
			marker = look.Accent(pickedMarker)
		}
		lines = append(lines, margin+marker+text)
	}
	return lines
}
