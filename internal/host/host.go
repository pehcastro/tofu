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
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/cron"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/memory"
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

var errTurnRunning = errors.New("a turn is running: wait for it to end, or stop it")

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
}

type Config struct {
	Dir     string
	Engine  Engine
	Play    Play
	Now     func() time.Time
	Shells  *shell.Registry
	Check   cron.Checker
	Resumed Carry
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

	mu        sync.Mutex
	closed    bool
	running   bool
	stopped   bool
	cancel    context.CancelFunc
	pick      Pick
	fired     []string
	unread    []cron.Fire
	answer    string
	previous  string
	streaming bool
	ticking   *time.Timer
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

	reads  *turn.ReadLedger
	inbox  *turn.Inbox
	roster *roster.Roster
	spent  *tokenTally
	shown  map[string]bool
	asks   *asks

	questions   *replies[answeredQuestion]
	memoryPicks *replies[memory.Scope]
}

type pendingImage struct {
	index int
	path  string
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
		id:          cfg.Resumed.Session,
		started:     SourceStartup,
		shown:       map[string]bool{},
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
	if h.id == "" {
		return h, troubles
	}
	h.started = SourceResumed
	if h.readOnly = h.hold(h.id); h.readOnly != nil {
		return h, append(troubles, h.readOnly.Error())
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
	ctx, cancel := context.WithCancel(context.Background())
	h.running, h.stopped, h.cancel, h.turn = true, false, cancel, turn.NewID(h.now())
	live := Live{Turn: h.turn, Steering: h.steering.ready, LeadStop: h.stopLead, Answers: h.answers, Origin: origin}
	go func() {
		out := h.emitter()
		live.Emit = out.emit
		out.emit(Event{Kind: EventTurnStarted, ID: live.Turn, Text: task, Origin: origin})
		h.play(ctx, pick, task, live)
		cancel()
		h.ended(out)
	}()
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
	h.mu.Lock()
	defer h.mu.Unlock()
	return id, h.hold(id)
}

func (h *Host) Remembered(note string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.inbox.Remembered(note)
}

func (h *Host) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.stopped = true
		h.cancel()
	}
}

func (h *Host) StopLead() {
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
	return h.asks.answer(id, answer)
}

func (h *Host) Standing() []StandingAnswer { return h.asks.standingNow() }

func (h *Host) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	if h.ticking != nil {
		h.ticking.Stop()
	}
	if h.cancel != nil {
		h.cancel()
	}
	h.letGo()
}

func (h *Host) deliver(event Event) {
	if !event.snapshot() {
		h.events <- event
		return
	}
	select {
	case h.events <- event:
	default:
	}
}

func (h *Host) note(text string) {
	h.emitter().emit(Event{Kind: EventNote, Text: text})
}

func (h *Host) hold(id string) error {
	if id == h.heldID || h.closed {
		return nil
	}
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return err
	}
	release, err := store.Hold(id)
	if err != nil {
		return err
	}
	h.letGo()
	h.release, h.heldID = release, id
	return nil
}

func (h *Host) letGo() {
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
	if h.engine != nil {
		h.engine.Renew()
	}
	h.reads, h.inbox, h.roster, h.spent = turn.NewReadLedger(), turn.NewInbox(), &roster.Roster{}, &tokenTally{}
	h.ran, h.carried, h.prefix = nil, resumed.Messages, &turn.Prefix{}
	for _, message := range resumed.Messages {
		if message.ToolCallID != "" {
			h.shown[message.ToolCallID] = true
		}
	}
	if resumed.Session == "" {
		return nil
	}
	store, err := resumed.reading(h.dir)
	if err == nil {
		err = turn.RestoreSubAgents(store, resumed.Session, h.roster, h.inbox)
	}
	if err != nil {
		return fmt.Errorf("the sub-agents of %s were not all read back: %w", resumed.Session, err)
	}
	return nil
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
	if h.running {
		h.mu.Unlock()
		return nil, errTurnRunning
	}
	if err := h.hold(carry.Session); err != nil {
		h.mu.Unlock()
		return nil, err
	}
	h.readOnly = nil
	h.id, h.pending, h.started = carry.Session, nil, SourceResumed
	restoreErr := h.carry(carry)
	h.mu.Unlock()
	chat := resumedChat(carry, h.dir)
	if restoreErr != nil {
		chat = append(chat, Event{Kind: EventNote, Text: restoreErr.Error()})
	}
	if err := h.loadCron(carry.Session); err != nil {
		chat = append(chat, Event{Kind: EventNote, Text: err.Error()})
	}
	h.armCron()
	return chat, nil
}

func (h *Host) Opening(carry Carry) []Event { return resumedChat(carry, h.dir) }

func (h *Host) Compacted(into string, messages []llm.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.id, h.carried, h.prefix = into, messages, &turn.Prefix{}
	return h.hold(into)
}

func (h *Host) Ran(command, output string, stopped bool) string {
	redactor := sys.LoadKeyRedactor()
	output = ansi.Strip(redactor.Redact(output))
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
	h.pending = append(h.pending, pendingImage{index: index, path: filepath.Join(store.AttachmentDir(id), name)})
	h.mu.Unlock()
}

func (h *Host) takePendingImages(task string) ([]llm.Image, error) {
	h.mu.Lock()
	var wanted []pendingImage
	h.pending = slices.DeleteFunc(h.pending, func(image pendingImage) bool {
		taken := strings.Contains(task, ImageToken(image.index))
		if taken {
			wanted = append(wanted, image)
		}
		return taken
	})
	h.mu.Unlock()
	images := make([]llm.Image, 0, len(wanted))
	for _, image := range wanted {
		data, err := os.ReadFile(image.path)
		if err != nil {
			return nil, err
		}
		images = append(images, llm.Image{MediaType: imageMediaType(image.path), Data: data})
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
