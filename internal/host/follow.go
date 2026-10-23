package host

import (
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/cron"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/memory"
	"tofu/internal/safe"
	"tofu/internal/session"
	"tofu/internal/shell"
)

const (
	followReplyWait = konst.FollowReplyMillis * time.Millisecond
	deskClient      = "tofu-desk"
)

var errHostSilent = errors.New("the tofu host did not answer, so nothing changed")

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
	followTakeOver  followOp = "takeover"
	followInput     followOp = "input"
	followResize    followOp = "resize"
	followRelease   followOp = "release"
	followTrace     followOp = "trace"
)

type followAsk struct {
	ID        int
	Op        followOp
	Text      string
	Asked     string
	Answer    Answer
	Question  QuestionAnswer
	Scope     memory.Scope `json:",omitempty"`
	By        string
	Pick      Pick
	Asking    AskingMode     `json:",omitempty"`
	Images    []pendingImage `json:",omitempty"`
	Command   string         `json:",omitempty"`
	Stopped   bool           `json:",omitempty"`
	Stop      StopAsked      `json:",omitzero"`
	Until     time.Time      `json:",omitzero"`
	Shell     string         `json:",omitempty"`
	Cols      int            `json:",omitempty"`
	Rows      int            `json:",omitempty"`
	Data      []byte         `json:",omitempty"`
	Terminals bool           `json:",omitempty"`
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
	Hold     shell.Hold       `json:",omitzero"`
	Shell    shell.Refused    `json:",omitempty"`
}

type followLine struct {
	Seq      uint64        `json:",omitempty"`
	Snapshot *picture      `json:",omitempty"`
	Event    *Event        `json:",omitempty"`
	Reply    *followReply  `json:",omitempty"`
	Settings *heldSettings `json:",omitempty"`
	Raw      *ShellRaw     `json:",omitempty"`
}

type heldSettings struct {
	Asking AskingMode
	Pick   Pick
}

type audience struct {
	mu    sync.Mutex
	seats map[*seat]struct{}
}

type link struct {
	conn net.Conn
	gone chan struct{}
	once sync.Once
}

type seat struct {
	link
	stream    chan []byte
	kept      chan []byte
	terminals bool
}

type following struct {
	link
	lines   *json.Decoder
	hub     hubJoined
	mu      sync.Mutex
	asked   int
	waiting map[int]chan followReply
	typed   string
	unsent  string
	chose   Pick
	seq     uint64
	joined  chan struct{}
}

func linked(conn net.Conn) link { return link{conn: conn, gone: make(chan struct{})} }

func (l *link) leave() {
	l.once.Do(func() {
		close(l.gone)
		_ = l.conn.Close()
	})
}

func (h *Host) openAudience(a *audience) {
	if a == nil {
		a = &audience{seats: map[*seat]struct{}{}}
	}
	h.bus.mu.Lock()
	h.bus.now.Settings = heldSettings{Asking: h.asking, Pick: h.pick}
	h.bus.mu.Unlock()
	h.audience.Store(a)
}

func (h *Host) seat(conn net.Conn, in io.Reader) {
	a := h.audience.Load()
	if a == nil {
		return
	}
	s := &seat{link: linked(conn), stream: make(chan []byte, konst.FollowQueue), kept: make(chan []byte, konst.FollowQueue)}
	h.bus.mu.Lock()
	a.mu.Lock()
	seated := a.seats != nil
	if seated {
		s.stream <- h.snapshot()
		a.seats[s] = struct{}{}
	}
	a.mu.Unlock()
	h.bus.mu.Unlock()
	if !seated {
		return
	}
	safe.Go(h.report, "follow write", s.write)
	h.serveSeat(a, s, in)
}

