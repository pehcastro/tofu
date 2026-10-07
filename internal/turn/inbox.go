package turn

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
)

const (
	sourceTask          = "task"
	sourceBrief         = "sub-agent brief"
	sourceHandback      = "turn end check"
	sourceTyped         = "typed by the person"
	sourceSteer         = "steer"
	sourceStepCapNotice = "step cap notice"
	sourceStopHook      = "stop hook"
	sourceLastWord      = "last word"
	sourceSpawnLine     = "spawn line"
	sourceForkTask      = "fork task"
	sourceForkCarry     = "fork carry"
	sourceCheck         = "sub-agent check"
	sourceReport        = "sub-agent report"
	sourceMessage       = "message to this sub-agent"
	sourceGateAsk       = "sub-agent gate ask"
)

type inboxItem struct {
	text   string
	source string
	posted time.Time
}

type Inbox struct {
	mu       sync.Mutex
	items    []inboxItem
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
	b.items = append(b.items, inboxItem{text: item, source: sourceMessage, posted: time.Now()})
	b.mu.Unlock()
	b.signal()
}

func (b *Inbox) attach(held *heldSubAgent, check *checkIn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	held.check = check
}

func (b *Inbox) postCheck(held *heldSubAgent, check *checkIn, gate []string, now time.Time) {
	b.mu.Lock()
	defer b.signal()
	defer b.mu.Unlock()
	if held.check != check {
		return
	}
	unread := slices.IndexFunc(b.items, func(item inboxItem) bool { return item.text == check.posted })
	text := check.compose(unread < 0, gate, now)
	if unread >= 0 {
		b.items[unread] = inboxItem{text: text, source: sourceCheck, posted: now}
		return
	}
	b.items = append(b.items, inboxItem{text: text, source: sourceCheck, posted: now})
}

func (b *Inbox) Take() []string {
	return textsOf(b.takeItems())
}

func originOf(items []inboxItem) llm.Origin {
	var sources []string
	for _, item := range items {
		if !slices.Contains(sources, item.source) {
			sources = append(sources, item.source)
		}
	}
	return llm.Origin{Source: strings.Join(sources, " and "), PostedAt: items[0].posted}
}

func textsOf(items []inboxItem) []string {
	var texts []string
	for _, item := range items {
		texts = append(texts, item.text)
	}
	return texts
}

func (b *Inbox) takeItems() []inboxItem {
	b.mu.Lock()
	defer b.mu.Unlock()
	taken := b.items
	b.items = nil
	return taken
}

func (b *Inbox) next(ctx context.Context, typed <-chan string) ([]inboxItem, string) {
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
	held.started(cancel)
	b.held[held.agent.ID] = held
}

func (b *Inbox) adopt(held *heldSubAgent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.held[held.agent.ID] == nil {
		b.held[held.agent.ID] = held
	}
}

func (b *Inbox) find(to string) (*heldSubAgent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	held := b.held[to]
	return held, held != nil && held.running
}

func (h *heldSubAgent) lineage() []string {
	ids := []string{h.agent.ID}
	h.inbox.mu.Lock()
	nested := slices.Collect(maps.Values(h.inbox.held))
	h.inbox.mu.Unlock()
	for _, child := range nested {
		ids = append(ids, child.lineage()...)
	}
	return ids
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
	held.started(cancel)
	return held, false, nil
}

func (b *Inbox) halt(to string, halt func(*heldSubAgent)) (*heldSubAgent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	held := b.held[to]
	if held == nil || !held.running {
		return held, false
	}
	halt(held)
	return held, true
}

func (b *Inbox) ask(held *heldSubAgent, asked string) chan bool {
	answer := make(chan bool, 1)
	b.mu.Lock()
	held.answer = answer
	b.items = append(b.items, inboxItem{text: asked, source: sourceGateAsk, posted: time.Now()})
	b.mu.Unlock()
	b.signal()
	return answer
}

func (b *Inbox) answer(to string, allowed bool) (*heldSubAgent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	held := b.held[to]
	if held == nil || held.answer == nil {
		return held, false
	}
	held.answer <- allowed
	held.answer = nil
	return held, true
}

func (b *Inbox) withdraw(held *heldSubAgent, answer chan bool, asked string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if held.answer != answer {
		return false
	}
	held.answer = nil
	b.items = slices.DeleteFunc(b.items, func(item inboxItem) bool { return item.source == sourceGateAsk && item.text == asked })
	return true
}

func (b *Inbox) ended(held *heldSubAgent, report string, stopping bool, log *session.Log) []inboxItem {
	b.mu.Lock()
	defer b.signal()
	defer b.mu.Unlock()
	if next := held.inbox.takeItems(); len(next) > 0 && !stopping {
		return next
	}
	held.running, held.check = false, nil
	b.running--
	if report != "" {
		b.items = append(b.items, inboxItem{text: report, source: sourceReport, posted: time.Now()})
	}
	if report != "" && log != nil {
		if _, err := log.Append(session.Event{Agent: held.agent.ID, Kind: session.EventReport}, session.ReportBody{State: held.agent.State.String(), Text: report}); err != nil {
			b.unclosed = append(b.unclosed, err)
		}
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

func (b *Inbox) hold(held *heldSubAgent, log *session.Log) {
	b.mu.Lock()
	defer b.mu.Unlock()
	held.log = log
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
			next = append(next, inboxItem{text: said, source: sourceTyped, posted: time.Now()})
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
		config.TaskOrigin = originOf(next)
		config.Task, config.Images, config.NewID, config.SessionSource = strings.Join(textsOf(next), "\n\n"), nil, nil, ""
		if said != "" && config.ImagesOf != nil {
			config.Images = config.ImagesOf(said)
		}
		config.Session = cmp.Or(row.Session, config.Session)
	}
}
