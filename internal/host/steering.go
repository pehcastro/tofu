package host

import (
	"slices"
	"strings"
	"sync"

	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
)

type steerQueue struct {
	mu       sync.Mutex
	ready    chan string
	overflow []string
	ids      []string
	now      []string
	held     []steered
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
	if f := h.following.Load(); f != nil {
		images := h.takePending(text)
		return h.keepUndelivered(f.ask(followAsk{Op: followSteer, Text: text, Images: images}), images).Steered
	}
	return h.queueSteer(session.NewEventID(), text)
}

func (h *Host) CarrySteers() []string {
	if h.following.Load() != nil {
		return nil
	}
	return h.carrySteers(h.emitter())
}

func (h *Host) queueSteer(id, text string) string {
	h.steering.mu.Lock()
	defer h.steering.mu.Unlock()
	h.steering.put(steered{id: id, text: text})
	return id
}

func (h *Host) withImages(event Event) Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	event.Images = shownPending(event.Text, h.pending)
	return event
}

func (h *Host) queueTyped(text string) string {
	if h.following.Load() != nil {
		return h.Steer(text)
	}
	return h.steerFor(text)
}

func (h *Host) SendNow(id string) bool {
	if f := h.following.Load(); f != nil {
		return f.ask(followAsk{Op: followSendNow, Text: id}).OK
	}
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
	if f := h.following.Load(); f != nil {
		f.ask(followAsk{Op: followUnqueue})
		return
	}
	h.steering.mu.Lock()
	defer h.steering.mu.Unlock()
	h.steering.held = append(h.steering.held, h.steering.drain()...)
	h.steering.now = nil
}

func (h *Host) carrySteers(out *emitter) []string {
	h.mu.Lock()
	if h.closed || h.running {
		h.mu.Unlock()
		return nil
	}
	h.steering.mu.Lock()
	h.steering.held = append(h.steering.held, h.steering.drain()...)
	unread := h.steering.held
	h.steering.held, h.steering.now = nil, nil
	h.steering.mu.Unlock()
	texts := make([]string, 0, len(unread))
	for _, entry := range unread {
		texts = append(texts, entry.text)
	}
	if len(unread) > 0 {
		h.begin(Pick{Wire: h.pick.Wire, Model: h.pick.Model, Effort: h.pick.Effort}, strings.Join(texts, "\n\n"), core.Origin{Source: core.SourceTyped})
	}
	h.mu.Unlock()
	for _, entry := range unread {
		out.emit(Event{Kind: EventSteered, ID: entry.id, Text: entry.text, Step: 1, Origin: wireOrigin(core.Origin{Source: core.SourceTyped})})
	}
	return texts
}

func (h *Host) Unsteer(text string) bool {
	if f := h.following.Load(); f != nil {
		return f.ask(followAsk{Op: followUnsteer, Text: text}).OK
	}
	h.steering.mu.Lock()
	defer h.steering.mu.Unlock()
	typed := func(entry steered) bool { return entry.text == text }
	if at := slices.IndexFunc(h.steering.held, typed); at >= 0 {
		h.steering.held = slices.Delete(h.steering.held, at, at+1)
		return true
	}
	entries := h.steering.drain()
	at := slices.IndexFunc(entries, typed)
	if at >= 0 {
		entries = slices.Delete(entries, at, at+1)
	}
	for _, kept := range entries {
		h.steering.put(kept)
	}
	return at >= 0
}
