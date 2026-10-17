package host

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/cron"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/memory"
	"tofu/internal/safe"
	"tofu/internal/session"
	"tofu/internal/shell"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const (
	SourceStartup  = "startup"
	SourceCleared  = "clear"
	SourceResumed  = "resume"
	ImageTokenHead = "[Image #"
	ranPreface     = "before sending this, the person ran a command in the project with !, outside any turn, and it printed:\n$ "
)

func ImageToken(index int) string { return ImageTokenHead + strconv.Itoa(index) + "]" }

var (
	errTurnRunning = errors.New("a turn is running: wait for it to end, or stop it")
	errProjectTurn = errors.New("another session this tofu holds is already working in this project, and this project lets one session work at a time: wait for it, or set oneWorkingSessionPerProject off")
)

type Play func(ctx context.Context, pick Pick, task string, live Live)

type Live struct {
	Turn     string
	Emit     func(Event)
	Steering <-chan string
	LeadStop <-chan struct{}
	Answers  <-chan Answer
	Origin   Origin
}

type Carry struct {
	Session  string
	Name     string
	Messages []llm.Message
	Tasks    []string
	Store    *session.Store
	Keys     *sys.KeyRedactor
}

type Config struct {
	Dir         string
	Engine      Engine
	Play        Play
	Now         func() time.Time
	Shells      *shell.Registry
	Check       cron.Checker
	Resumed     Carry
	ProjectTurn *sync.Mutex
	Detached    bool
	Unrecorded  bool
}

type Host struct {
	dir      string
	engine   Engine
	play     Play
	now      func() time.Time
	shells   *shell.Registry
	events   chan Event
	answers  chan Answer
	steering steerQueue
	stopLead chan struct{}
	sendNow  chan struct{}
	cron     *cron.Book
	cronMove chan struct{}
	readOnly error
	turnLock *sync.Mutex
	keys     func() keyMask
	done     chan struct{}
	detached bool

	audience  atomic.Pointer[audience]
	following atomic.Pointer[following]

	mu        sync.Mutex
	closed    bool
	running   bool
	stopped   bool
	cancelled bool
	cancel    context.CancelFunc
	turnOver  chan struct{}
	pick      Pick
	fired     []string
	unread    []cron.Fire
	answer    string
	previous  string
	streaming bool
	ticking   *time.Timer
	checkIn   *turn.LeadCheckIn
	checking  *time.Timer
	id        string
	started   string
	carried   []llm.Message
	prefix    *turn.Prefix
	pending   []pendingImage
	ran       []string
	release   func() error
	heldID    string
	turn      string
	asking    AskingMode

	reads   *turn.ReadLedger
	inbox   *turn.Inbox
	settled []<-chan struct{}
	roster  *roster.Roster
	spent   *tokenTally
	shown   map[string]bool
	asks    *asks

	questions   *replies[answeredQuestion]
	memoryPicks *replies[memory.Scope]
}

type pendingImage struct {
	Index int
	Path  string
}

func New(cfg Config) (*Host, []string) {
	cronMove := make(chan struct{}, 1)
	h := &Host{
		dir:         cfg.Dir,
		engine:      cfg.Engine,
		play:        cfg.Play,
		now:         cfg.Now,
		shells:      cfg.Shells,
		events:      make(chan Event, konst.HostEventBuffer),
		answers:     make(chan Answer, 1),
		steering:    steerQueue{ready: make(chan string, konst.HostSteeringQueue)},
		stopLead:    make(chan struct{}, 1),
		sendNow:     make(chan struct{}, 1),
		cron:        &cron.Book{Check: cfg.Check, Changed: cronMove},
		cronMove:    cronMove,
		turnLock:    cfg.ProjectTurn,
		keys:        sync.OnceValue(loadKeyMask),
		done:        make(chan struct{}),
		detached:    cfg.Detached,
		id:          cfg.Resumed.Session,
		started:     SourceStartup,
		asks:        &asks{standing: map[string]Answer{}},
		questions:   &replies[answeredQuestion]{},
		memoryPicks: &replies[memory.Scope]{},
	}
	h.asks.session, h.asks.unkept = h.standingIn, h.standingUnkept
	if h.now == nil {
		h.now = time.Now
	}
	if h.play == nil {
		h.play = h.run
	}
	var troubles []string
	if err := h.carry(cfg.Resumed); err != nil {
		troubles = append(troubles, err.Error())
	}
	h.armCron()
	if h.id == "" {
		return h, troubles
	}
	if !cfg.Unrecorded {
		h.started = SourceResumed
	}
	leader, err := h.holdOrFollow(h.id)
	switch {
	case err != nil:
		h.readOnly = err
		return h, append(troubles, err.Error())
	case leader != nil:
		return h, append(troubles, leader.note(h.id))
	}
	if err := h.loadCron(h.id); err != nil {
		return h, append(troubles, err.Error())
	}
	h.armCron()
	return h, troubles
}

