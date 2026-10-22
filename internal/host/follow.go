package host

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"tofu/internal/cron"
	"tofu/internal/llm"
	"tofu/internal/memory"
	"tofu/internal/safe"
	"tofu/internal/session"
	"tofu/internal/turn"
)

const (
	followQueue        = 1024
	followReplyWait    = 3 * time.Second
	followDialWait     = time.Second
	followTakeOverWait = 2 * time.Second
	followRetry        = 50 * time.Millisecond
	followNetwork      = "unix"
)

type followOp string

const (
	followSend      followOp = "send"
	followStop      followOp = "stop"
	followStopLead  followOp = "stopLead"
	followAnswer    followOp = "answer"
	followAnswerAsk followOp = "answerAsk"
	followAllowFor  followOp = "allowFor"
	followRevoke    followOp = "revoke"
	followStanding  followOp = "standing"
	followQuestion  followOp = "question"
	followMemory    followOp = "memory"
	followSteer     followOp = "steer"
	followSendNow   followOp = "sendNow"
	followUnsteer   followOp = "unsteer"
	followUnqueue   followOp = "unqueue"
	followAttach    followOp = "attach"
	followRan       followOp = "ran"
	followRemember  followOp = "remember"
	followCron      followOp = "cron"
	followRoster    followOp = "roster"
	followSettings  followOp = "settings"
)

type followAsk struct {
	ID       int
	Op       followOp
	Text     string
	Asked    string
	Answer   Answer
	Question QuestionAnswer
	Scope    memory.Scope `json:",omitempty"`
	By       string
	Pick     Pick
	Asking   AskingMode     `json:",omitempty"`
	Images   []pendingImage `json:",omitempty"`
	Command  string         `json:",omitempty"`
	Stopped  bool           `json:",omitempty"`
	Stop     StopAsked      `json:",omitzero"`
	Until    time.Time      `json:",omitzero"`
}

type followReply struct {
	ID       int
	OK       bool
	Stands   bool
	Steered  string
	Refused  string `json:",omitempty"`
	Cron     cron.Reply
	Failed   string           `json:",omitempty"`
	Agents   []SubAgentRow    `json:",omitempty"`
	Standing []StandingAnswer `json:",omitempty"`
}

type followLine struct {
	Event    *Event        `json:",omitempty"`
	Reply    *followReply  `json:",omitempty"`
	Settings *heldSettings `json:",omitempty"`
}

type heldSettings struct {
	Asking AskingMode
	Pick   Pick
}

type audience struct {
	mu       sync.Mutex
	listener net.Listener
	seats    map[*seat]struct{}
	attached int
	changed  chan struct{}
	started  *Event
	partial  string
	asks     []Event
	steers   []Event
	held     heldSettings
}

type link struct {
	conn net.Conn
	gone chan struct{}
	once sync.Once
}

type seat struct {
	link
	out      chan []byte
	attached bool
}

type following struct {
	link
	holder  session.BusyError
	mu      sync.Mutex
	asked   int
	waiting map[int]chan followReply
	typed   string
	unsent  string
	chose   Pick
}

func linked(conn net.Conn) link { return link{conn: conn, gone: make(chan struct{})} }

func (l *link) leave() {
	l.once.Do(func() {
		close(l.gone)
		_ = l.conn.Close()
	})
}

func (h *Host) openAudience(store *session.Store, id string, a *audience) {
	path := store.Channel(id)
	_ = os.Remove(path)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	listener, err := net.Listen(followNetwork, path)
	if err != nil {
		if a != nil {
			a.close()
		}
		return
	}
	if a == nil {
		a = &audience{seats: map[*seat]struct{}{}, changed: make(chan struct{}, 1)}
	}
	a.mu.Lock()
	moved := a.listener
	a.listener, a.held = listener, heldSettings{Asking: h.asking, Pick: h.pick}
	a.mu.Unlock()
	if moved != nil {
		_ = moved.Close()
	}
	h.audience.Store(a)
	safe.Go(h.report, "follow admit", func() { h.admit(a, listener) })
}