func (a *audience) broadcast(said followLine, snapshot func() []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.seats) == 0 {
		return
	}
	line, err := json.Marshal(said)
	if err != nil {
		return
	}
	var resynced []byte
	for s := range a.seats {
		select {
		case s.stream <- line:
		default:
			if resynced == nil {
				resynced = snapshot()
			}
			s.resync(resynced)
		}
	}
}

func (s *seat) resync(snapshot []byte) {
	for emptied := false; !emptied; {
		select {
		case <-s.stream:
		default:
			emptied = true
		}
	}
	s.stream <- snapshot
}

func (a *audience) attach(s *seat, terminals bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s.terminals = terminals
}

func (a *audience) wantsTerminals() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for s := range a.seats {
		if s.terminals {
			return true
		}
	}
	return false
}

func (a *audience) drop(s *seat) {
	a.mu.Lock()
	delete(a.seats, s)
	a.mu.Unlock()
	s.leave()
}

func (a *audience) close() {
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
	case s.kept <- line:
		return true
	default:
		return false
	}
}

func (s *seat) write() {
	for {
		var line []byte
		select {
		case line = <-s.kept:
		case line = <-s.stream:
		case <-s.gone:
			return
		}
		if _, err := s.conn.Write(line); err != nil {
			s.leave()
			return
		}
	}
}