func (h *Host) Events() <-chan Event { return h.events }

func (h *Host) Cron() *cron.Book { return h.cron }

func (h *Host) ID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.id
}

func (h *Host) Carried() []llm.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.carried
}

func (h *Host) Choose(pick Pick) {
	h.mu.Lock()
	h.pick = pick
	h.mu.Unlock()
}

func (h *Host) Send(pick Pick, task string) bool {
	if h.detached && h.following.Load() == nil {
		id := h.pendingID()
		h.mu.Lock()
		_, err := h.holdOrFollow(id)
		h.mu.Unlock()
		if err != nil {
			h.note(err.Error())
			return false
		}
	}
	if f := h.following.Load(); f != nil {
		h.mu.Lock()
		images := h.takePending(task)
		h.mu.Unlock()
		return f.send(pick, task, images)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return false
	}
	h.pick = Pick{Wire: pick.Wire, Model: pick.Model, Effort: pick.Effort}
	h.begin(pick, task, Origin{Kind: OriginPerson})
	return true
}

func (h *Host) begin(pick Pick, task string, origin Origin) {
	h.dropCheckIn()
	ctx, cancel := context.WithCancel(turn.WithLeadCheckIn(context.Background(), h.keepCheckIn))
	turnOver := make(chan struct{})
	h.running, h.stopped, h.cancelled, h.cancel, h.turn, h.turnOver = true, false, false, cancel, turn.NewID(h.now()), turnOver
	live := Live{Turn: h.turn, Steering: h.steering.ready, LeadStop: h.stopLead, Answers: h.answers, Origin: origin}
	safe.Go(h.report, "turn", func() {
		defer close(turnOver)
		out := h.emitter()
		live.Emit = out.emit
		out.emit(Event{Kind: EventTurnStarted, ID: live.Turn, Text: task, Origin: origin})
		_ = safe.Call(func(p safe.Panic) { out.emit(Event{Kind: EventFailure, Text: p.Saved(h.stateDir())}) }, "turn", func() error {
			h.play(ctx, pick, task, live)
			return nil
		})
		cancel()
		h.ended(out)
		h.armCheckIn()
	})
}

func (h *Host) Turn() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.turn, h.running
}

func (h *Host) SetAsking(asking AskingMode) {
	h.mu.Lock()
	h.asking = asking
	h.mu.Unlock()
}

func (h *Host) Settings() (AskingMode, Pick) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.asking, h.pick
}

