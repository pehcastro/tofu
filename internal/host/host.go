package host

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
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
	errProjectTurn = errors.New("another session in this project's tofu host is already working, and this project lets one session work at a time: wait for it, or set oneWorkingSessionPerProject off")
)

type Play func(ctx context.Context, pick Pick, task string, live Live)

type Live struct {
	Turn       string
	Emit       func(Event)
	Steering   <-chan string
	LeadStop   <-chan struct{}
	Answers    <-chan Answer
	Origin     llm.Origin
	Unattended bool
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
	Joins       bool
	Unrecorded  bool
}

type Host struct {
	dir      string
	engine   Engine
	play     Play
	now      func() time.Time
	shells   *shell.Registry
	events   chan Event
	bus      bus
	answers  chan Answer
	steering steerQueue
	stopLead chan struct{}
	sendNow  chan struct{}
	cron     *cron.Book
	cronMove chan struct{}
	turnLock *sync.Mutex
	keys     func() keyMask
	done     chan struct{}
	joins    bool

	audience  atomic.Pointer[audience]
	following atomic.Pointer[following]
	shellRaw  chan ShellRaw
	held      heldShells

	mu        sync.Mutex
	closed    bool
	running   bool
	stopped   bool
	cancelled bool
	stopBy    string
	closeBy   string
	closeWhy  string
	member    MemberKind
	cancel    context.CancelCauseFunc
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
	carriedOn string
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
	shown   *shownResults
	asks    *asks

	questions   *replies[answeredQuestion]
	memoryPicks *replies[memory.Scope]
}

type pendingImage struct {
	Index    int
	Path     string
	Bytes    int
	Format   string
	Recorded bool
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
		shellRaw:    make(chan ShellRaw, konst.FollowQueue),
		answers:     make(chan Answer, 1),
		steering:    steerQueue{ready: make(chan string, konst.HostSteeringQueue)},
		stopLead:    make(chan struct{}, 1),
		sendNow:     make(chan struct{}, 1),
		cron:        &cron.Book{Check: cfg.Check, Changed: cronMove},
		cronMove:    cronMove,
		turnLock:    cfg.ProjectTurn,
		keys:        sync.OnceValue(loadKeyMask),
		done:        make(chan struct{}),
		joins:       cfg.Joins,
		member:      MemberTUI,
		id:          cfg.Resumed.Session,
		started:     SourceStartup,
		asks:        &asks{standing: map[string]Answer{}, until: map[string]time.Time{}},
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
	if err := h.enter(h.id); err != nil {
		return h, append(troubles, err.Error())
	}
	if h.joins {
		return h, troubles
	}
	safe.Go(h.report, "follow binding", h.followBinding)
	if err := h.loadCron(h.id); err != nil {
		return h, append(troubles, err.Error())
	}
	h.armCron()
	return h, troubles
}

func (h *Host) followBinding() {
	seen, bound := ConfigSeen(h.dir), boundOrchestrator(h.dir)
	every := time.NewTicker(konst.ServeShellPollMillis * time.Millisecond)
	defer every.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-every.C:
		}
		now := ConfigSeen(h.dir)
		if maps.Equal(now, seen) {
			continue
		}
		seen = now
		if moved := boundOrchestrator(h.dir); moved != bound {
			bound = moved
			h.mu.Lock()
			h.settle("", bound)
			h.mu.Unlock()
		}
	}
}

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
	f := h.following.Load()
	switch {
	case f == nil:
		h.Settle("", pick)
	case f.chosen(pick):
		safe.Go(h.report, "follow pick", func() { h.Settle("", pick) })
	}
}

func (h *Host) SetAsking(asking AskingMode) { h.Settle(asking, Pick{}) }

func (h *Host) Settle(asking AskingMode, pick Pick) {
	h.mu.Lock()
	h.settle(asking, pick)
	h.mu.Unlock()
	if f := h.following.Load(); f != nil {
		f.ask(followAsk{Op: followSettings, Asking: asking, Pick: pick})
	}
}

func (h *Host) settle(asking AskingMode, pick Pick) {
	was := heldSettings{Asking: h.asking, Pick: h.pick}
	h.asking = cmp.Or(asking, h.asking)
	if pick != (Pick{}) {
		h.pick = pick
	}
	now := heldSettings{Asking: h.asking, Pick: h.pick}
	if now == was {
		return
	}
	h.bus.mu.Lock()
	h.bus.now.Settings = now
	h.fanOut(followLine{Settings: &now})
	h.bus.mu.Unlock()
}