func (h *Host) admit(a *audience, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		s := &seat{link: linked(conn), out: make(chan []byte, followQueue)}
		a.mu.Lock()
		if a.seats == nil {
			a.mu.Unlock()
			_ = conn.Close()
			continue
		}
		if line, err := json.Marshal(followLine{Settings: &a.held}); err == nil {
			s.send(line)
		}
		for _, event := range h.catchUp(a) {
			if line, err := json.Marshal(followLine{Event: &event}); err == nil {
				s.send(line)
			}
		}
		a.seats[s] = struct{}{}
		a.mu.Unlock()
		safe.Go(h.report, "follow write", s.write)
		safe.Go(h.report, "follow read", func() { h.serveSeat(a, s) })
	}
}

func (h *Host) catchUp(a *audience) []Event {
	var caught []Event
	if a.started != nil {
		caught = append(caught, *a.started)
		if a.partial != "" {
			caught = append(caught, Event{Kind: EventTextDelta, Text: a.partial})
		}
	}
	h.steering.mu.Lock()
	queued := h.steering.queued()
	h.steering.mu.Unlock()
	a.steers = slices.DeleteFunc(a.steers, func(steer Event) bool { return !slices.Contains(queued, steer.ID) })
	return append(append(caught, a.asks...), a.steers...)
}

func (a *audience) tell(event Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case event.Kind == EventTurnStarted:
		a.started, a.partial = &event, ""
	case event.Kind == EventTurnEnded:
		a.started, a.partial = nil, ""
	case event.Kind == EventTextDelta && event.Agent == "":
		a.partial += event.Text
	case endsTheLeadReply(event):
		a.partial = ""
	}
	switch event.Kind {
	case EventAwaitPerson:
		a.asks = append(a.asks, event)
	case EventResumed:
		a.asks = slices.DeleteFunc(a.asks, func(asked Event) bool { return asked.ID == event.ID })
	case EventSteerQueued:
		a.steers = append(a.steers, event)
	}
	a.broadcast(followLine{Event: &event})
}

func (a *audience) settle(held heldSettings) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.held = held
	a.broadcast(followLine{Settings: &held})
}

func (a *audience) broadcast(said followLine) {
	if len(a.seats) == 0 {
		return
	}
	line, err := json.Marshal(said)
	if err != nil {
		return
	}
	for s := range a.seats {
		if !s.send(line) {
			a.unseat(s)
		}
	}
}

func (a *audience) unseat(s *seat) {
	if _, seated := a.seats[s]; !seated {
		return
	}
	delete(a.seats, s)
	s.leave()
	if s.attached {
		a.attached--
		a.changes()
	}
}

func (a *audience) attach(s *seat) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, seated := a.seats[s]; seated && !s.attached {
		s.attached = true
		a.attached++
		a.changes()
	}
}

func (a *audience) changes() {
	select {
	case a.changed <- struct{}{}:
	default:
	}
}

func (a *audience) attachedNow() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.attached
}

func (a *audience) drop(s *seat) {
	a.mu.Lock()
	a.unseat(s)
	a.mu.Unlock()
	s.leave()
}

func (a *audience) close() {
	a.mu.Lock()
	listener, seats := a.listener, a.seats
	a.seats, a.attached = nil, 0
	a.changes()
	a.mu.Unlock()
	_ = listener.Close()
	for s := range seats {
		s.leave()
	}
}

func (s *seat) send(line []byte) bool {
	select {
	case s.out <- line:
		return true
	default:
		return false
	}
}

func (s *seat) write() {
	for {
		select {
		case line := <-s.out:
			if _, err := s.conn.Write(line); err != nil {
				s.leave()
				return
			}
		case <-s.gone:
			return
		}
	}
}

