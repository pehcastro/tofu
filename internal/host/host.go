package host

import (
	"cmp"
	"context"
	"encoding/json"
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

	"tofu/internal/agent/cron"
	"tofu/internal/agent/hooks"
	"tofu/internal/agent/loop"
	roster "tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/models/transport"
	"tofu/internal/platform/credential"
	"tofu/internal/platform/outside"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/store/memory/entries"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/shell/process"
)

const (
	ImageTokenHead = "[Image #"
	ranPreface     = "before sending this, the person ran a command in the project with !, outside any turn, and it printed:\n$ "
)

func ImageToken(index int) string { return ImageTokenHead + strconv.Itoa(index) + "]" }

func turnRunning(turn string) error {
	return errors.New("turn " + turn + " is running: wait for it to end, or stop it")
}

var errProjectTurn = errors.New("another session in this project's tofu host is already working, and this project lets one session work at a time: wait for it, or set oneWorkingSessionPerProject off")

type Play func(ctx context.Context, pick Pick, task string, live Live)

type Live struct {
	Turn       string
	Emit       func(Event)
	Steering   <-chan string
	LeadStop   <-chan struct{}
	Answers    <-chan Answer
	Origin     core.Origin
	Unattended bool
}

type Carry struct {
	Session  string
	Name     string
	Messages []core.Message
	Tasks    []string
	Store    *session.Store
	Keys     *credential.KeyRedactor
}

type Config struct {
	Dir         string
	Engine      Engine
	Play        Play
	Now         func() time.Time
	Shells      *process.Registry
	Check       cron.Checker
	Resumed     Carry
	ProjectTurn *sync.Mutex
	Joins       bool
	Unrecorded  bool
	ReadKeys    func() (map[string]string, error)
}

type Host struct {
	dir      string
	engine   Engine
	play     Play
	now      func() time.Time
	shells   *process.Registry
	events   chan Event
	bus      bus
	answers  chan Answer
	steering steerQueue
	stopLead chan struct{}
	sendNow  chan struct{}
	cron     *cron.Book
	cronMove chan struct{}
	turnLock *sync.Mutex
	heldKeys credential.HeldKeys
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
	cronOut   map[string]time.Duration
	answer    string
	previous  string
	streaming bool
	ticking   *time.Timer
	checkIn   *loop.LeadCheckIn
	checking  *time.Timer
	id        string
	started   hooks.SessionSource
	carried   []core.Message
	carriedOn string
	prefix    *loop.Prefix
	pending   []pendingImage
	ran       []string
	release   func() error
	heldID    string
	turn      string
	asking    AskingMode

	reads   *contract.ReadLedger
	inbox   *loop.Inbox
	settled []<-chan struct{}
	roster  *roster.Roster
	spent   *tokenTally
	shown   *shownResults
	asks    *asks
	judge   JudgeChain

	questions   *replies[answeredQuestion]
	memoryPicks *replies[entries.Scope]
}

type pendingImage struct {
	Index    int
	Path     string
	Bytes    int
	Format   string
	Recorded bool
}

