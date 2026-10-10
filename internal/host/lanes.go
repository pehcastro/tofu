package host

import (
	"cmp"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"tofu/internal/konst"
	"tofu/internal/session"
	"tofu/internal/status"
)

type lane struct {
	host    *Host
	release func()
	items   *items
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
	if p.Asking != "" {
		l.host.SetAsking(p.Asking)
	}
	if p.Session == "" {
		id, err := l.host.OpenFresh()
		if err != nil {
			return nil, busy(err)
		}
		s.mu.Lock()
		l.items.session = id
		s.mu.Unlock()
		return SessionOpenResult{Session: id, Fresh: true}, nil
	}
	carry, err := s.Carry(p.Session)
	if err != nil {
		return nil, err
	}
	chat, err := l.host.Resume(carry)
	if err != nil {
		return nil, busy(err)
	}
	if store, err := session.OpenIn(s.Host.dir); err == nil {
		if side, found, _ := store.Side(carry.Session); found && side.Model != "" {
			asking, pick := l.host.Settings()
			pick.Wire, pick.Model = cmp.Or(side.Wire, pick.Wire), side.Model
			l.host.Choose(pick)
			s.box.push(merged("session.settings", "", &SessionSettings{Identity: Identity{Session: carry.Session}, Asking: asking, Pick: s.shown(pick)}))
		}
	}
	read := newItems(carry.Session)
	lines := newest(read.all(chat), p.Replay)
	s.mu.Lock()
	defer s.mu.Unlock()
	maps.Copy(read.logged, l.items.logged)
	*l.items = read
	for _, line := range lines {
		s.box.push(line)
	}
	return SessionOpenResult{Session: carry.Session}, nil
}

func (s *server) refocus(l *lane, p SessionOpenParams) (any, error) {
	carry, err := s.Carry(p.Session)
	if err != nil {
		return nil, err
	}
	if p.Asking != "" {
		l.host.SetAsking(p.Asking)
	}
	read := newItems(carry.Session)
	lines := newest(read.all(resumedChat(carry, s.Host.dir)), p.Replay)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.focusOn(l)
	for _, line := range lines {
		s.box.push(line)
	}
	return SessionOpenResult{Session: carry.Session}, nil
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
	case !s.stopTurns(l):
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

func (s *server) stopTurns(lanes ...*lane) bool {
	for _, l := range lanes {
		if l.running() {
			l.host.Stop()
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
	go func() {
		defer close(l.pumped)
		for {
			select {
			case event := <-l.host.Events():
				deliver(event)
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
	}()
}

func (l *lane) halt() {
	close(l.stop)
	<-l.pumped
}
