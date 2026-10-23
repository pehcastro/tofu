package host

import (
	"cmp"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/session"
	"tofu/internal/status"
)

type lane struct {
	host    *Host
	release func()
	items   *items
	replay  []HistoryLine
	pending map[string]ApprovalRequest
	asked   map[string]QuestionRequest
	status  *statusFeed
	stop    chan struct{}
	pumped  chan struct{}
}

func newLane(h *Host, release func()) *lane {
	fresh := newItems(h.ID())
	return &lane{host: h, release: release, items: &fresh, pending: map[string]ApprovalRequest{}, asked: map[string]QuestionRequest{},
		status: &statusFeed{board: status.Board{Now: time.Now}, acked: map[string]bool{}}, stop: make(chan struct{}), pumped: make(chan struct{})}
}

func (l *lane) running() bool {
	_, running := l.host.Turn()
	return running
}

func (l *lane) asks(asking AskingMode) {
	if asking != "" {
		l.host.SetAsking(asking)
	}
}

func (s *server) first() *lane {
	return &lane{host: s.Host, release: s.Release, items: &s.items, pending: s.pending, asked: s.asked, status: &s.status}
}

func (s *server) tracked() []*lane {
	if lanes := s.lanes.Load(); lanes != nil {
		return *lanes
	}
	return []*lane{s.first()}
}

func (s *server) focus() *lane {
	lanes := s.tracked()
	return lanes[len(lanes)-1]
}

func (s *server) without(l *lane) []*lane {
	return slices.DeleteFunc(slices.Clone(s.tracked()), func(other *lane) bool { return other == l })
}

func (s *server) focusOn(l *lane) {
	was, lanes := s.focus(), append(s.without(l), l)
	s.lanes.Store(&lanes)
	s.reportStatus(was)
	s.reportStatus(l)
}

func (s *server) laneOf(h *Host) *lane {
	lanes := s.tracked()
	if at := slices.IndexFunc(lanes, func(l *lane) bool { return l.host == h }); at >= 0 {
		return lanes[at]
	}
	return s.focus()
}

func (s *server) holding(id string) *lane {
	if id == "" {
		return nil
	}
	lanes := s.tracked()
	if at := slices.IndexFunc(lanes, func(l *lane) bool { return l.host.ID() == id }); at >= 0 {
		return lanes[at]
	}
	store, err := session.OpenIn(s.Host.dir)
	if err != nil {
		return nil
	}
	for _, l := range lanes {
		if family, err := store.Identity(l.host.ID()); err == nil && family.Family == id {
			return l
		}
	}
	return nil
}

func (s *server) lane(id string) (*lane, error) {
	if id == "" {
		return s.focus(), nil
	}
	if l := s.holding(id); l != nil {
		return l, nil
	}
	return nil, &Refusal{Code: CodeRefused, Message: "session " + strconv.Quote(id) + " is not open here, and " + strconv.Quote(s.focus().host.ID()) + " is in focus: open it first"}
}

func (s *server) open(p SessionOpenParams) (any, error) {
	if p.Project != "" && filepath.Clean(p.Project) != filepath.Clean(s.Dir) {
		return nil, &Refusal{Code: CodeRefused, Message: "this tofu serves " + s.Dir + ", not " + p.Project}
	}
	if err := checkAsking(p.Asking); err != nil {
		return nil, err
	}
	focus, held := s.focus(), s.holding(p.Session)
	switch {
	case held != nil && held != focus:
		return s.refocus(held, p)
	case held == nil && s.Spawn != nil && focus.running():
		h, release := s.Spawn()
		asking, pick := focus.host.Settings()
		h.SetAsking(asking)
		h.Choose(pick)
		spawned := newLane(h, release)
		opened, err := s.openOn(spawned, p)
		if err != nil {
			release()
			return nil, err
		}
		s.mu.Lock()
		s.focusOn(spawned)
		s.mu.Unlock()
		s.pumpOn(spawned)
		return opened, nil
	}
	return s.openOn(focus, p)
}

