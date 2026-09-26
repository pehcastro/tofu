package session

import "slices"

const waitingWord = "waiting"

type pending struct {
	id   string
	task string
}

func (m *Model) Queue(shown, whole string, chips []Chip) {
	m.Append(Entry{Kind: User, Body: shown, Chips: chips, waiting: true})
	m.queue = append(m.queue, pending{id: m.entries[len(m.entries)-1].ID, task: whole})
	m.pick = len(m.queue) - 1
}

func (m *Model) Queued() []string {
	tasks := make([]string, 0, len(m.queue))
	for _, row := range m.queue {
		tasks = append(tasks, row.task)
	}
	return tasks
}

func (m *Model) queuedAt(id string) int {
	return slices.IndexFunc(m.entries, func(entry Entry) bool { return entry.waiting && entry.ID == id })
}

func (m *Model) Release() (string, bool) {
	if len(m.queue) == 0 {
		return "", false
	}
	first := m.queue[0]
	m.queue = m.queue[1:]
	m.pick = max(m.pick-1, 0)
	if at := m.queuedAt(first.id); at >= 0 {
		m.entries[at].waiting = false
		m.revision++
	}
	return first.task, true
}

func (m *Model) Delivered(task string) {
	if len(m.queue) > 0 && m.queue[0].task == task {
		m.Release()
	}
}

func (m *Model) Unqueue() {
	if len(m.queue) == 0 {
		return
	}
	at := min(m.pick, len(m.queue)-1)
	id := m.queue[at].id
	m.queue = slices.Delete(m.queue, at, at+1)
	m.pick = min(at, max(len(m.queue)-1, 0))
	row := m.queuedAt(id)
	if row < 0 {
		return
	}
	m.entries = slices.Delete(m.entries, row, row+1)
	m.revision++
	if m.top.entry > row {
		m.top.entry--
	}
}

func (m *Model) PickQueued(by int) {
	if len(m.queue) == 0 {
		return
	}
	m.pick = (min(m.pick, len(m.queue)-1) + by + len(m.queue)) % len(m.queue)
	m.revision++
}

func (m *Model) pickedQueue() string {
	if len(m.queue) == 0 {
		return ""
	}
	return m.queue[min(m.pick, len(m.queue)-1)].id
}