func (h *Host) serveSeat(a *audience, s *seat) {
	defer a.drop(s)
	asks := json.NewDecoder(s.conn)
	for {
		var raw json.RawMessage
		if err := asks.Decode(&raw); err != nil {
			line, _ := json.Marshal(followLine{Reply: &followReply{Refused: err.Error()}})
			_, _ = s.conn.Write(line)
			return
		}
		reply := h.served(a, s, raw)
		line, _ := json.Marshal(followLine{Reply: &reply})
		if !s.send(line) {
			return
		}
	}
}

func (h *Host) served(a *audience, s *seat, raw json.RawMessage) followReply {
	var ask followAsk
	if err := json.Unmarshal(raw, &ask); err != nil {
		return followReply{ID: ask.ID, Refused: err.Error()}
	}
	reply := followReply{ID: ask.ID, OK: true}
	switch ask.Op {
	case followSend:
		h.received(ask.Images)
		if _, running := h.Turn(); running {
			reply.OK = false
			break
		}
		_, pick := h.Settings()
		if ask.Pick != (Pick{}) {
			pick = ask.Pick
		}
		h.emitter().emit(Event{Kind: EventTask, Text: ask.Text, Origin: wireOrigin(llm.Origin{Source: llm.SourceTask}), Images: shownPending(ask.Text, ask.Images)})
		reply.OK = h.Send(pick, ask.Text)
	case followStop:
		h.Stop(ask.Stop)
	case followStopLead:
		h.StopLead(ask.Stop)
	case followAnswer:
		reply.OK = h.Answer(ask.Asked, ask.Answer)
	case followAnswerAsk:
		reply.OK, reply.Stands = h.AnswerAsk(ask.Asked, ask.Answer)
	case followAllowFor:
		reply.OK, reply.Stands = h.AllowAskFor(ask.Asked, ask.Until)
	case followRevoke:
		gone, revoked := h.Revoke(ask.Asked)
		reply.OK, reply.Standing = revoked, []StandingAnswer{gone}
	case followStanding:
		reply.Standing = h.Standing()
	case followQuestion:
		reply.OK = h.AnswerQuestion(ask.Asked, ask.Question, ask.By)
	case followMemory:
		reply.OK = h.AnswerMemory(ask.Asked, ask.Scope)
	case followSteer:
		h.received(ask.Images)
		reply.Steered = h.steerFor(ask.Text)
	case followSendNow:
		reply.OK = h.SendNow(ask.Text)
	case followUnsteer:
		reply.OK = h.Unsteer(ask.Text)
	case followUnqueue:
		h.DropSteering()
	case followAttach:
		a.attach(s)
	case followSettings:
		h.Settle(ask.Asking, ask.Pick)
	case followRan:
		h.Ran(ask.Command, ask.Text, ask.Stopped)
	case followRemember:
		h.Remembered(ask.Text)
	case followCron:
		var err error
		if reply.Cron, err = h.CronCommand(ask.Text); err != nil {
			reply.OK, reply.Failed = false, err.Error()
		}
	case followRoster:
		h.mu.Lock()
		held := h.roster
		h.mu.Unlock()
		reply.Agents = SubAgentRows(held.SubAgents(), time.Now(), 0, nil, nil)
	default:
		reply.OK = false
	}
	return reply
}

func (h *Host) steerFor(text string) string {
	id := h.Steer(text)
	h.emitter().emit(Event{Kind: EventSteerQueued, ID: id, Text: text})
	return id
}

func (h *Host) dialHolder(id string, refused error) (*following, error) {
	var busy session.BusyError
	if !errors.As(refused, &busy) {
		return nil, refused
	}
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(followNetwork, store.Channel(id), followDialWait)
	if err != nil {
		return nil, err
	}
	return &following{link: linked(conn), holder: busy, waiting: map[int]chan followReply{}}, nil
}