func (h *Host) OpenFresh() (string, error) {
	if err := h.Fresh(); err != nil {
		return "", err
	}
	id := h.pendingID()
	if h.detached {
		return id, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return id, h.hold(id)
}

func (h *Host) Remembered(note string) {
	if f := h.following.Load(); f != nil {
		f.ask(followAsk{Op: followRemember, Text: note})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.inbox.Remembered(note)
}

func (h *Host) Stop() {
	if f := h.following.Load(); f != nil {
		f.ask(followAsk{Op: followStop})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.stopped, h.cancelled = true, true
		h.cancel()
	}
}

func (h *Host) Cancelled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cancelled
}

func (h *Host) StopLead() {
	if f := h.following.Load(); f != nil {
		f.ask(followAsk{Op: followStopLead})
		return
	}
	h.mu.Lock()
	h.stopped = true
	inbox := h.inbox
	h.mu.Unlock()
	inbox.StopLead()
	select {
	case h.stopLead <- struct{}{}:
	default:
	}
}

func (h *Host) Answer(id string, answer Answer) bool {
	if f := h.following.Load(); f != nil {
		return f.ask(followAsk{Op: followAnswer, Asked: id, Answer: answer}).OK
	}
	if answered, _ := h.asks.answer(id, answer); answered {
		return true
	}
	select {
	case h.answers <- answer:
		return true
	default:
		return false
	}
}

func (h *Host) AnswerAsk(id string, answer Answer) (answered, stands bool) {
	if f := h.following.Load(); f != nil {
		reply := f.ask(followAsk{Op: followAnswerAsk, Asked: id, Answer: answer})
		return reply.OK, reply.Stands
	}
	return h.asks.answer(id, answer)
}

func (h *Host) Standing() []StandingAnswer { return h.asks.standingNow() }

func (h *Host) Close() {
	h.mu.Lock()
	if !h.closed {
		close(h.done)
	}
	h.closed = true
	h.dropCheckIn()
	if h.ticking != nil {
		h.ticking.Stop()
	}
	if h.cancel != nil {
		h.cancel()
	}
	h.settled = append(h.settled, h.inbox.CancelCompactions())
	turnOver, settled := h.turnOver, h.settled
	h.mu.Unlock()
	givenUp, stop := context.WithTimeout(context.Background(), konst.HostCloseWaitMillis*time.Millisecond)
	defer stop()
	for turnOver != nil {
		select {
		case <-turnOver:
			turnOver = nil
		case <-givenUp.Done():
			turnOver = nil
		case <-h.events:
		}
	}
	for _, over := range settled {
		select {
		case <-over:
		case <-givenUp.Done():
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.letGo()
}

func (h *Host) deliver(event Event) {
	if a := h.audience.Load(); a != nil {
		a.tell(event)
	}
	select {
	case h.events <- event:
		return
	default:
	}
	if !event.snapshot() {
		select {
		case h.events <- event:
		case <-h.done:
		}
	}
}

func (h *Host) note(text string) {
	h.emitter().emit(Event{Kind: EventNote, Text: text})
}

func (h *Host) stateDir() string {
	dir, _ := sys.ProjectStateDirAt(h.dir)
	return dir
}

func (h *Host) report(p safe.Panic) {
	h.note(p.Saved(h.stateDir()))
}

func (h *Host) hold(id string) error {
	if id == h.heldID || h.closed {
		return nil
	}
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return err
	}
	if h.detached {
		started := startHolder(h.dir, store, id)
		if started == nil {
			return cmp.Or(store.Busy(id), errNoHolder)
		}
		safe.Go(h.report, "holder note", func() { h.note(started.Error() + ", so this tofu runs the session itself") })
	}
	release, err := store.Hold(id)
	if err != nil {
		return err
	}
	moving := h.audience.Swap(nil)
	h.letGo()
	h.release, h.heldID = release, id
	switch {
	case release != nil:
		h.openAudience(store, id, moving)
	case moving != nil:
		moving.close()
	}
	return nil
}

func (h *Host) holdOrFollow(id string) (*following, error) {
	refused := h.hold(id)
	if refused == nil {
		return nil, nil
	}
	leader, err := h.dialHolder(id, refused)
	if err != nil {
		return nil, refused
	}
	h.letGo()
	h.follow(leader, id)
	return leader, nil
}

func (h *Host) letGo() {
	if a := h.audience.Swap(nil); a != nil {
		a.close()
	}
	if f := h.following.Swap(nil); f != nil {
		f.leave()
	}
	if h.release != nil {
		_ = h.release()
	}
	h.release, h.heldID = nil, ""
}

func (h *Host) loadCron(id string) error {
	if id == "" {
		return h.cron.Load("")
	}
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return err
	}
	return h.cron.Load(cronFile(store, id))
}

func cronFile(store *session.Store, id string) string {
	return filepath.Join(store.Dir(id), "cron.json")
}

func (h *Host) carry(resumed Carry) error {
	if h.inbox != nil {
		h.settled = append(h.settled, h.inbox.CancelCompactions())
	}
	if h.engine != nil {
		h.engine.Renew()
	}
	h.reads, h.inbox, h.roster, h.spent = turn.NewReadLedger(), turn.NewInbox(), &roster.Roster{}, &tokenTally{}
	h.ran, h.carried, h.prefix, h.shown = nil, resumed.Messages, &turn.Prefix{}, map[string]bool{}
	for _, message := range resumed.Messages {
		if message.ToolCallID != "" {
			h.shown[message.ToolCallID] = true
		}
	}
	if resumed.Session == "" {
		return nil
	}
	store, err := resumed.reading(h.dir)
	if err == nil && !h.detached && h.following.Load() == nil {
		err = turn.RestoreSubAgents(store, resumed.Session, h.roster, h.inbox)
	}
	if err != nil {
		return fmt.Errorf("the sub-agents of %s were not all read back: %w", resumed.Session, err)
	}
	side, found, err := store.Side(resumed.Session)
	if err != nil || !found || side.Effort == "" {
		return err
	}
	effort, err := llm.ParseEffort(side.Effort)
	if err == nil {
		h.pick.Effort = effort
	}
	return err
}

func (c Carry) reading(dir string) (*session.Store, error) {
	if c.Store != nil {
		return c.Store, nil
	}
	return session.OpenIn(dir)
}

func (h *Host) Fresh() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropCheckIn()
	if h.running {
		return errTurnRunning
	}
	h.letGo()
	_ = h.carry(Carry{})
	h.id, h.pending, h.started, h.readOnly = "", nil, SourceCleared, nil
	return h.cron.Load("")
}

