package host

import (
	"slices"
	"strings"
	"sync"

	"tofu/internal/session"
)

type steerQueue struct {
	mu       sync.Mutex
	ready    chan string
	overflow []string
	ids      []string
	now      []string
}

type steered struct {
	id   string
	text string
}

func (q *steerQueue) put(entry steered) {
	q.ids = append(q.ids, entry.id)
	if len(q.overflow) == 0 {
		select {
		case q.ready <- entry.text:
			return
		default:
		}
	}
	q.overflow = append(q.overflow, entry.text)
}

func (q *steerQueue) queued() []string {
	return q.ids[max(len(q.ids)-len(q.ready)-len(q.overflow), 0):]
}

func (q *steerQueue) drain() []steered {
	var entries []steered
	for drained := false; !drained; {
		select {
		case text := <-q.ready:
			entries = append(entries, steered{text: text})
		default:
			drained = true
		}
	}
	for _, text := range q.overflow {
		entries = append(entries, steered{text: text})
	}
	ids := q.ids[max(len(q.ids)-len(entries), 0):]
	for index := range entries {
		entries[index].id = ids[index]
	}
	q.overflow, q.ids = nil, nil
	return entries
}

func (q *steerQueue) take(emit func(Event), step int) []string {
	q.mu.Lock()
	var taken []steered
	for _, entry := range q.drain() {
		if len(q.now) == 0 || slices.Contains(q.now, entry.id) {
			taken = append(taken, entry)
			continue
		}
		q.put(entry)
	}
	q.now = nil
	q.mu.Unlock()
	if len(taken) == 0 {
		return nil
	}
	texts := make([]string, 0, len(taken))
	for _, entry := range taken {
		emit(Event{Kind: EventSteered, ID: entry.id, Text: entry.text, Step: step})
		texts = append(texts, entry.text)
	}
	return []string{strings.Join(texts, "\n\n")}
}

func (q *steerQueue) heard() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.ids) <= len(q.ready)+len(q.overflow) {
		return ""
	}
	id := q.ids[0]
	q.ids = q.ids[1:]
	return id
}

func (h *Host) Steer(text string) string {
	h.steering.mu.Lock()
	defer h.steering.mu.Unlock()
	id := session.NewEventID()
	h.steering.put(steered{id: id, text: text})
	return id
}

func (h *Host) SendNow(id string) bool {
	h.steering.mu.Lock()
	queued := h.steering.queued()
	found := id == "" && len(queued) > 0 || slices.Contains(queued, id)
	switch {
	case found && id == "":
		h.steering.now = nil
	case found:
		h.steering.now = append(h.steering.now, id)
	}
	h.steering.mu.Unlock()
	if found {
		select {
		case h.sendNow <- struct{}{}:
		default:
		}
	}
	return found
}

func (h *Host) DropSteering() {
	h.steering.mu.Lock()
	defer h.steering.mu.Unlock()
	h.steering.drain()
	h.steering.now = nil
}

func (h *Host) Unsteer(text string) bool {
	h.steering.mu.Lock()
	defer h.steering.mu.Unlock()
	entries := h.steering.drain()
	at := slices.IndexFunc(entries, func(entry steered) bool { return entry.text == text })
	if at >= 0 {
		entries = slices.Delete(entries, at, at+1)
	}
	for _, kept := range entries {
		h.steering.put(kept)
	}
	return at >= 0
}