func (s *server) openOn(l *lane, p SessionOpenParams) (any, error) {
	if p.Session == "" {
		id, err := l.host.OpenFresh()
		if err != nil {
			return nil, busy(err)
		}
		l.asks(p.Asking)
		s.mu.Lock()
		*l.items, l.replay = newItems(id), []HistoryLine{}
		s.mu.Unlock()
		return SessionOpenResult{Session: id, Fresh: true}, nil
	}
	carry, err := s.Carry(p.Session)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	was := l.items.session
	l.items.session = carry.Session
	s.mu.Unlock()
	chat, err := l.host.Resume(carry)
	if err != nil {
		s.mu.Lock()
		l.items.session = was
		s.mu.Unlock()
		return nil, busy(err)
	}
	defer l.asks(p.Asking)
	s.mu.Lock()
	defer s.mu.Unlock()
	if store, err := session.OpenIn(s.Host.dir); err == nil {
		if side, found, _ := store.Side(carry.Session); found && side.Model != "" {
			asking, pick := l.host.Settings()
			pick.Wire, pick.Model = cmp.Or(side.Wire, pick.Wire), side.Model
			l.host.Choose(pick)
			s.box.push(merged("session.settings", "", &SessionSettings{Identity: Identity{Session: carry.Session}, Asking: asking, Pick: s.shown(pick)}))
		}
	}
	read := newItems(carry.Session)
	lines := read.all(chat)
	replay := freeze(lines)
	if live := l.host.seen(); live.Turn != nil {
		read.translate(*live.Turn, time.Now())
		read.translate(Event{Kind: EventSubAgent, SubAgents: live.Agents}, time.Now())
	}
	if was == carry.Session {
		maps.Copy(read.logged, l.items.logged)
	}
	*l.items, l.replay = read, replay
	for _, line := range newest(lines, p.Replay) {
		s.box.push(line)
	}
	return SessionOpenResult{Session: carry.Session}, nil
}

func (s *server) refocus(l *lane, p SessionOpenParams) (any, error) {
	carry, err := s.Carry(p.Session)
	if err != nil {
		return nil, err
	}
	l.asks(p.Asking)
	read := newItems(carry.Session)
	lines := read.all(resumedChat(carry, s.Host.dir))
	replay := freeze(lines)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.focusOn(l)
	if l.items.session == carry.Session {
		l.replay = replay
	}
	for _, line := range newest(lines, p.Replay) {
		s.box.push(line)
	}
	return SessionOpenResult{Session: carry.Session}, nil
}

func (s *server) rebuild(l *lane) {
	s.mu.Lock()
	session, since := l.items.session, len(l.replay)
	s.mu.Unlock()
	carry, err := s.Carry(session)
	if err != nil {
		return
	}
	read := newItems(carry.Session)
	replay := freeze(read.all(l.host.Opening(carry)))
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.items.session == carry.Session && len(l.replay) == since {
		l.replay = replay
	}
}

func newest(lines []outgoing, replay *int) []outgoing {
	if replay == nil {
		return lines
	}
	return lines[len(lines)-min(max(*replay, 0), len(lines)):]
}

func (s *server) closeSession(p SessionCloseParams) (any, error) {
	l, err := s.lane(p.Session)
	if err != nil {
		return nil, err
	}
	named := strconv.Quote(l.host.ID())
	switch {
	case len(s.tracked()) == 1:
		return nil, &Refusal{Code: CodeRefused, Message: "session " + named + " is the only one open here: open another before closing it"}
	case l.running() && !p.Stop:
		return nil, &Refusal{Code: CodeRefused, Message: "a turn is running in session " + named + ": wait for it to end, or close it with stop"}
	case !s.stopTurns(StopAsked{By: s.asker("session.close with stop"), Why: "the session was closed", Everything: true}, l):
		return nil, &Refusal{Code: CodeRefused, Message: "the turn in session " + named + " did not stop in time, so the session stays open"}
	}
	l.halt()
	s.mu.Lock()
	lanes := s.without(l)
	s.lanes.Store(&lanes)
	s.sendStatus(l, l.status.board.Sync(nil))
	s.reportStatus(s.focus())
	s.mu.Unlock()
	if l.release != nil {
		l.release()
	}
	return Ack{OK: true}, nil
}

func (s *server) stopTurns(asked StopAsked, lanes ...*lane) bool {
	for _, l := range lanes {
		if l.running() {
			l.host.Stop(asked)
		}
	}
	gaveUp := time.Now().Add(konst.ServeStopMillis * time.Millisecond)
	for slices.ContainsFunc(lanes, (*lane).running) {
		if time.Now().After(gaveUp) {
			return false
		}
		time.Sleep(konst.ServeShellPollMillis * time.Millisecond)
	}
	return true
}

func (s *server) pumpOn(l *lane) {
	deliver := func(event Event) {
		event.from = l.host
		s.publish(event)
	}
	safe.Go(l.host.report, "serve pump", func() {
		defer close(l.pumped)
		safe.Loop(l.host.report, "serve pump", l.stop, func() {
			for {
				select {
				case event := <-l.host.Events():
					deliver(event)
				case raw := <-l.host.shellRaw:
					s.rawCame(l, raw)
				case <-l.host.cronMove:
					s.mu.Lock()
					id := l.items.identity("", "cron")
					s.mu.Unlock()
					s.box.push(merged("cron.updated", "", &CronUpdated{Identity: id, CronState: l.host.cronState()}))
				case <-l.stop:
					for events := l.host.Events(); len(events) > 0; {
						deliver(<-events)
					}
					return
				}
			}
		})
	})
}

func (l *lane) halt() {
	close(l.stop)
	<-l.pumped
}
