package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/sys"
)

const (
	HubVerb       = "host"
	HubRunVerb    = "run"
	hubNetwork    = "unix"
	hubSocketDir  = "h"
	hubHostsDir   = "hosts"
	hubShortKey   = 8
	hubAskStatus  = "status"
	hubAskJoin    = "join"
	hubAskRoster  = "roster"
	hubAskMembers = "members"
	hubAskStop    = "stop"
	hubAnswerWait = konst.HostAnswerMillis * time.Millisecond
	hubRetry      = konst.HostRetryMillis * time.Millisecond
)

var (
	ErrHubRunning = errors.New("a tofu host already serves this project")
	errNoHub      = errors.New("no tofu host serves this project")
	errNoSession  = errors.New("a member joins a session, and this join named none")
)

type HubSession func(id string, projectTurn *sync.Mutex) (*Host, func(), []string)

type HubStatus struct {
	PID     int       `json:"pid"`
	Socket  string    `json:"socket"`
	Project string    `json:"project"`
	Version string    `json:"version"`
	Members int64     `json:"members"`
	Started time.Time `json:"started"`
}

type hubPlace struct {
	project, socket, lock, log, registry string
}

type hubAsk struct {
	Ask     string `json:"ask"`
	Session string `json:"session,omitempty"`
	Member  Member `json:"member,omitzero"`
}

type hubJoined struct {
	HubStatus
	Session string `json:"session,omitempty"`
	Refused string `json:"refused,omitempty"`
}

type hub struct {
	status      HubStatus
	members     atomic.Int64
	changed     chan struct{}
	stopped     chan struct{}
	stop        sync.Once
	open        HubSession
	report      safe.Report
	say         func(string)
	projectTurn sync.Mutex
	mu          sync.Mutex
	sessions    []*hosted
}

type hosted struct {
	id      string
	host    *Host
	end     func()
	failed  error
	members []Member
	ending  bool
	ready   chan struct{}
	ended   chan struct{}
}

func (m Member) named() string {
	return "process " + strconv.Itoa(m.Process) + " (" + string(m.Kind) + ")"
}

func placeOf(project string) (hubPlace, error) {
	abs, err := filepath.Abs(project)
	if err != nil {
		return hubPlace{}, err
	}
	if final, err := filepath.EvalSymlinks(abs); err == nil {
		abs = final
	}
	home, err := sys.HomeConfigDir()
	if err != nil {
		return hubPlace{}, err
	}
	keyed := abs
	if sys.OS() == "windows" || sys.OS() == "darwin" {
		keyed = strings.ToLower(abs)
	}
	sum := sha256.Sum256([]byte(keyed))
	key := hex.EncodeToString(sum[:])
	short := filepath.Join(home, hubSocketDir, key[:hubShortKey])
	return hubPlace{project: abs, socket: short + ".sock", lock: short + ".lock", log: short + ".log", registry: filepath.Join(home, hubHostsDir, key+".json")}, nil
}

func askHubFor(place hubPlace, ask hubAsk, answer any) error {
	conn, err := net.DialTimeout(hubNetwork, place.socket, hubAnswerWait)
	if err != nil {
		return errNoHub
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(hubAnswerWait))
	if err := json.NewEncoder(conn).Encode(ask); err != nil {
		return err
	}
	return json.NewDecoder(conn).Decode(answer)
}

func askHub(place hubPlace, ask string) (HubStatus, error) {
	var status HubStatus
	return status, askHubFor(place, hubAsk{Ask: ask}, &status)
}

func HubStatusOf(project string) (HubStatus, error) {
	place, err := placeOf(project)
	if err != nil {
		return HubStatus{}, err
	}
	return askHub(place, hubAskStatus)
}

func StopHub(project string) (HubStatus, error) {
	place, err := placeOf(project)
	if err != nil {
		return HubStatus{}, err
	}
	stopped, err := askHub(place, hubAskStop)
	if err != nil {
		return stopped, err
	}
	for deadline := time.Now().Add(konst.HostReadyMillis * time.Millisecond); time.Now().Before(deadline); time.Sleep(hubRetry) {
		if _, err := os.Stat(place.registry); errors.Is(err, os.ErrNotExist) {
			return stopped, nil
		}
	}
	return stopped, fmt.Errorf("the tofu host %d was asked to stop and its registry %s is still there", stopped.PID, place.registry)
}

func EnsureHub(project string) (HubStatus, error) {
	place, err := placeOf(project)
	if err != nil {
		return HubStatus{}, err
	}
	if status, err := askHub(place, hubAskStatus); err == nil {
		return status, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return HubStatus{}, err
	}
	if err := os.MkdirAll(filepath.Dir(place.log), 0o700); err != nil {
		return HubStatus{}, err
	}
	log, err := os.OpenFile(place.log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return HubStatus{}, err
	}
	defer func() { _ = log.Close() }()
	if err := startDetached(func() *exec.Cmd {
		cmd := exec.Command(exe, HubVerb, HubRunVerb, "--dir", place.project)
		cmd.Dir, cmd.Stdout, cmd.Stderr = place.project, log, log
		return cmd
	}); err != nil {
		return HubStatus{}, err
	}
	ready := konst.HostReadyMillis * time.Millisecond
	for deadline := time.Now().Add(ready); time.Now().Before(deadline); time.Sleep(hubRetry) {
		if status, err := askHub(place, hubAskStatus); err == nil {
			return status, nil
		}
	}
	return HubStatus{}, fmt.Errorf("the tofu host did not answer on %s within %s, its log is %s", place.socket, ready, place.log)
}

