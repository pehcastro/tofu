package turn

import (
	"cmp"
	"context"
	"errors"
	"strings"
	"sync"

	"tofu/internal/session"
)

type Inbox struct {
	mu       sync.Mutex
	items    []string
	running  int
	wake     chan struct{}
	held     map[string]*heldSubAgent
	logs     map[*session.Log]int
	unclosed []error
}

func NewInbox() *Inbox {
	return &Inbox{wake: make(chan struct{}, 1), held: map[string]*heldSubAgent{}, logs: map[*session.Log]int{}}
}

func (b *Inbox) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *Inbox) post(item string) {
	b.mu.Lock()
	b.items = append(b.items, item)
	b.mu.Unlock()
	b.signal()
}

func (b *Inbox) Take() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	taken := b.items
	b.items = nil
	return taken
}

func (b *Inbox) next(ctx context.Context, typed <-chan string) ([]string, string) {
	for {
		b.mu.Lock()
		taken, running := b.items, b.running
		b.items = nil
		b.mu.Unlock()
		if len(taken) > 0 || running == 0 {
			return taken, ""
		}
		select {
		case <-b.wake:
		case said := <-typed:
			return nil, said
		case <-ctx.Done():
			return nil, ""
		}
	}
}

func (b *Inbox) settle() error {
	for {
		b.mu.Lock()
		running, unclosed := b.running, b.unclosed
		b.items = nil
		if running == 0 {
			b.unclosed = nil
		}
		b.mu.Unlock()
		if running == 0 {
			return errors.Join(unclosed...)
		}
		<-b.wake
	}
}

func (b *Inbox) reserve(limit int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running >= limit {
		return BreadthLimitError{Running: b.running, Limit: limit}
	}
	b.running++
	return nil
}

func (b *Inbox) unreserve() {
	b.mu.Lock()
	b.running--
	b.mu.Unlock()
	b.signal()
}

func (b *Inbox) keep(held *heldSubAgent, cancel context.CancelFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	held.running, held.cancel = true, cancel
	b.held[held.agent.ID] = held
}

func (b *Inbox) resume(to, text string, limit int, cancel context.CancelFunc) (*heldSubAgent, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	held := b.held[to]
	switch {
	case held == nil:
		return nil, false, nil
	case held.running:
		held.inbox.post(text)
		return held, true, nil
	case b.running >= limit:
		return held, false, BreadthLimitError{Running: b.running, Limit: limit}
	}
	b.running++
	held.running, held.cancel = true, cancel
	return held, false, nil
}

func (b *Inbox) stop(to string) (*heldSubAgent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	held := b.held[to]
	if held == nil || !held.running {
		return held, false
	}
	held.cancel()
	return held, true
}

func (b *Inbox) ended(held *heldSubAgent, report string, stopping bool, log *session.Log) []string {
	b.mu.Lock()
	defer b.signal()
	defer b.mu.Unlock()
	if next := held.inbox.Take(); len(next) > 0 && !stopping {
		return next
	}
	held.running = false
	b.running--
	if report != "" {
		b.items = append(b.items, report)
	}
	if err := b.releasing(log); err != nil {
		b.unclosed = append(b.unclosed, err)
	}
	return nil
}

func (b *Inbox) open(store *session.Store, header session.Header) (*session.Log, error) {
	if b == nil {
		return store.Open(header)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for log := range b.logs {
		if log.ID() == header.ID {
			b.logs[log]++
			return log, nil
		}
	}
	log, err := store.Open(header)
	if err == nil {
		b.logs[log] = 1
	}
	return log, err
}

func (b *Inbox) hold(log *session.Log) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, kept := b.logs[log]; kept {
		b.logs[log]++
	}
}

func (b *Inbox) release(log *session.Log) error {
	if b == nil {
		return log.Close()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.releasing(log)
}

func (b *Inbox) releasing(log *session.Log) error {
	refs, kept := b.logs[log]
	switch {
	case !kept:
		return nil
	case refs > 1:
		b.logs[log]--
		return nil
	}
	delete(b.logs, log)
	return log.Close()
}

func Lead(ctx context.Context, config Config, typed <-chan string, heard func(string), ended func(Row, error)) error {
	var failed []error
	for {
		row, err := Run(ctx, config)
		ended(row, err)
		failed = append(failed, err)
		next, said := config.Inbox.next(ctx, typed)
		if said != "" {
			next = append(next, said)
			if heard != nil {
				heard(said)
			}
		}
		if len(next) == 0 || ctx.Err() != nil {
			return errors.Join(append(failed, config.Inbox.settle())...)
		}
		if row.Conversation != nil {
			config.History = Sendable(row.Conversation)
		}
		config.Task, config.Images, config.NewID = strings.Join(next, "\n\n"), nil, nil
		config.Session = cmp.Or(row.Session, config.Session)
	}
}