func New(cfg Config) (h *Host, troubles []string) {
	opening, op := time.Now(), walls.OpSessionOpen
	if cfg.Resumed.Session != "" && !cfg.Unrecorded {
		op = walls.OpSessionResume
	}
	defer func() { h.opened(op, opening, len(troubles) == 0) }()
	cronMove := make(chan struct{}, 1)
	h = &Host{
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
		cronOut:     map[string]time.Duration{},
		turnLock:    cfg.ProjectTurn,
		heldKeys:    credential.HeldKeys{Read: cfg.ReadKeys},
		done:        make(chan struct{}),
		joins:       cfg.Joins,
		member:      MemberTUI,
		id:          cfg.Resumed.Session,
		started:     hooks.SessionStartup,
		asks:        &asks{standing: map[string]Answer{}, until: map[string]time.Time{}, given: map[string]time.Time{}},
		questions:   &replies[answeredQuestion]{},
		memoryPicks: &replies[entries.Scope]{},
		bus:         bus{heard: map[string]Resolution{}, noted: map[string]Resolution{}},
	}
	h.asks.session, h.asks.unkept, h.asks.granted = h.standingIn, h.standingUnkept, h.bind
	if h.now == nil {
		h.now = time.Now
	}
	if h.play == nil {
		h.play = h.run
	}
	if err := h.restorePick(cfg.Resumed); err != nil {
		troubles = append(troubles, err.Error())
	}
	if err := h.carry(cfg.Resumed); err != nil {
		troubles = append(troubles, err.Error())
	}
	h.armCron()
	if h.id == "" {
		return h, troubles
	}
	if !cfg.Unrecorded {
		h.started = hooks.SessionResumed
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

func (h *Host) Carried() []core.Message {
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
		started := h.keepUndelivered(f.send(pick, task, images), images).OK
		if started {
			h.mu.Lock()
			h.cancelled = false
			h.mu.Unlock()
		}
		return started
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return false
	}
	h.settle("", Pick{Wire: pick.Wire, Model: pick.Model, Effort: pick.Effort})
	h.begin(pick, task, core.Origin{Source: core.SourceTask})
	return true
}

func (h *Host) begin(pick Pick, task string, origin core.Origin) {
	h.dropCheckIn()
	clocked, _ := outside.Open(context.Background(), time.Now)
	ctx, cancel := context.WithCancelCause(loop.WithLeadCheckIn(clocked, h.keepCheckIn))
	turnOver := make(chan struct{})
	h.running, h.stopped, h.cancelled, h.stopBy, h.cancel, h.turn, h.turnOver = true, false, false, "", cancel, loop.NewID(h.now()), turnOver
	live := Live{Turn: h.turn, Steering: h.steering.ready, LeadStop: h.stopLead, Answers: h.answers, Origin: origin, Unattended: origin.Source == core.SourceCronFire}
	h.recordUnrecorded()
	images := shownPending(task, h.pending)
	safe.Go(h.report, "turn", func() {
		defer close(turnOver)
		out := h.emitter()
		live.Emit = out.emit
		out.emit(Event{Kind: EventTurnStarted, ID: live.Turn, Text: task, Origin: wireOrigin(origin), Images: images, At: h.now()})
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

func (h *Host) OpenFresh() (id string, err error) {
	opening := time.Now()
	defer func() { h.opened(walls.OpSessionOpen, opening, err == nil) }()
	if err := h.freshAlone(); err != nil {
		return "", err
	}
	id = h.pendingID()
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
		h.mu.Lock()
		h.cancelled = true
		h.mu.Unlock()
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
	event, body := session.Event{Turn: turnID, Kind: session.EventNotice}, session.StopBody{NoticeBody: session.NoticeBody{Text: said, From: loop.RecordedOrigin(core.Origin{Source: core.SourceNotice})}, Stop: stop}
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

func (h *Host) opened(op walls.Op, opening time.Time, ok bool) {
	if h.ID() == "" {
		return
	}
	role := "host"
	if h.joins {
		role = "member"
	}
	h.wall(walls.Wall{Op: op, Name: role, MS: time.Since(opening).Milliseconds(), At: opening.UnixMilli(), OK: ok, OutsideMS: new(int64)})
}

func (h *Host) wall(wall walls.Wall) {
	h.mu.Lock()
	id, inbox := cmp.Or(wall.Session, h.id), h.inbox
	h.mu.Unlock()
	wall.Session = id
	if written, _ := inbox.Record(id, session.Event{Kind: session.EventWall}, wall); !written {
		projectWall(h.dir, wall)
	}
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
	if answered, _ := h.asks.grant(id, answer, time.Time{}, h.noteOf(id)); answered {
		return true
	}
	select {
	case h.answers <- answer:
		return true
	default:
		return false
	}
}

func (h *Host) AnswerAsk(id string, answer Answer, said *Resolution) (answered, stands bool) {
	if f := h.following.Load(); f != nil {
		reply := f.ask(followAsk{Op: followAnswerAsk, Asked: id, Answer: answer, Said: said})
		return reply.OK, reply.Stands
	}
	return h.asks.grant(id, answer, time.Time{}, cmp.Or(said, h.noteOf(id)))
}

func (h *Host) AllowAskFor(id string, until time.Time, said *Resolution) (answered, stands bool) {
	if f := h.following.Load(); f != nil {
		reply := f.ask(followAsk{Op: followAllowFor, Asked: id, Until: until, Said: said})
		return reply.OK, reply.Stands
	}
	return h.asks.grant(id, AllowedAWhile, until, cmp.Or(said, h.noteOf(id)))
}

func (h *Host) Revoke(target string) (StandingAnswer, bool) {
	if f := h.following.Load(); f != nil {
		reply := f.ask(followAsk{Op: followRevoke, Asked: target})
		if !reply.OK || len(reply.Standing) == 0 {
			return StandingAnswer{}, false
		}
		return reply.Standing[0], true
	}
	gone, revoked := h.asks.revoke(target)
	if revoked {
		gone = withKept(gone)
	}
	return gone, revoked
}

func (h *Host) Standing() []StandingAnswer {
	if f := h.following.Load(); f != nil {
		return append([]StandingAnswer{}, f.ask(followAsk{Op: followStanding}).Standing...)
	}
	now := h.asks.standingNow()
	for at := range now {
		now[at] = withKept(now[at])
	}
	return now
}

func withKept(one StandingAnswer) StandingAnswer {
	one.Kept = KeptBesideSession
	if heldInMemoryOnly(one) {
		one.Kept = KeptUntilRestart
	}
	return one
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
	dir, _ := platformproject.ProjectStateDirAt(h.dir)
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

func (h *Host) writes(id string) bool {
	now := h.seen().Session
	return h.ID() == id || now != nil && now.ID == id
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
	return filepath.Join(store.Dir(id), konst.CronBookFile)
}

func (h *Host) carry(resumed Carry) error {
	if h.inbox != nil {
		h.settled = append(h.settled, h.inbox.CancelCompactions())
	}
	if h.engine != nil {
		h.engine.Renew()
	}
	h.reads, h.inbox, h.roster, h.spent = contract.NewReadLedger(), loop.NewInbox(), &roster.Roster{}, &tokenTally{}
	h.ran, h.carried, h.carriedOn, h.prefix, h.shown = nil, resumed.Messages, "", &loop.Prefix{}, &shownResults{ids: map[string]bool{}}
	for _, message := range resumed.Messages[sinceLastReply(resumed.Messages):] {
		if message.Role == core.RoleTool {
			h.shown.firstTime("", message.ToolCallID)
		}
	}
	if resumed.Session == "" {
		return nil
	}
	store, err := resumed.reading(h.dir)
	if err == nil && !h.joins {
		err = loop.RestoreSubAgents(store, resumed.Session, h.roster, h.inbox)
	}
	if err != nil {
		return fmt.Errorf("the sub-agents of %s were not all read back: %w", resumed.Session, err)
	}
	if h.prefix, err = loop.HeldPrefix(store, resumed.Session, ""); err != nil {
		h.prefix = &loop.Prefix{}
		err = fmt.Errorf("the system prompt and tools %s first sent were not read back, so the next turn sends them anew: %w", resumed.Session, err)
		if !h.joins {
			notice := session.StopBody{NoticeBody: session.NoticeBody{Text: err.Error(), From: loop.RecordedOrigin(core.Origin{Source: core.SourceNotice})}}
			err = errors.Join(err, appendNotice(h.dir, resumed.Session, session.Event{Kind: session.EventNotice}, notice))
		}
		return err
	}
	if header, err := store.Header(resumed.Session); err == nil {
		h.carriedOn = header.Wire
	}
	side, found, err := store.Side(resumed.Session)
	if err != nil || !found || side.Effort == "" {
		return err
	}
	effort, err := core.ParseEffort(side.Effort)
	if err == nil {
		h.pick.Effort = effort
	}
	return err
}

func (h *Host) restorePick(resumed Carry) error {
	if resumed.Session == "" {
		return nil
	}
	store, err := resumed.reading(h.dir)
	if err != nil {
		return nil
	}
	chosen, err := SessionPick(store, h.dir, resumed.Session)
	if chosen.Model != "" {
		h.pick.Wire, h.pick.Model = chosen.Wire, chosen.Model
	}
	return err
}

func SessionPick(store *session.Store, dir, id string) (Pick, error) {
	header, err := store.Header(id)
	if err != nil {
		return Pick{}, nil
	}
	library, chosen := libraryAt(dir), header.Chosen
	side, isSide, _ := store.Side(id)
	if model, known := library.Resolve(side.Model); chosen == nil && isSide && known && model.Slug() == side.Model {
		chosen = &session.Chosen{Wire: library.WireOf(model), Model: side.Model}
	}
	if chosen == nil {
		return Pick{}, nil
	}
	if model, known := library.Resolve(chosen.Model); known && library.WireOf(model) == chosen.Wire {
		return Pick{Wire: chosen.Wire, Model: model.Slug()}, nil
	}
	if !isSide {
		return Pick{}, nil
	}
	return Pick{}, fmt.Errorf("side chat %s runs on %s, which the model library no longer has: pick another with tofu session set %s --model source/model",
		id, chosen.Model, id)
}

func SettleHeld(project, id string, pick Pick) (bool, error) {
	if len(MembersOf(project)[id]) == 0 {
		return false, nil
	}
	conn, lines, _, err := joinHub(project, id, Member{Process: os.Getpid(), Kind: MemberServe})
	if err != nil {
		return true, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(followReplyWait))
	var first followLine
	if err := lines.Decode(&first); err != nil {
		return true, err
	}
	if first.Snapshot != nil {
		pick.Effort = cmp.Or(pick.Effort, first.Snapshot.Settings.Pick.Effort)
	}
	if err := json.NewEncoder(conn).Encode(followAsk{ID: 1, Op: followSettings, Pick: pick}); err != nil {
		return true, err
	}
	for {
		var line followLine
		if err := lines.Decode(&line); err != nil {
			return true, err
		}
		if line.Reply == nil || line.Reply.ID != 1 {
			continue
		}
		if line.Reply.Refused != "" {
			return true, errors.New(line.Reply.Refused)
		}
		return true, nil
	}
}

func (c Carry) reading(dir string) (*session.Store, error) {
	if c.Store != nil {
		return c.Store, nil
	}
	return session.OpenIn(dir)
}

func (h *Host) Fresh() error {
	if f := h.following.Load(); f != nil {
		if refused := f.renew(); refused != "" {
			return turnRunning(refused)
		}
	}
	return h.freshAlone()
}

func (h *Host) freshAlone() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropCheckIn()
	if h.running {
		return turnRunning(h.turn)
	}
	h.letGo()
	h.forget()
	_ = h.carry(Carry{})
	h.id, h.pending, h.started = "", nil, hooks.SessionCleared
	return h.cron.Load("")
}

func (h *Host) Resume(carry Carry) (_ Opened, err error) {
	opening := time.Now()
	defer func() { h.opened(walls.OpSessionResume, opening, err == nil) }()
	h.mu.Lock()
	h.dropCheckIn()
	turn, running := h.turn, h.running
	h.mu.Unlock()
	if running {
		return Opened{}, turnRunning(turn)
	}
	h.forget()
	h.mu.Lock()
	was := h.pick
	err = h.restorePick(carry)
	h.mu.Unlock()
	if err == nil {
		err = h.enter(carry.Session)
	}
	if err != nil {
		h.mu.Lock()
		h.pick = was
		h.mu.Unlock()
		return Opened{}, err
	}
	h.mu.Lock()
	h.id, h.pending, h.started = carry.Session, nil, hooks.SessionResumed
	restoreErr := h.carry(carry)
	h.mu.Unlock()
	if f := h.following.Load(); f != nil {
		f.caughtUp()
	}
	opened := h.Opening(carry)
	if restoreErr != nil {
		opened.Chat = append(opened.Chat, Event{Kind: EventNote, Text: restoreErr.Error()})
	}
	if err := h.loadCron(carry.Session); err != nil {
		opened.Chat = append(opened.Chat, Event{Kind: EventNote, Text: err.Error()})
	}
	h.armCron()
	return opened, nil
}

func (h *Host) Opening(carry Carry) Opened {
	if carry.Keys == nil {
		held := h.keys()
		carry.Keys = &held.redactor
	}
	return resumedChat(carry, h.dir)
}

func (h *Host) Compacted(into string, messages []core.Message) error {
	h.mu.Lock()
	h.dropCheckIn()
	h.id, h.carried, h.started = into, messages, hooks.SessionCompact
	h.mu.Unlock()
	if err := h.enter(into); err != nil {
		return err
	}
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return err
	}
	if !h.joins {
		if err := h.cron.Keep(cronFile(store, into)); err != nil {
			return fmt.Errorf("cron jobs were not written: %w", err)
		}
	}
	h.emitter().emit(labelledAs(store, Event{Kind: EventSession, ID: into, Root: into}))
	return nil
}

func (h *Host) Ran(command, output string, stopped bool) string {
	redactor := h.keys().redactor
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

func (h *Host) takePendingImages(task string) ([]core.Image, error) {
	wanted := h.takePending(task)
	images := make([]core.Image, 0, len(wanted))
	for _, image := range wanted {
		data, err := os.ReadFile(image.Path)
		if err != nil {
			return nil, err
		}
		images = append(images, core.Image{MediaType: imageMediaType(image.Path), Data: data}.Fitted())
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

const RegexJudge = "the regex arm"

type judgeChainKey struct{}

type JudgeChain struct {
	mu           sync.Mutex
	session      string
	tried        []classifierTry
	judge        string
	byClassifier bool
	noModel      error
	told         string
	source       string
	open         func(ctx context.Context, slug string) (loop.Model, error)
}

type classifierTry struct {
	ClassifierHealth
	err  error
	skip bool
}

func JudgeChainOf(ctx context.Context) *JudgeChain {
	if chain, held := ctx.Value(judgeChainKey{}).(*JudgeChain); held {
		return chain
	}
	return &JudgeChain{}
}

func (c *JudgeChain) during(ctx context.Context, session, source string, open func(context.Context, string) (loop.Model, error)) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != session {
		c.tried, c.judge, c.byClassifier, c.noModel, c.told = nil, "", false, nil, ""
	}
	c.session, c.source, c.open = session, source, open
	return context.WithValue(ctx, judgeChainKey{}, c)
}

func (c *JudgeChain) Source() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.source
}

func (c *JudgeChain) Skipped(provider string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, try := range c.tried {
		if try.Provider == provider && try.skip {
			return try.err
		}
	}
	return nil
}

func (c *JudgeChain) Tried(provider string, err error) {
	try := classifierTry{ClassifierHealth: ClassifierHealth{Provider: provider, State: ClassifierOK}, err: err}
	var refused *transport.Error
	if errors.As(err, &refused) {
		try.Status = refused.Status
	}
	if err != nil {
		try.State, try.skip = classifierStateOf(transport.KindOf(err))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if at := slices.IndexFunc(c.tried, func(held classifierTry) bool { return held.Provider == provider }); at >= 0 {
		c.tried[at] = try
		return
	}
	c.tried = append(c.tried, try)
}

func classifierStateOf(kind transport.Kind) (state ClassifierState, skipForTheSession bool) {
	switch kind {
	case transport.KindBilling, transport.KindBudget:
		return ClassifierOutOfCredits, true
	case transport.KindAuth, transport.KindMissingCredential:
		return ClassifierRefusing, true
	case transport.KindModelAccess, transport.KindRequestTooLarge, transport.KindBadRequest, transport.KindRateLimit, transport.KindInvalidAnswer:
		return ClassifierRefusing, false
	case transport.KindUnknown, transport.KindTimeout, transport.KindProvider:
		return ClassifierUnreachable, false
	}
	panic("host: unknown transport kind " + kind.String())
}

func (c *JudgeChain) Open(ctx context.Context, slug string) (loop.Model, error) {
	c.mu.Lock()
	open := c.open
	c.mu.Unlock()
	if open == nil {
		return nil, errors.New("no plan model is open in this session to judge for them")
	}
	return open(ctx, slug)
}

func (c *JudgeChain) JudgedByClassifier(slug string) { c.judged(slug, true, nil) }

func (c *JudgeChain) JudgedByModel(slug string) { c.judged(slug, false, nil) }

func (c *JudgeChain) JudgedByRegex(noModel error) { c.judged(RegexJudge, false, noModel) }

func (c *JudgeChain) judged(by string, byClassifier bool, noModel error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.judge, c.byClassifier, c.noModel = by, byClassifier, noModel
}

func (c *JudgeChain) tell() (line, accent string, changed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var failed []classifierTry
	for _, try := range c.tried {
		if try.State != ClassifierOK {
			failed = append(failed, try)
		}
	}
	accent = c.judge
	switch {
	case c.judge == "":
	case c.byClassifier && len(failed) == 0 && c.told != "":
		line = "judge is back on " + c.judge
	case c.byClassifier && len(failed) == 0:
	case c.noModel == nil:
		line = "judge is running on " + c.judge + " because " + c.because(failed)
	default:
		accent, line = "", "judge is running on "+RegexJudge+" because "+c.because(failed)+", and "+c.noModel.Error()
	}
	if line == "" || line == c.told {
		return "", "", false
	}
	c.told = line
	return line, accent, true
}

func (c *JudgeChain) because(failed []classifierTry) string {
	if len(failed) > 1 && len(failed) == len(c.tried) {
		unreachable := 0
		for _, try := range failed {
			if try.State == ClassifierUnreachable {
				unreachable++
			}
		}
		switch unreachable {
		case 0:
			return "all classifiers configured are out of credit"
		case len(failed):
			return "all classifiers configured are unreachable"
		}
		return "all classifiers configured failed"
	}
	said := make([]string, len(failed))
	for i, try := range failed {
		said[i] = try.Provider + " is " + string(try.State)
	}
	return strings.Join(said, " and ")
}

func (c *JudgeChain) status() *JudgeStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.judge == "" {
		return nil
	}
	status := &JudgeStatus{Classifiers: make([]ClassifierHealth, len(c.tried)), Judge: c.judge, Line: c.told}
	for i, try := range c.tried {
		status.Classifiers[i] = try.ClassifierHealth
	}
	return status
}