func (h *Host) follow(f *following, id string) {
	settled := followAsk{Op: followSettings, Asking: h.asking, Pick: h.pick}
	h.following.Store(f)
	h.readOnly = f.holder
	safe.Go(h.report, "follow", func() { h.heed(f) })
	_ = h.loadCron(id)
	if os.Getenv(HolderVariable) != id {
		safe.Go(h.report, "follow attach", func() {
			f.ask(followAsk{Op: followAttach})
			f.ask(settled)
		})
	}
}

func heldRoster(store *session.Store, id string) []SubAgentRow {
	conn, err := net.DialTimeout(followNetwork, store.Channel(id), followDialWait)
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()
	asked, _ := json.Marshal(followAsk{ID: 1, Op: followRoster})
	if conn.SetDeadline(time.Now().Add(followReplyWait)) != nil {
		return nil
	}
	if _, err := conn.Write(asked); err != nil {
		return nil
	}
	lines := json.NewDecoder(conn)
	for {
		var line followLine
		if lines.Decode(&line) != nil {
			return nil
		}
		if line.Reply != nil && line.Reply.ID == 1 {
			return line.Reply.Agents
		}
	}
}

func (f *following) note(id string) string {
	return "process " + strconv.Itoa(f.holder.PID) + " has held session " + id + " since " + f.holder.Since.Format(time.TimeOnly) + ", so this tofu follows it and what you type runs there"
}

func (f *following) ask(ask followAsk) followReply {
	reply := make(chan followReply, 1)
	f.mu.Lock()
	if ask.Op == followSteer && ask.Text == f.unsent {
		f.unsent = ""
		f.mu.Unlock()
		return followReply{}
	}
	f.asked++
	ask.ID = f.asked
	f.waiting[ask.ID] = reply
	line, err := json.Marshal(ask)
	if err == nil {
		_ = f.conn.SetWriteDeadline(time.Now().Add(followReplyWait))
		_, err = f.conn.Write(line)
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.waiting, ask.ID)
		f.mu.Unlock()
	}()
	if err != nil {
		return followReply{}
	}
	select {
	case got := <-reply:
		return got
	case <-f.gone:
	case <-time.After(followReplyWait):
	}
	return followReply{}
}

func (f *following) send(pick Pick, task string, images []pendingImage) followReply {
	f.mu.Lock()
	f.typed = task
	f.mu.Unlock()
	reply := f.ask(followAsk{Op: followSend, Text: task, Pick: pick, Images: images})
	if !reply.delivered() {
		f.mu.Lock()
		f.unsent = task
		f.mu.Unlock()
	}
	return reply
}

func (reply followReply) delivered() bool { return reply.Refused == "" && reply.ID != 0 }

func (h *Host) keepUndelivered(reply followReply, images []pendingImage) followReply {
	if !reply.delivered() {
		h.mu.Lock()
		h.pending = append(h.pending, images...)
		h.mu.Unlock()
	}
	return reply
}

func (f *following) chosen(pick Pick) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	moved := f.chose != pick
	f.chose = pick
	return moved
}

func (f *following) echoes(event Event) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	echo := (event.Kind == EventTask || event.Kind == EventSteerQueued) && event.Text == f.typed
	if echo {
		f.typed = ""
	}
	return echo
}

func (h *Host) heed(f *following) {
	lines := json.NewDecoder(f.conn)
	for {
		var raw json.RawMessage
		if lines.Decode(&raw) != nil {
			break
		}
		var line followLine
		_ = json.Unmarshal(raw, &line)
		if held := line.Settings; held != nil {
			h.mu.Lock()
			h.asking, h.pick = held.Asking, held.Pick
			h.mu.Unlock()
		}
		if line.Reply != nil && line.Reply.Refused != "" {
			h.note("process " + strconv.Itoa(f.holder.PID) + " could not read what this tofu sent, so it did not run: " + line.Reply.Refused)
		}
		if line.Reply != nil {
			f.mu.Lock()
			waiting := f.waiting[line.Reply.ID]
			f.mu.Unlock()
			if waiting != nil {
				waiting <- *line.Reply
			}
		}
		if line.Event != nil && !f.echoes(*line.Event) {
			h.mirror(f, *line.Event)
		}
	}
	f.leave()
	h.takeOver(f)
}

