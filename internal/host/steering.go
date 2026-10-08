package host

import (
	"slices"
	"strings"
	"sync"
)

type steerQueue struct {
	mu       sync.Mutex
	ready    chan string
	overflow []string
}

func (q *steerQueue) put(text string) {
	if len(q.overflow) == 0 {
		select {
		case q.ready <- text:
			return
		default:
		}
	}
	q.overflow = append(q.overflow, text)
}

func (q *steerQueue) drain() []string {
	var texts []string
	for {
		select {
		case text := <-q.ready:
			texts = append(texts, text)
		default:
			texts = append(texts, q.overflow...)
			q.overflow = nil
			return texts
		}
	}
}

func (q *steerQueue) take(emit func(Event)) []string {
	q.mu.Lock()
	texts := q.drain()
	q.mu.Unlock()
	if len(texts) == 0 {
		return nil
	}
	for _, text := range texts {
		emit(Event{Kind: EventSteered, Text: text})
	}
	return []string{strings.Join(texts, "\n\n")}
}

func (h *Host) Steer(text string) {
	h.steering.mu.Lock()
	defer h.steering.mu.Unlock()
	h.steering.put(text)
}

func (h *Host) DropSteering() {
	h.steering.mu.Lock()
	defer h.steering.mu.Unlock()
	h.steering.drain()
}

func (h *Host) Unsteer(text string) bool {
	h.steering.mu.Lock()
	defer h.steering.mu.Unlock()
	texts := h.steering.drain()
	at := slices.Index(texts, text)
	if at >= 0 {
		texts = slices.Delete(texts, at, at+1)
	}
	for _, kept := range texts {
		h.steering.put(kept)
	}
	return at >= 0
}