func (h *Host) Resume(carry Carry) ([]Event, error) {
	h.mu.Lock()
	h.dropCheckIn()
	if h.running {
		h.mu.Unlock()
		return nil, errTurnRunning
	}
	leader, err := h.holdOrFollow(carry.Session)
	if err != nil {
		h.mu.Unlock()
		return nil, err
	}
	if leader == nil {
		h.readOnly = nil
	}
	h.id, h.pending, h.started = carry.Session, nil, SourceResumed
	restoreErr := h.carry(carry)
	h.mu.Unlock()
	chat := h.Opening(carry)
	if restoreErr != nil {
		chat = append(chat, Event{Kind: EventNote, Text: restoreErr.Error()})
	}
	if err := h.loadCron(carry.Session); err != nil {
		chat = append(chat, Event{Kind: EventNote, Text: err.Error()})
	}
	h.armCron()
	return chat, nil
}

func (h *Host) Opening(carry Carry) []Event {
	chat := resumedChat(carry, h.dir)
	if f := h.following.Load(); f != nil {
		chat = append(chat, Event{Kind: EventNote, Text: f.note(carry.Session)})
	}
	return chat
}

func (h *Host) Compacted(into string, messages []llm.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropCheckIn()
	h.id, h.carried, h.prefix = into, messages, &turn.Prefix{}
	_, err := h.holdOrFollow(into)
	return err
}

func (h *Host) Ran(command, output string, stopped bool) string {
	redactor := sys.LoadKeyRedactor()
	output = ansi.Strip(redactor.Redact(output))
	if f := h.following.Load(); f != nil {
		f.ask(followAsk{Op: followRan, Command: command, Text: output, Stopped: stopped})
		return output
	}
	if !stopped {
		h.mu.Lock()
		h.ran = append(h.ran, redactor.Redact(ranPreface+command+"\n"+output))
		h.mu.Unlock()
	}
	return output
}

func (h *Host) takeRan() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	taken := h.ran
	h.ran = nil
	if len(taken) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(taken, "\n\n")
}

func (h *Host) pendingID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.id == "" {
		h.id = session.NewEventID()
	}
	return h.id
}

func (h *Host) AttachmentDir() (string, error) {
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return "", err
	}
	return store.AttachmentDir(h.pendingID()), nil
}

func (h *Host) Attached(index int, name string, bytes int, format string) {
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return
	}
	id := h.pendingID()
	_ = store.AppendEvent(id, session.EventAttachment, session.Attachment{File: session.AttachmentPath(id, name), Bytes: bytes, Format: format})
	h.mu.Lock()
	h.pending = append(h.pending, pendingImage{Index: index, Path: filepath.Join(store.AttachmentDir(id), name)})
	h.mu.Unlock()
}

func (h *Host) takePending(task string) []pendingImage {
	var wanted []pendingImage
	h.pending = slices.DeleteFunc(h.pending, func(image pendingImage) bool {
		taken := strings.Contains(task, ImageToken(image.Index))
		if taken {
			wanted = append(wanted, image)
		}
		return taken
	})
	return wanted
}

func (h *Host) takePendingImages(task string) ([]llm.Image, error) {
	h.mu.Lock()
	wanted := h.takePending(task)
	h.mu.Unlock()
	images := make([]llm.Image, 0, len(wanted))
	for _, image := range wanted {
		data, err := os.ReadFile(image.Path)
		if err != nil {
			return nil, err
		}
		images = append(images, llm.Image{MediaType: imageMediaType(image.Path), Data: data})
	}
	return images, nil
}

func imageMediaType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	return cmp.Or(imageMediaTypes()[ext], "image/"+strings.TrimPrefix(ext, "."))
}

func imageMediaTypes() map[string]string {
	return map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}
}
