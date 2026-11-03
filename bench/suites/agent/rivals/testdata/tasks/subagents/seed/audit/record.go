package audit

import (
	"sync"
	"time"

	"example.com/depot/retry"
)

type Event struct {
	At     time.Time
	Actor  string
	Action string
	Target string
}

type Sink interface {
	Append(batch []Event) error
}

type Recorder struct {
	mu      sync.Mutex
	sink    Sink
	pending []Event
	now     func() time.Time
}

func NewRecorder(s Sink) *Recorder {
	return &Recorder{sink: s, now: time.Now}
}

func (r *Recorder) Note(actor, action, target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, Event{At: r.now(), Actor: actor, Action: action, Target: target})
}

func (r *Recorder) Flush() error {
	batch := r.drain()
	if len(batch) == 0 {
		return nil
	}
	return r.store(batch)
}

func (r *Recorder) drain() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	batch := r.pending
	r.pending = nil
	return batch
}

func (r *Recorder) store(batch []Event) error {
	return retry.Do(2, func() error {
		return r.sink.Append(batch)
	})
}