func (h *Host) reachHost() error {
	if !h.joins || h.following.Load() != nil {
		return nil
	}
	return h.enter(h.pendingID())
}

func (h *Host) Send(pick Pick, task string) bool {
	if err := h.reachHost(); err != nil {
		h.note(err.Error())
		return false
	}
	if f := h.following.Load(); f != nil {
		images := h.takePending(task)
		return h.keepUndelivered(f.send(pick, task, images), images).OK
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return false
	}
	h.settle("", Pick{Wire: pick.Wire, Model: pick.Model, Effort: pick.Effort})
	h.begin(pick, task, llm.Origin{Source: llm.SourceTask})
	return true
}

func (h *Host) begin(pick Pick, task string, origin llm.Origin) {
	h.dropCheckIn()
	ctx, cancel := context.WithCancelCause(turn.WithLeadCheckIn(context.Background(), h.keepCheckIn))
	turnOver := make(chan struct{})
	h.running, h.stopped, h.cancelled, h.stopBy, h.cancel, h.turn, h.turnOver = true, false, false, "", cancel, turn.NewID(h.now()), turnOver
	live := Live{Turn: h.turn, Steering: h.steering.ready, LeadStop: h.stopLead, Answers: h.answers, Origin: origin, Unattended: origin.Source == llm.SourceCronFire}
	h.recordUnrecorded()
	images := shownPending(task, h.pending)
	safe.Go(h.report, "turn", func() {
		defer close(turnOver)
		out := h.emitter()
		live.Emit = out.emit
		out.emit(Event{Kind: EventTurnStarted, ID: live.Turn, Text: task, Origin: wireOrigin(origin), Images: images})
		crashed := ""
		_ = safe.Call(func(p safe.Panic) {
			crashed = p.Saved(h.stateDir())
			out.emit(Event{Kind: EventFailure, Text: crashed})
		}, "turn", func() error {
			h.play(ctx, pick, task, live)
			return nil
		})
		cancel(nil)
		h.traceStop(h.ending(crashed))
		h.ended(out)
		h.armCheckIn()
	})
}

func (h *Host) ending(crashed string) session.Stop {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case crashed != "":
		return session.Stop{Kind: session.TurnEnd, By: "a crash", Why: crashed}
	case h.stopBy != "":
		return session.Stop{Kind: session.TurnEnd, By: h.stopBy, Why: "it was stopped"}
	}
	return session.Stop{Kind: session.TurnEnd, By: "the turn itself", Why: "the lead finished and no sub-agent was running"}
}

func (h *Host) Turn() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.turn, h.running
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
	if h.joins {
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
	h.inbox.Remembered("", note)
}

type StopAsked struct {
	By         string
	Why        string
	Everything bool
}

func (asked StopAsked) followed() StopAsked {
	asked.By = "member process " + strconv.Itoa(os.Getpid()) + ", " + asked.By
	return asked
}

func (h *Host) Stop(asked StopAsked) {
	if f := h.following.Load(); f != nil {
		f.ask(followAsk{Op: followStop, Stop: asked.followed()})
		return
	}
	h.mu.Lock()
	cancel, running := h.cancel, h.inbox.Running()
	everything := cancel != nil && (asked.Everything || len(running) == 0)
	if everything {
		h.stopped, h.cancelled, h.stopBy = true, true, asked.By
	}
	h.mu.Unlock()
	switch {
	case cancel == nil:
	case everything:
		h.traceStop(session.Stop{Kind: session.StopTurn, By: asked.By, Why: asked.Why, Took: append([]string{"the lead"}, running...)})
		cancel(errors.New("stopped by " + asked.By))
	default:
		if asked.Why != "" {
			asked.Why += "; "
		}
		asked.Why += "it did not ask to stop everything, so only the lead stops"
		h.StopLead(asked)
	}
}

func (h *Host) traceStop(stop session.Stop) { h.trace(stop.Said(), &stop) }

func (h *Host) trace(said string, stop *session.Stop) {
	h.mu.Lock()
	id, turnID, inbox := h.id, h.turn, h.inbox
	h.mu.Unlock()
	if id == "" {
		return
	}
	event, body := session.Event{Turn: turnID, Kind: session.EventNotice}, session.StopBody{NoticeBody: session.NoticeBody{Text: said, From: turn.RecordedOrigin(llm.Origin{Source: llm.SourceNotice})}, Stop: stop}
	written, err := inbox.Record(id, event, body)
	if !written && err == nil {
		err = appendNotice(h.dir, id, event, body)
	}
	if err != nil {
		h.note("the trace row for " + said + " was not written: " + err.Error())
	}
}