func joinHub(project, id string, kind MemberKind) (net.Conn, *json.Decoder, hubJoined, error) {
	status, err := EnsureHub(project)
	if err != nil {
		return nil, nil, hubJoined{HubStatus: status}, err
	}
	conn, err := net.DialTimeout(hubNetwork, status.Socket, hubAnswerWait)
	if err != nil {
		return nil, nil, hubJoined{HubStatus: status}, err
	}
	_ = conn.SetDeadline(time.Now().Add(konst.HostJoinMillis * time.Millisecond))
	lines := json.NewDecoder(conn)
	var joined hubJoined
	err = json.NewEncoder(conn).Encode(hubAsk{Ask: hubAskJoin, Session: id, Member: Member{Process: os.Getpid(), Kind: kind}})
	if err == nil {
		err = lines.Decode(&joined)
	}
	if err == nil && joined.Refused != "" {
		err = errors.New(joined.Refused)
	}
	if err != nil {
		_ = conn.Close()
		return nil, nil, joined, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, lines, joined, nil
}

func MembersOf(project string) map[string][]Member {
	place, err := placeOf(project)
	if err != nil {
		return nil
	}
	var members map[string][]Member
	if askHubFor(place, hubAsk{Ask: hubAskMembers}, &members) != nil {
		return nil
	}
	return members
}

func takeHubLock(place hubPlace) (*os.File, error) {
	for deadline := time.Now().Add(konst.HostReadyMillis * time.Millisecond); ; time.Sleep(hubRetry) {
		if file, held, err := lockHub(place.lock); err != nil || !held {
			return file, err
		}
		if _, err := askHub(place, hubAskStatus); err == nil {
			return nil, ErrHubRunning
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the host lock %s stays held and nothing answers on %s", place.lock, place.socket)
		}
	}
}

func RunHub(project string, report safe.Report, say func(string), open HubSession) error {
	place, err := placeOf(project)
	if err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(place.socket), filepath.Dir(place.registry)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := os.Chdir(place.project); err != nil {
		return err
	}
	lock, err := takeHubLock(place)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	_ = os.Remove(place.socket)
	listener, err := net.Listen(hubNetwork, place.socket)
	if err != nil {
		return fmt.Errorf("the host socket %s does not open: %w", place.socket, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(place.socket)
		_ = os.Remove(place.registry)
	}()
	h := &hub{
		status:  HubStatus{PID: os.Getpid(), Socket: place.socket, Project: place.project, Version: konst.Version, Started: time.Now()},
		changed: make(chan struct{}, 1),
		stopped: make(chan struct{}),
		open:    open,
		report:  report,
		say:     say,
	}
	if err := writeRegistry(place.registry, h.status); err != nil {
		return fmt.Errorf("the host registry %s: %w", place.registry, err)
	}
	safe.Go(report, "host accept", func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			safe.Go(report, "host member", func() { h.serve(conn) })
		}
	})
	say("process " + strconv.Itoa(h.status.PID) + " serves " + place.project + " on " + place.socket)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	say(h.wait(ctx, konst.HostLingerMillis*time.Millisecond))
	_ = listener.Close()
	h.endAll()
	return nil
}

func writeRegistry(path string, status HubStatus) error {
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	staged := path + "." + strconv.Itoa(status.PID)
	if err := os.WriteFile(staged, data, 0o600); err != nil {
		return err
	}
	return os.Rename(staged, path)
}

func (h *hub) wait(ctx context.Context, linger time.Duration) string {
	for {
		var alone <-chan time.Time
		if h.members.Load() == 0 {
			alone = time.After(linger)
		}
		select {
		case <-h.changed:
		case <-alone:
			if h.members.Load() == 0 {
				return "no member for " + linger.String() + ", so the host exits"
			}
		case <-h.stopped:
			return "asked to stop, so the host exits"
		case <-ctx.Done():
			return "interrupted, so the host exits"
		}
	}
}

func (h *hub) moved(by int64) {
	h.members.Add(by)
	select {
	case h.changed <- struct{}{}:
	default:
	}
}

func (h *hub) answer(conn net.Conn) error {
	status := h.status
	status.Members = h.members.Load()
	return json.NewEncoder(conn).Encode(status)
}

