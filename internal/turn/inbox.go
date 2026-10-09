package turn

import (
	"cmp"
	"context"
	"errors"
	"fmt"
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
	sourceMidTurnNote   = "mid-turn message note"
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
	sourceMemory        = "memory saved"
	sourceAnswer        = "question answer"
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
	asked    []*Asked
	waits    []*context.CancelCauseFunc
	since    time.Time
	waited   time.Duration
	stopped  bool
}

type leadStopped struct{}

func (leadStopped) Error() string {
	return "the lead was stopped, so the wait for the person was withdrawn"
}

func (b *Inbox) LeadAsks(person Person) Person {
	if b == nil || person == nil {
		return person
	}
	return func(ctx context.Context, request GateRequest, decision GateDecision) (PersonAnswer, error) {
		ctx, withdraw := context.WithCancelCause(ctx)
		defer b.waitOn(&withdraw)()
		answer, err := person(ctx, request, decision)
		if cause := context.Cause(ctx); errors.Is(cause, leadStopped{}) {
			return PersonDenied, cause
		}
		return answer, err
	}
}

func (b *Inbox) waitOn(withdraw *context.CancelCauseFunc) (done func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		(*withdraw)(leadStopped{})
	}
	if len(b.waits) == 0 {
		b.since = time.Now()
	}
	b.waits = append(b.waits, withdraw)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		(*withdraw)(nil)
		b.waits = slices.DeleteFunc(b.waits, func(open *context.CancelCauseFunc) bool { return open == withdraw })
		if len(b.waits) == 0 {
			b.waited += time.Since(b.since)
		}
	}
}

func (b *Inbox) StopLead() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = true
	for _, withdraw := range b.waits {
		(*withdraw)(leadStopped{})
	}
}

func (b *Inbox) personTime() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.waits) == 0 {
		return b.waited
	}
	return b.waited + time.Since(b.since)
}

func (b *Inbox) awaitLead(ctx context.Context, held *heldSubAgent, answer chan bool, asked string, wait time.Duration) (bool, error) {
	from, stalled := time.Now(), b.personTime()
	var gaveUp error
	for gaveUp == nil {
		answerable := time.Since(from) - (b.personTime() - stalled)
		if answerable >= wait {
			gaveUp = fmt.Errorf("its %s to answer ran out: %w", wait, context.DeadlineExceeded)
			break
		}
		timer := time.NewTimer(wait - answerable)
		select {
		case allowed := <-answer:
			timer.Stop()
			return allowed, nil
		case <-ctx.Done():
			gaveUp = context.Cause(ctx)
		case <-timer.C:
		}
		timer.Stop()
	}
	if b.withdraw(held, answer, asked) {
		return false, gaveUp
	}
	return <-answer, nil
}

type Asked struct {
	inbox *Inbox
	poll  string
}

func (b *Inbox) Open(poll string) *Asked {
	asked := &Asked{inbox: b, poll: poll}
	b.mu.Lock()
	b.asked = append(b.asked, asked)
	b.mu.Unlock()
	return asked
}

func (q *Asked) Answer(said string) {
	q.inbox.mu.Lock()
	q.inbox.items = append(q.inbox.items, inboxItem{text: said, source: sourceAnswer, posted: time.Now()})
	q.inbox.mu.Unlock()
	q.inbox.signal()
}

func (q *Asked) Close() {
	q.inbox.mu.Lock()
	q.inbox.asked = slices.DeleteFunc(q.inbox.asked, func(open *Asked) bool { return open == q })
	q.inbox.mu.Unlock()
	q.inbox.signal()
}

type QuestionType string

const (
	QuestionChoice QuestionType = "choice"
	QuestionMulti  QuestionType = "multi"
	QuestionText   QuestionType = "text"
	QuestionYesNo  QuestionType = "yesno"
)

type PersonOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Preview     string `json:"preview,omitempty"`
}

type PersonQuestion struct {
	ID          string         `json:"id"`
	Header      string         `json:"header"`
	Question    string         `json:"question"`
	Type        QuestionType   `json:"type"`
	Options     []PersonOption `json:"options,omitempty"`
	Recommended *int           `json:"recommended,omitempty"`
}

type PersonReply struct {
	ID     string   `json:"id"`
	Chosen []string `json:"chosen"`
	Text   string   `json:"text,omitempty"`
}

type PersonForm func(ctx context.Context, questions []PersonQuestion, wait time.Duration) ([]PersonReply, error)

type QuestionDismissed struct{}

func (QuestionDismissed) Error() string { return "the person dismissed the question" }

type QuestionUndelivered struct{ Why string }

func (e QuestionUndelivered) Error() string {
	return "the question reached nobody who can answer it: " + e.Why
}

type QuestionsBlock func() (blocks, decided bool)

type questionsBlockKey struct{}

func WithQuestionsBlock(ctx context.Context, blocks QuestionsBlock) context.Context {
	return context.WithValue(ctx, questionsBlockKey{}, blocks)
}

func QuestionsBlockFrom(ctx context.Context) (blocks, decided bool) {
	read, set := ctx.Value(questionsBlockKey{}).(QuestionsBlock)
	if !set {
		return false, false
	}
	return read()
}

type personFormKey struct{}

func WithPersonForm(ctx context.Context, form PersonForm) context.Context {
	return context.WithValue(ctx, personFormKey{}, form)
}

func PersonFormFrom(ctx context.Context) PersonForm {
	form, _ := ctx.Value(personFormKey{}).(PersonForm)
	return form
}

func (b *Inbox) stillAsked() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	polls := make([]string, len(b.asked))
	for i, open := range b.asked {
		polls[i] = open.poll
	}
	return polls
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

func SaidByThePerson(source string) bool {
	return source == sourceTask || source == sourceTyped || source == sourceSteer
}

func (b *Inbox) Remembered(note string) {
	b.mu.Lock()
	b.items = append(b.items, inboxItem{text: note, source: sourceMemory, posted: time.Now()})
	b.mu.Unlock()
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
		taken, waiting := b.items, b.running+len(b.asked)
		b.items = nil
		b.mu.Unlock()
		if len(taken) > 0 || waiting == 0 {
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
	config.Inbox.mu.Lock()
	config.Inbox.stopped = false
	config.Inbox.mu.Unlock()
	for {
		row, err := Run(ctx, config)
		ended(row, err)
		failed = append(failed, err)
		next, said := config.Inbox.next(ctx, typed)
		if said != "" {
			if heard != nil {
				heard(said)
			}
			if config.Steering != nil {
				said = strings.Join(append([]string{said}, config.Steering()...), "\n\n")
			}
			next = append(next, inboxItem{text: said, source: sourceTyped, posted: time.Now()})
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