func appendNotice(dir, id string, event session.Event, body session.StopBody) error {
	store, err := session.OpenIn(dir)
	if err != nil {
		return err
	}
	if _, err := store.Header(id); err != nil {
		return nil
	}
	log, err := store.Open(session.Header{ID: id})
	if err != nil {
		return err
	}
	_, err = log.Append(event, body)
	return errors.Join(err, log.Close())
}

func (h *Host) Cancelled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cancelled
}

func (h *Host) StopLead(asked StopAsked) {
	if f := h.following.Load(); f != nil {
		f.ask(followAsk{Op: followStopLead, Stop: asked.followed()})
		return
	}
	h.mu.Lock()
	h.stopped = true
	inbox, turnRuns, kept := h.inbox, h.running, h.inbox.Running()
	if turnRuns && len(kept) == 0 {
		h.stopBy = asked.By
	}
	h.mu.Unlock()
	if turnRuns {
		h.traceStop(session.Stop{Kind: session.StopLead, By: asked.By, Why: asked.Why, Took: []string{"the lead's request, its waits and its calls"}, Kept: kept})
	}
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

func (h *Host) AllowAskFor(id string, until time.Time) (answered, stands bool) {
	if f := h.following.Load(); f != nil {
		reply := f.ask(followAsk{Op: followAllowFor, Asked: id, Until: until})
		return reply.OK, reply.Stands
	}
	return h.asks.grant(id, AllowedAWhile, until)
}

func (h *Host) Revoke(target string) (StandingAnswer, bool) {
	if f := h.following.Load(); f != nil {
		reply := f.ask(followAsk{Op: followRevoke, Asked: target})
		if !reply.OK || len(reply.Standing) == 0 {
			return StandingAnswer{}, false
		}
		return reply.Standing[0], true
	}
	return h.asks.revoke(target)
}

func (h *Host) Standing() []StandingAnswer {
	if f := h.following.Load(); f != nil {
		return append([]StandingAnswer{}, f.ask(followAsk{Op: followStanding}).Standing...)
	}
	return h.asks.standingNow()
}

func (h *Host) ClosingBy(by string) { h.endedBy(by, "process "+strconv.Itoa(os.Getpid())+" quit") }

func (h *Host) endedBy(by, why string) {
	h.mu.Lock()
	h.closeBy, h.closeWhy = by, why
	h.mu.Unlock()
}

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
	cancel, closing := h.cancel, h.closing()
	if cancel != nil {
		h.stopBy = closing.By
	}
	h.settled = append(h.settled, h.inbox.CancelCompactions())
	turnOver, settled, holding := h.turnOver, h.settled, h.heldID != ""
	h.mu.Unlock()
	if holding {
		h.traceStop(closing)
	}
	if cancel != nil {
		cancel(errors.New("closed by " + closing.By + ": " + closing.Why))
	}
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

func (h *Host) closing() session.Stop {
	closing := session.Stop{Kind: session.HostClose, By: cmp.Or(h.closeBy, "process "+strconv.Itoa(os.Getpid())), Why: cmp.Or(h.closeWhy, "its window or tofu serve closed the session")}
	if h.cancel != nil {
		closing.Took = append([]string{"the lead"}, h.inbox.Running()...)
	}
	return closing
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
	release, err := store.Hold(id)
	if err != nil {
		return err
	}
	moving := h.audience.Swap(nil)
	h.letGo()
	h.release, h.heldID = release, id
	switch {
	case release != nil:
		h.openAudience(moving)
	case moving != nil:
		moving.close()
	}
	return nil
}

func (h *Host) holds(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.heldID == id && h.release != nil
}

func (h *Host) enter(id string) error {
	if !h.joins {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.hold(id)
	}
	f, err := h.join(id)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.letGo()
	h.id = id
	h.follow(f, id)
	return nil
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
	h.ran, h.carried, h.carriedOn, h.prefix, h.shown = nil, resumed.Messages, "", &turn.Prefix{}, &shownResults{ids: map[string]bool{}}
	for _, message := range resumed.Messages[sinceLastReply(resumed.Messages):] {
		if message.Role == llm.RoleTool {
			h.shown.firstTime("", message.ToolCallID)
		}
	}
	if resumed.Session == "" {
		return nil
	}
	store, err := resumed.reading(h.dir)
	if err == nil && !h.joins {
		err = turn.RestoreSubAgents(store, resumed.Session, h.roster, h.inbox)
	}
	if err != nil {
		return fmt.Errorf("the sub-agents of %s were not all read back: %w", resumed.Session, err)
	}
	if header, err := store.Header(resumed.Session); err == nil {
		h.carriedOn = header.Wire
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
	h.forget()
	_ = h.carry(Carry{})
	h.id, h.pending, h.started = "", nil, SourceCleared
	return h.cron.Load("")
}

func (h *Host) Resume(carry Carry) ([]Event, error) {
	h.mu.Lock()
	h.dropCheckIn()
	running := h.running
	h.mu.Unlock()
	if running {
		return nil, errTurnRunning
	}
	h.forget()
	if err := h.enter(carry.Session); err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.id, h.pending, h.started = carry.Session, nil, SourceResumed
	restoreErr := h.carry(carry)
	h.mu.Unlock()
	if f := h.following.Load(); f != nil {
		f.caughtUp()
	}
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

func (h *Host) Opening(carry Carry) []Event { return resumedChat(carry, h.dir) }

func (h *Host) Compacted(into string, messages []llm.Message) error {
	h.mu.Lock()
	h.dropCheckIn()
	h.id, h.carried, h.prefix = into, messages, &turn.Prefix{}
	h.mu.Unlock()
	return h.enter(into)
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
	image := pendingImage{Index: index, Path: filepath.Join(store.AttachmentDir(id), name), Bytes: bytes, Format: format}
	h.mu.Lock()
	image.Recorded = h.release != nil
	h.pending = append(h.pending, image)
	h.mu.Unlock()
	if image.Recorded {
		recordAttached(store, id, image)
	}
}

func recordAttached(store *session.Store, id string, image pendingImage) {
	_ = store.AppendEvent(id, session.EventAttachment, session.Attachment{File: session.AttachmentPath(id, filepath.Base(image.Path)), Bytes: image.Bytes, Format: image.Format, N: image.Index})
}

func (h *Host) recordUnrecorded() {
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return
	}
	for i, image := range h.pending {
		if !image.Recorded {
			recordAttached(store, h.id, image)
			h.pending[i].Recorded = true
		}
	}
}

func (h *Host) received(images []pendingImage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending = append(h.pending, images...)
	h.recordUnrecorded()
}

func (h *Host) takePending(task string) []pendingImage {
	h.mu.Lock()
	defer h.mu.Unlock()
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
	wanted := h.takePending(task)
	images := make([]llm.Image, 0, len(wanted))
	for _, image := range wanted {
		data, err := os.ReadFile(image.Path)
		if err != nil {
			return nil, err
		}
		images = append(images, llm.Image{MediaType: imageMediaType(image.Path), Data: data}.Fitted())
	}
	return images, nil
}

func shownPending(task string, pending []pendingImage) []MessageImage {
	var shown []MessageImage
	for _, image := range pending {
		if strings.Contains(task, ImageToken(image.Index)) {
			shown = append(shown, MessageImage{N: image.Index, Path: image.Path, Mime: imageMediaType(image.Path)})
		}
	}
	return shown
}

func shownAttached(text string, attached []session.Attachment, store *session.Store) []MessageImage {
	var shown []MessageImage
	for k, n := range imageNumbers(text) {
		at := slices.IndexFunc(attached, func(image session.Attachment) bool { return image.N == n })
		if at < 0 && k < len(attached) && attached[k].N == 0 {
			at = k
		}
		if at >= 0 {
			path := store.AttachmentFile(attached[at].File)
			shown = append(shown, MessageImage{N: n, Path: path, Mime: imageMediaType(path)})
		}
	}
	return shown
}

func imageNumbers(text string) []int {
	var numbers []int
	for {
		_, after, found := strings.Cut(text, ImageTokenHead)
		if !found {
			return numbers
		}
		token, rest, _ := strings.Cut(after, "]")
		if n, err := strconv.Atoi(token); err == nil {
			numbers = append(numbers, n)
		}
		text = rest
	}
}

func imageMediaType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	return cmp.Or(imageMediaTypes()[ext], "image/"+strings.TrimPrefix(ext, "."))
}

func imageMediaTypes() map[string]string {
	return map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}
}
