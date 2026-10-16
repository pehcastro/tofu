package host

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

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
	followQuestion  followOp = "question"
	followMemory    followOp = "memory"
	followSteer     followOp = "steer"
	followSendNow   followOp = "sendNow"
	followUnsteer   followOp = "unsteer"
	followUnqueue   followOp = "unqueue"
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
}

type followReply struct {
	ID      int
	OK      bool
	Stands  bool
	Steered string
	Refused string `json:",omitempty"`
}

type followLine struct {
	Event *Event       `json:",omitempty"`
	Reply *followReply `json:",omitempty"`
}

type audience struct {
	listener net.Listener
	mu       sync.Mutex
	seats    map[*seat]struct{}
	started  *Event
	partial  string
}

type link struct {
	conn net.Conn
	gone chan struct{}
	once sync.Once
}

type seat struct {
	link
	out chan []byte
}

type following struct {
	link
	holder  session.BusyError
	mu      sync.Mutex
	asked   int
	waiting map[int]chan followReply
	typed   string
	unsent  string
}

func linked(conn net.Conn) link { return link{conn: conn, gone: make(chan struct{})} }

func (l *link) leave() {
	l.once.Do(func() {
		close(l.gone)
		_ = l.conn.Close()
	})
}

func (h *Host) openAudience(store *session.Store, id string) {
	path := store.Channel(id)
	_ = os.Remove(path)
	listener, err := net.Listen(followNetwork, path)
	if err != nil {
		return
	}
	a := &audience{listener: listener, seats: map[*seat]struct{}{}}
	h.audience.Store(a)
	safe.Go(h.report, "follow admit", func() { h.admit(a) })
}

func (h *Host) admit(a *audience) {
	for {
		conn, err := a.listener.Accept()
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
		for _, event := range a.catchUp() {
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

func (a *audience) catchUp() []Event {
	if a.started == nil {
		return nil
	}
	if a.partial == "" {
		return []Event{*a.started}
	}
	return []Event{*a.started, {Kind: EventTextDelta, Text: a.partial}}
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
	if len(a.seats) == 0 {
		return
	}
	line, err := json.Marshal(followLine{Event: &event})
	if err != nil {
		return
	}
	for s := range a.seats {
		if !s.send(line) {
			delete(a.seats, s)
			s.leave()
		}
	}
}

func (a *audience) drop(s *seat) {
	a.mu.Lock()
	delete(a.seats, s)
	a.mu.Unlock()
	s.leave()
}

func (a *audience) close() {
	_ = a.listener.Close()
	a.mu.Lock()
	seats := a.seats
	a.seats = nil
	a.mu.Unlock()
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
		reply := h.served(raw)
		line, _ := json.Marshal(followLine{Reply: &reply})
		if !s.send(line) {
			return
		}
	}
}

func (h *Host) served(raw json.RawMessage) followReply {
	var ask followAsk
	if err := json.Unmarshal(raw, &ask); err != nil {
		return followReply{ID: ask.ID, Refused: err.Error()}
	}
	reply := followReply{ID: ask.ID, OK: true}
	switch ask.Op {
	case followSend:
		if _, running := h.Turn(); running {
			reply.Steered = h.steerFor(ask.Text)
			break
		}
		_, pick := h.Settings()
		h.emitter().emit(Event{Kind: EventTask, Text: ask.Text, Origin: Origin{Kind: OriginPerson}})
		reply.OK = h.Send(pick, ask.Text)
	case followStop:
		h.Stop()
	case followStopLead:
		h.StopLead()
	case followAnswer:
		reply.OK = h.Answer(ask.Asked, ask.Answer)
	case followAnswerAsk:
		reply.OK, reply.Stands = h.AnswerAsk(ask.Asked, ask.Answer)
	case followQuestion:
		reply.OK = h.AnswerQuestion(ask.Asked, ask.Question, ask.By)
	case followMemory:
		reply.OK = h.AnswerMemory(ask.Asked, ask.Scope)
	case followSteer:
		reply.Steered = h.steerFor(ask.Text)
	case followSendNow:
		reply.OK = h.SendNow(ask.Text)
	case followUnsteer:
		reply.OK = h.Unsteer(ask.Text)
	case followUnqueue:
		h.DropSteering()
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
	h.following.Store(f)
	h.readOnly = f.holder
	safe.Go(h.report, "follow", func() { h.heed(f, id) })
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

func (f *following) send(task string) bool {
	f.mu.Lock()
	f.typed = task
	f.mu.Unlock()
	reply := f.ask(followAsk{Op: followSend, Text: task})
	if reply.Refused != "" || reply.ID == 0 {
		f.mu.Lock()
		f.unsent = task
		f.mu.Unlock()
	}
	return reply.OK
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

func (h *Host) heed(f *following, id string) {
	lines := json.NewDecoder(f.conn)
	for {
		var raw json.RawMessage
		if lines.Decode(&raw) != nil {
			break
		}
		var line followLine
		_ = json.Unmarshal(raw, &line)
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
	h.takeOver(f, id)
}

func (h *Host) mirror(f *following, event Event) {
	if string(event.Args) == "null" {
		event.Args = nil
	}
	if event.Kind == EventTurnStarted || event.Kind == EventTurnEnded {
		h.mu.Lock()
		h.running = event.Kind == EventTurnStarted
		if h.running {
			h.turn = event.ID
		}
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

func (h *Host) takeOver(f *following, id string) {
	lost := time.Now()
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
			h.follow(next, id)
			h.mu.Unlock()
			h.note(next.note(id))
			return
		}
		h.mu.Unlock()
	}
	h.following.CompareAndSwap(f, nil)
	h.note("process " + strconv.Itoa(f.holder.PID) + " let go of session " + id + ", and it is still held after " + followTakeOverWait.String() + ", so this tofu reads it and writes nothing")
}

func (h *Host) tookOver(f *following, id string, took time.Duration, ran bool, reloaded error) {
	out := h.emitter()
	if ran {
		out.emit(Event{Kind: EventNote, Text: "the turn process " + strconv.Itoa(f.holder.PID) + " was running ended with it"})
		out.emit(Event{Kind: EventTurnEnded})
	}
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