func (h *hub) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(hubAnswerWait))
	lines := json.NewDecoder(conn)
	var asked hubAsk
	if lines.Decode(&asked) != nil {
		return
	}
	switch asked.Ask {
	case hubAskStatus:
		_ = h.answer(conn)
	case hubAskStop:
		_ = h.answer(conn)
		h.stop.Do(func() { close(h.stopped) })
	case hubAskRoster:
		_ = json.NewEncoder(conn).Encode(h.roster(asked.Session))
	case hubAskMembers:
		_ = json.NewEncoder(conn).Encode(h.membersNow())
	case hubAskJoin:
		h.moved(1)
		defer h.moved(-1)
		h.join(conn, lines, asked)
	}
}

func (h *hub) join(conn net.Conn, lines *json.Decoder, asked hubAsk) {
	_ = conn.SetDeadline(time.Time{})
	joined := hubJoined{HubStatus: h.status}
	member := asked.Member
	member.Joined = time.Now()
	s, err := h.take(asked.Session, member)
	if err != nil {
		joined.Refused = err.Error()
		_ = json.NewEncoder(conn).Encode(joined)
		return
	}
	joined.Members, joined.Session = h.members.Load(), s.id
	h.told(s)
	if json.NewEncoder(conn).Encode(joined) == nil {
		s.host.seat(conn, io.MultiReader(lines.Buffered(), conn))
	}
	h.leave(s, member)
}

func (h *hub) told(s *hosted) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s.host.membersAre(slices.Clone(s.members))
}

func (h *hub) take(id string, member Member) (*hosted, error) {
	if id == "" {
		return nil, errNoSession
	}
	for {
		h.mu.Lock()
		at := slices.IndexFunc(h.sessions, func(s *hosted) bool { return s.id == id || s.host != nil && s.host.ID() == id })
		if at < 0 {
			s := &hosted{id: id, members: []Member{member}, ready: make(chan struct{}), ended: make(chan struct{})}
			h.sessions = append(h.sessions, s)
			h.mu.Unlock()
			return s, h.opened(s)
		}
		s := h.sessions[at]
		if !s.ending {
			s.members = append(s.members, member)
		}
		h.mu.Unlock()
		if s.ending {
			<-s.ended
			continue
		}
		<-s.ready
		return s, s.failed
	}
}

func (h *hub) opened(s *hosted) error {
	defer close(s.ready)
	live, end, troubles := h.open(s.id, &h.projectTurn)
	for _, trouble := range troubles {
		h.say("session " + s.id + ": " + trouble)
	}
	if live == nil || !live.holds(s.id) {
		if end != nil {
			end()
		}
		s.failed = fmt.Errorf("the tofu host, process %d, did not open session %s: %s", h.status.PID, s.id, strings.Join(troubles, "; "))
		h.forget(s)
		return s.failed
	}
	safe.Go(h.report, "host session events", func() {
		for {
			select {
			case event := <-live.events:
				if event.Kind == EventNote || event.Kind == EventFailure {
					h.say("session " + s.id + ": " + event.Text)
				}
			case <-live.done:
				return
			}
		}
	})
	h.mu.Lock()
	s.host, s.end = live, end
	h.mu.Unlock()
	h.say("opened session " + s.id)
	return nil
}

func (h *hub) forget(s *hosted) {
	h.mu.Lock()
	h.sessions = slices.DeleteFunc(h.sessions, func(other *hosted) bool { return other == s })
	h.mu.Unlock()
	close(s.ended)
}

func (h *hub) leave(s *hosted, member Member) {
	h.mu.Lock()
	if at := slices.Index(s.members, member); at >= 0 {
		s.members = slices.Delete(s.members, at, at+1)
	}
	last := len(s.members) == 0 && !s.ending
	s.ending = s.ending || last
	h.mu.Unlock()
	if !last {
		h.told(s)
		return
	}
	h.end(s, "the last member, "+member.named(), "it left, so the session's turn stops and its shells end")
}

func (h *hub) end(s *hosted, by, why string) {
	s.host.endedBy(by, why)
	s.end()
	h.say("closed session " + s.host.ID() + ", " + by + ": " + why)
	h.forget(s)
}

func (h *hub) endAll() {
	h.mu.Lock()
	open := slices.Clone(h.sessions)
	h.mu.Unlock()
	for _, s := range open {
		<-s.ready
		h.mu.Lock()
		ends := !s.ending && s.failed == nil
		s.ending = true
		h.mu.Unlock()
		if ends {
			h.end(s, "the tofu host, process "+strconv.Itoa(h.status.PID), "it exits, so the session's turn stops and its shells end")
		}
		<-s.ended
	}
}

func (h *hub) roster(id string) []SubAgentRow {
	h.mu.Lock()
	defer h.mu.Unlock()
	at := slices.IndexFunc(h.sessions, func(s *hosted) bool { return s.host != nil && s.host.ID() == id })
	if at < 0 {
		return nil
	}
	return h.sessions[at].host.rosterRows()
}

func (h *hub) membersNow() map[string][]Member {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := map[string][]Member{}
	for _, s := range h.sessions {
		if s.host != nil {
			now[s.host.ID()] = slices.Clone(s.members)
		}
	}
	return now
}