func (h *Host) mirror(f *following, event Event) {
	if string(event.Args) == "null" {
		event.Args = nil
	}
	switch {
	case event.Kind == EventTurnStarted || event.Kind == EventTurnEnded:
		h.mu.Lock()
		h.running = event.Kind == EventTurnStarted
		if h.running {
			h.turn = event.ID
		}
		h.mu.Unlock()
	case event.Kind == EventSession && event.ID != "":
		h.mu.Lock()
		h.id = event.ID
		h.mu.Unlock()
	}
	if event.snapshot() {
		select {
		case h.events <- event:
		default:
		}
		return
	}
	select {
	case h.events <- event:
	case <-f.gone:
	}
}

func (h *Host) takeOver(f *following) {
	lost := time.Now()
	id := h.ID()
	for ; time.Since(lost) < followTakeOverWait; time.Sleep(followRetry) {
		h.mu.Lock()
		if h.closed || h.following.Load() != f {
			h.mu.Unlock()
			return
		}
		refused := h.hold(id)
		if refused == nil {
			ran := h.running
			h.readOnly, h.running = nil, false
			reloaded := h.reload(id)
			h.mu.Unlock()
			h.tookOver(f, id, time.Since(lost), ran, reloaded)
			return
		}
		next, err := h.dialHolder(id, refused)
		if err == nil {
			ran := h.running && next.holder.PID != f.holder.PID
			h.running = h.running && !ran
			h.follow(next, id)
			h.mu.Unlock()
			if ran {
				h.endedWith(f)
			}
			h.note(next.note(id))
			return
		}
		h.mu.Unlock()
	}
	h.following.CompareAndSwap(f, nil)
	h.note("process " + strconv.Itoa(f.holder.PID) + " let go of session " + id + ", and it is still held after " + followTakeOverWait.String() + ", so this tofu reads it and writes nothing")
}

func (h *Host) endedWith(f *following) {
	out := h.emitter()
	out.emit(Event{Kind: EventNote, Text: "the turn process " + strconv.Itoa(f.holder.PID) + " was running ended with it"})
	out.emit(Event{Kind: EventTurnEnded})
}

func (h *Host) tookOver(f *following, id string, took time.Duration, ran bool, reloaded error) {
	if ran {
		h.endedWith(f)
		h.traceStop(session.Stop{Kind: session.TurnEnd, By: "the holder, process " + strconv.Itoa(f.holder.PID), Why: "it exited or crashed with the turn running, and process " + strconv.Itoa(os.Getpid()) + " took the session over"})
	}
	out := h.emitter()
	out.emit(Event{Kind: EventNote, Text: "process " + strconv.Itoa(f.holder.PID) + " let go of session " + id + ", so this tofu holds it now and runs what you type (took " + took.Round(time.Millisecond).String() + ")"})
	if reloaded != nil {
		out.emit(Event{Kind: EventNote, Text: "the conversation of " + id + " did not read back, so the next turn starts without it: " + reloaded.Error()})
	}
	if err := h.loadCron(id); err != nil {
		out.emit(Event{Kind: EventNote, Text: err.Error()})
	}
	h.armCron()
}

func (h *Host) reload(id string) error {
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return err
	}
	events, err := store.Events(id)
	if err != nil {
		return err
	}
	said, _, err := store.Said(events)
	if err != nil {
		return err
	}
	messages, err := turn.ConversationFrom(said)
	if err != nil {
		return err
	}
	h.id = id
	return h.carry(Carry{Session: id, Messages: messages, Store: store})
}

func (h *Host) heldBy() *SessionHolder {
	if f := h.following.Load(); f != nil {
		return &SessionHolder{PID: f.holder.PID, Since: f.holder.Since}
	}
	return nil
}