func (h *Host) serveSeat(a *audience, s *seat, in io.Reader) {
	defer func() {
		a.drop(s)
		h.useTerminals(a)
		h.handBackHeld(s)
	}()
	asks := json.NewDecoder(in)
	for {
		var raw json.RawMessage
		if err := asks.Decode(&raw); err != nil {
			line, _ := json.Marshal(followLine{Reply: &followReply{Refused: err.Error()}})
			_, _ = s.conn.Write(line)
			return
		}
		reply := h.served(a, s, raw)
		line, _ := json.Marshal(followLine{Reply: &reply})
		select {
		case s.kept <- line:
		case <-s.gone:
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
		a.attach(s, ask.Terminals)
		h.useTerminals(a)
	case followTakeOver, followInput, followResize, followRelease:
		reply = h.servedShell(s, ask)
	case followSettings:
		h.Settle(ask.Asking, ask.Pick)
	case followTrace:
		h.trace(ask.Text, nil)
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
		reply.Agents = h.rosterRows()
	default:
		reply.OK = false
	}
	return reply
}

func (h *Host) rosterRows() []SubAgentRow {
	h.mu.Lock()
	held := h.roster
	h.mu.Unlock()
	return SubAgentRows(held.SubAgents(), time.Now(), 0, nil, nil)
}

func (h *Host) steerFor(text string) string {
	id := h.Steer(text)
	h.emitter().emit(Event{Kind: EventSteerQueued, ID: id, Text: text})
	return id
}

func memberOf(client string) MemberKind {
	if strings.HasPrefix(client, deskClient) {
		return MemberDesk
	}
	return MemberServe
}

func (h *Host) joinAs(kind MemberKind) {
	h.mu.Lock()
	h.member = kind
	h.mu.Unlock()
}

func (h *Host) membersAre(members []Member) {
	h.emitter().emit(Event{Kind: EventMembers, Members: members})
}

func (h *Host) join(id string) (*following, error) {
	h.mu.Lock()
	kind := h.member
	h.mu.Unlock()
	conn, lines, hub, err := joinHub(h.dir, id, kind)
	if err != nil {
		return nil, err
	}
	return &following{link: linked(conn), lines: lines, hub: hub, waiting: map[int]chan followReply{}, joined: make(chan struct{})}, nil
}

func (h *Host) follow(f *following, id string) {
	settled := followAsk{Op: followSettings, Asking: h.asking, Pick: h.pick}
	h.following.Store(f)
	safe.Go(h.report, "follow", func() { h.heed(f) })
	_ = h.loadCron(id)
	safe.Go(h.report, "follow attach", func() {
		f.ask(followAsk{Op: followAttach, Terminals: h.shells != nil && h.shells.Terminals.Load()})
		f.ask(settled)
	})
}

func heldRoster(store *session.Store, id string) []SubAgentRow {
	place, err := placeOf(cmp.Or(store.Project(), "."))
	if err != nil {
		return nil
	}
	var rows []SubAgentRow
	if askHubFor(place, hubAsk{Ask: hubAskRoster, Session: id}, &rows) != nil {
		return nil
	}
	return rows
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
	for {
		var raw json.RawMessage
		if f.lines.Decode(&raw) != nil {
			break
		}
		var line followLine
		_ = json.Unmarshal(raw, &line)
		if held := line.Settings; held != nil {
			h.mu.Lock()
			h.asking, h.pick = held.Asking, held.Pick
			h.mu.Unlock()
		}
		if now := line.Snapshot; now != nil {
			h.rejoin(f, *now, line.Seq)
		}
		if line.Reply != nil && line.Reply.Refused != "" {
			h.note("the tofu host, process " + strconv.Itoa(f.hub.PID) + ", could not read what this tofu sent, so it did not run: " + line.Reply.Refused)
		}
		if line.Reply != nil {
			f.mu.Lock()
			waiting := f.waiting[line.Reply.ID]
			f.mu.Unlock()
			if waiting != nil {
				waiting <- *line.Reply
			}
		}
		if line.Raw != nil {
			select {
			case h.shellRaw <- *line.Raw:
			case <-f.gone:
			}
		}
		if event := line.Event; event != nil {
			f.seq = line.Seq
			if !f.echoes(*event) {
				h.mirror(f, *event)
			}
		}
	}
	f.leave()
	h.lost(f)
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
	h.bus.mu.Lock()
	told := h.bus.now.apply(event)
	h.bus.mu.Unlock()
	h.hand(told, f.gone)
}

func (h *Host) rejoin(f *following, now picture, seq uint64) {
	h.mu.Lock()
	h.asking, h.pick = now.Settings.Asking, now.Settings.Pick
	id := h.id
	h.mu.Unlock()
	h.bus.mu.Lock()
	if now.Session != nil && now.Session.ID == id && h.bus.now.Session == nil {
		h.bus.now.Session = now.Session
	}
	moves := h.bus.now.toward(now)
	h.bus.mu.Unlock()
	select {
	case <-f.joined:
		said := "process " + strconv.Itoa(os.Getpid()) + " fell behind the tofu host, process " + strconv.Itoa(f.hub.PID) + ", on session " + id + " after event " + strconv.FormatUint(f.seq, 10) + ", so it read the session again as it stood at event " + strconv.FormatUint(seq, 10)
		safe.Go(h.report, "follow trace", func() { f.ask(followAsk{Op: followTrace, Text: said}) })
	default:
		close(f.joined)
	}
	f.seq = seq
	for _, event := range moves {
		h.mirror(f, event)
	}
	h.bus.mu.Lock()
	h.bus.now = now
	h.bus.mu.Unlock()
}

func (f *following) caughtUp() {
	select {
	case <-f.joined:
	case <-f.gone:
	case <-time.After(followReplyWait):
	}
}

func (h *Host) lost(f *following) {
	if !h.following.CompareAndSwap(f, nil) {
		return
	}
	h.mu.Lock()
	ran, id := h.running, h.id
	h.running = false
	h.mu.Unlock()
	out, pid := h.emitter(), strconv.Itoa(f.hub.PID)
	if ran {
		out.emit(Event{Kind: EventNote, Text: "the turn the tofu host, process " + pid + ", was running ended with it"})
		out.emit(Event{Kind: EventTurnEnded})
	}
	out.emit(Event{Kind: EventNote, Text: "the tofu host, process " + pid + ", went away from session " + id + ", so the next message starts it again"})
}

func (h *Host) joined() (hubJoined, bool) {
	if f := h.following.Load(); f != nil {
		return f.hub, true
	}
	return hubJoined{}, false
}
