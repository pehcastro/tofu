package host

import (
	"bytes"
	"cmp"
	"encoding/json"
	"hash/fnv"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/session"
	"tofu/internal/status"
)

const (
	replayDir     = "replay"
	replayPattern = "lane-*"
)

type lane struct {
	host       *Host
	release    func()
	items      *items
	replay     *replay
	rebuilding sync.Mutex
	status     *statusFeed
	stop       chan struct{}
	pumped     chan struct{}
}

type replay struct {
	file  *os.File
	size  int64
	dead  int64
	lines []storedLine
}

type storedLine struct {
	method string
	at     int64
	length int64
	sum    uint64
}

type frozenLine struct {
	method string
	params []byte
}

type changedLine struct {
	index int
	line  frozenLine
}

func (s *server) replayOf(lines []outgoing) *replay {
	if err := os.MkdirAll(s.replays, 0o755); err != nil {
		return nil
	}
	file, err := os.CreateTemp(s.replays, replayPattern)
	if err != nil {
		return nil
	}
	r := &replay{file: file}
	for _, out := range lines {
		if r.add(out) != nil {
			r.drop()
			return nil
		}
	}
	return r
}

func frozen(out outgoing) (frozenLine, error) {
	var written bytes.Buffer
	encoder := json.NewEncoder(&written)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(out.msg.Params)
	return frozenLine{method: out.msg.Method, params: bytes.TrimSpace(written.Bytes())}, err
}

func (f frozenLine) sum() uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(f.method))
	_, _ = hash.Write(f.params)
	return hash.Sum64()
}

func (r *replay) add(out outgoing) error {
	line, err := frozen(out)
	if err != nil {
		return nil
	}
	return r.push(line)
}

func (r *replay) push(line frozenLine) error {
	stored, err := r.put(line)
	if err == nil {
		r.lines = append(r.lines, stored)
	}
	return err
}

func (r *replay) put(line frozenLine) (storedLine, error) {
	if _, err := r.file.WriteAt(line.params, r.size); err != nil {
		return storedLine{}, err
	}
	stored := storedLine{method: line.method, at: r.size, length: int64(len(line.params)), sum: line.sum()}
	r.size += stored.length
	return stored, nil
}

func (r *replay) read(stored storedLine) (frozenLine, error) {
	params := make([]byte, stored.length)
	_, err := r.file.ReadAt(params, stored.at)
	return frozenLine{method: stored.method, params: params}, err
}

func (r *replay) page(first, end int) ([]HistoryLine, error) {
	page := make([]HistoryLine, 0, end-first)
	for _, stored := range r.lines[first:end] {
		line, err := r.read(stored)
		if err != nil {
			return nil, err
		}
		page = append(page, HistoryLine{Method: line.method, Params: json.RawMessage(line.params)})
	}
	return page, nil
}

func (r *replay) merge(count int, changed []changedLine) error {
	for _, gone := range r.lines[min(count, len(r.lines)):] {
		r.dead += gone.length
	}
	r.lines = r.lines[:min(count, len(r.lines))]
	for _, change := range changed {
		stored, err := r.put(change.line)
		if err != nil {
			return err
		}
		if change.index < len(r.lines) {
			r.dead += r.lines[change.index].length
			r.lines[change.index] = stored
		} else {
			r.lines = append(r.lines, stored)
		}
	}
	return nil
}

func (s *server) rewritten(from *replay, known []storedLine, count int, changed []changedLine) *replay {
	built := s.replayOf(nil)
	for index := 0; built != nil && index < count; index++ {
		var line frozenLine
		var err error
		if len(changed) > 0 && changed[0].index == index {
			line, changed = changed[0].line, changed[1:]
		} else {
			line, err = from.read(known[index])
		}
		if err != nil || built.push(line) != nil {
			built.drop()
			return nil
		}
	}
	return built
}

func (r *replay) drop() {
	if r == nil {
		return
	}
	_ = r.file.Close()
	_ = os.Remove(r.file.Name())
}

func (l *lane) replace(with *replay) {
	l.replay.drop()
	l.replay = with
}

func newLane(h *Host, release func()) *lane {
	fresh := newItems(h.ID())
	return &lane{host: h, release: release, items: &fresh, status: &statusFeed{board: status.Board{Now: time.Now}, acked: map[string]bool{}}, stop: make(chan struct{}), pumped: make(chan struct{})}
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
	return &lane{host: s.Host, release: s.Release, items: &s.items, status: &s.status}
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
		s.mu.Lock()
		h.joinAs(memberOf(s.client))
		s.mu.Unlock()
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
			return nil, err
		}
		l.asks(p.Asking)
		empty := s.replayOf(nil)
		s.mu.Lock()
		*l.items = newItems(id)
		l.replace(empty)
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
		return nil, err
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
	replay := s.replayOf(lines)
	if live := l.host.seen(); live.Turn != nil {
		read.translate(*live.Turn, time.Now())
		read.translate(Event{Kind: EventSubAgent, SubAgents: live.Agents}, time.Now())
	}
	if was == carry.Session {
		maps.Copy(read.logged, l.items.logged)
	}
	*l.items = read
	l.replace(replay)
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
	replay := s.replayOf(lines)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.focusOn(l)
	if l.items.session != carry.Session {
		replay.drop()
	} else {
		l.replace(replay)
	}
	for _, line := range newest(lines, p.Replay) {
		s.box.push(line)
	}
	return SessionOpenResult{Session: carry.Session}, nil
}

func (s *server) rebuild(l *lane) {
	l.rebuilding.Lock()
	defer l.rebuilding.Unlock()
	s.mu.Lock()
	session, was := l.items.session, l.replay
	var known []storedLine
	var dead int64
	if was != nil {
		known, dead = slices.Clone(was.lines), was.dead
	}
	s.mu.Unlock()
	carry, err := s.Carry(session)
	if err != nil {
		return
	}
	var built *replay
	if was == nil {
		if built = s.replayOf(nil); built == nil {
			return
		}
	}
	read, count, live, failed := newItems(carry.Session), 0, int64(0), false
	var changed []changedLine
	for _, event := range l.host.Opening(carry) {
		for _, out := range read.translate(event, event.At) {
			line, err := frozen(out)
			switch {
			case err != nil:
				continue
			case built != nil:
				failed = failed || built.push(line) != nil
			case count >= len(known):
				changed = append(changed, changedLine{index: count, line: line})
			case known[count].sum != line.sum():
				changed, dead = append(changed, changedLine{index: count, line: line}), dead+known[count].length
			}
			count, live = count+1, live+int64(len(line.params))
		}
	}
	for _, gone := range known[min(count, len(known)):] {
		dead += gone.length
	}
	if built == nil && dead > live {
		built = s.rewritten(was, known, count, changed)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case failed || l.items.session != carry.Session || l.replay != was || was != nil && len(was.lines) != len(known):
		built.drop()
	case built != nil:
		l.replace(built)
	case was.merge(count, changed) != nil:
		l.replace(nil)
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
	l.replace(nil)
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
