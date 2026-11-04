package driver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/platform/recover"
	"tofu/internal/store/settings"
	"tofu/internal/sys"
)

const idleAfter = konst.BrowserIdleAfterMillis * time.Millisecond

type session struct {
	mu   sync.Mutex
	conn net.Conn
	out  *json.Encoder
}

func (s *session) send(answer result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.conn.SetWriteDeadline(time.Now().Add(konst.BrowserDialTimeoutMillis * time.Millisecond))
	if err == nil {
		err = s.out.Encode(answer)
	}
	if err != nil {
		_ = s.conn.Close()
	}
}

type route struct {
	session *session
	id      int64
	sent    time.Time
	frames  []json.RawMessage
}

type relay struct {
	mu        sync.Mutex
	extension io.Writer
	tabs      map[int]Tab
	claims    map[int]*session
	pending   map[int64]route
	sessions  map[*session]bool
	lastID    int64
	closed    bool
	shown     status
	idle      *time.Timer
	ready     chan struct{}
	sleeper   *time.Timer

	home         string
	extensionDir string
	builds       Builds
	restart      chan struct{}
	draining     bool
	manifestTofu string
	installed    func(exe string) (string, error)
	sleepAfter   func() time.Duration
	cursor       bool
	report       recover.Report
}

func Host(origin string, stdin io.Reader, stdout io.Writer, home string) error {
	tofu, err := Build()
	if err != nil {
		return err
	}
	reapLeftovers(home)
	return host(origin, stdin, stdout, home, tofu, installedBuild, func() time.Duration { return sleepAfter(home) })
}

func sleepAfter(home string) time.Duration {
	minutes := 0
	if store, err := settings.Open(filepath.Join(sys.StateDir(home), settings.FileName), ""); err == nil {
		minutes = store.Int(settings.BrowserSleepMinutes)
	}
	if minutes < 1 {
		minutes = konst.BrowserSleepMinutesDefault
	}
	return time.Duration(minutes) * time.Minute
}

func installedBuild(exe string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), konst.BrowserDialTimeoutMillis*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "browser", "build", "--json").Output()
	var envelope struct {
		Data struct {
			Build string `json:"build"`
		} `json:"data"`
	}
	if err == nil {
		err = json.Unmarshal(out, &envelope)
	}
	if err == nil && envelope.Data.Build == "" {
		err = fmt.Errorf("%s browser build printed no build", exe)
	}
	return envelope.Data.Build, err
}

func cursorOn(home string) bool {
	store, err := settings.Open(filepath.Join(sys.StateDir(home), settings.FileName), "")
	if err != nil {
		return settings.DeclaredDefault(settings.BrowserCursor) != 0
	}
	return store.Bool(settings.BrowserCursor)
}

func host(origin string, stdin io.Reader, stdout io.Writer, home, tofu string, installed func(exe string) (string, error), sleepAfter func() time.Duration) error {
	extensionDir, manifestPath := installPaths(home)
	var installedHost hostManifest
	raw, err := os.ReadFile(manifestPath)
	if err == nil {
		err = json.Unmarshal(raw, &installedHost)
	}
	if err != nil || !slices.Contains(installedHost.AllowedOrigins, origin) {
		return fmt.Errorf("%s is not the extension installed in %s: run tofu browser install", origin, manifestPath)
	}

	path, err := socketPath(home)
	if err != nil {
		return err
	}
	if conn, err := net.DialTimeout("unix", path, konst.BrowserDialTimeoutMillis*time.Millisecond); err == nil {
		_ = conn.Close()
		return fmt.Errorf("another tofu browser host already serves %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	r := &relay{extension: stdout, tabs: map[int]Tab{}, claims: map[int]*session{}, pending: map[int64]route{}, sessions: map[*session]bool{}, shown: statusIdle, home: home, extensionDir: extensionDir, builds: Builds{Tofu: tofu}, restart: make(chan struct{}, 1),
		manifestTofu: installedHost.Path, installed: installed, sleepAfter: sleepAfter, cursor: cursorOn(home), report: func(p recover.Panic) { p.Saved(sys.StateDir(home)) }}
	r.idle = time.AfterFunc(idleAfter, func() {
		_ = recover.Call(r.report, "browser idle", func() error {
			r.mu.Lock()
			defer r.mu.Unlock()
			if !r.closed && len(r.pending) == 0 {
				r.show(statusIdle)
			}
			return nil
		})
	})
	recover.Go(r.report, "browser accept", func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			recover.Go(r.report, "browser session", func() { r.serve(conn) })
		}
	})

	debug.FreeOSMemory()
	read := make(chan error, 1)
	recover.Go(r.report, "browser extension reader", func() {
		read <- recover.Call(r.report, "browser extension reader", func() error { return r.readExtension(stdin) })
	})
	select {
	case err = <-read:
	case <-r.restart:
		err = nil
	}
	_ = listener.Close()
	r.mu.Lock()
	r.closed = true
	for s := range r.sessions {
		_ = s.conn.Close()
	}
	r.mu.Unlock()
	return err
}

func (r *relay) installedTofu() string {
	if source, err := os.ReadFile(sourceRecord(r.home)); err == nil {
		return string(source)
	}
	return r.manifestTofu
}

func (r *relay) drainFor(current string) error {
	if _, err := Refresh(r.home); err != nil {
		return fmt.Errorf("the installed tofu is build %s and the copy Chrome starts could not take it, so this relay on %s keeps running: %v: run tofu browser install", current, r.builds.Tofu, err)
	}
	r.draining = true
	r.settle()
	return nil
}

func (r *relay) awaitTabs() error {
	r.mu.Lock()
	if r.ready == nil {
		r.ready = make(chan struct{})
		_ = r.tell(toExtension{T: messageWake})
	}
	ready := r.ready
	r.mu.Unlock()
	select {
	case <-ready:
		return nil
	case <-time.After(konst.BrowserWakeMillis * time.Millisecond):
		return fmt.Errorf("the extension did not send its tabs within %d ms of the wake: reload the tofu card in chrome://extensions", konst.BrowserWakeMillis)
	}
}

func (r *relay) sleepSoon() {
	switch {
	case r.ready == nil:
	case r.sleeper == nil:
		r.sleeper = time.AfterFunc(r.sleepAfter(), r.sleep)
	default:
		r.sleeper.Reset(r.sleepAfter())
	}
}

func (r *relay) sleep() {
	_ = recover.Call(r.report, "browser sleep", func() error {
		r.mu.Lock()
		stays := r.closed || r.ready == nil || len(r.sessions) > 0
		if !stays {
			r.ready, r.tabs = nil, map[int]Tab{}
			_ = r.tell(toExtension{T: messageSleep})
		}
		r.mu.Unlock()
		if !stays {
			debug.FreeOSMemory()
		}
		return nil
	})
}

func (r *relay) readExtension(stdin io.Reader) error {
	for {
		raw, err := ReadMessage(stdin)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		message, err := parseExtensionMessage(raw)
		if err != nil {
			return err
		}
		if to, answer := r.receive(message); to != nil {
			to.send(answer)
		}
	}
}

func (r *relay) receive(message extensionMessage) (*session, result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch message.T {
	case messageHello:
		r.adopt(message.Build)
		if r.ready == nil {
			_ = r.tell(toExtension{T: messageSleep})
			break
		}
		r.tabs = map[int]Tab{}
		for _, tab := range message.Tabs {
			r.tabs[tab.ID] = tab
		}
		maps.DeleteFunc(r.claims, func(tab int, _ *session) bool {
			_, open := r.tabs[tab]
			return !open
		})
		select {
		case <-r.ready:
		default:
			close(r.ready)
		}
	case messageTabRemoved:
		delete(r.tabs, message.TabID)
		delete(r.claims, message.TabID)
	case messageTabUpdated:
		if r.ready != nil {
			r.tabs[message.Tab.ID] = message.Tab
		}
	case messageFrames:
		if to, asked := r.pending[message.ID]; asked {
			to.frames = append(to.frames, message.Frames...)
			r.pending[message.ID] = to
		}
	case messageResult:
		to, asked := r.pending[message.ID]
		if !asked {
			return nil, result{}
		}
		delete(r.pending, message.ID)
		if to.frames != nil && message.OK {
			var tail []json.RawMessage
			if err := json.Unmarshal(message.Value, &tail); err != nil {
				message.OK, message.Error = false, fmt.Sprintf("the extension ended a screencast with %s, not frames: %v", message.Value, err)
			}
			message.Value, _ = json.Marshal(append(to.frames, tail...))
		}
		message.ID, message.Host = to.id, time.Since(to.sent)
		r.settle()
		return to.session, message.result
	}
	return nil, result{}
}

func (r *relay) adopt(running string) {
	r.builds.Extension = running
	updatedFrom := filepath.Join(filepath.Dir(r.extensionDir), "updated-from")
	switch running {
	case "":
		return
	case r.builds.Tofu:
		if stale, err := os.ReadFile(updatedFrom); err == nil {
			r.builds.UpdatedFrom = string(stale)
			_ = os.Remove(updatedFrom)
		}
		return
	}
	err := writeExtension(r.extensionDir)
	if err == nil {
		err = sys.WriteFile(updatedFrom, []byte(running), 0o644)
	}
	if err != nil {
		r.builds.Problem = fmt.Sprintf("the extension in %s runs build %s and this tofu ships %s, and tofu could not rewrite it: %v: run tofu browser install, then reload the tofu card", r.extensionDir, running, r.builds.Tofu, err)
		return
	}
	r.builds.Problem = fmt.Sprintf("the extension is reloading from build %s to %s: try again in a second", running, r.builds.Tofu)
	_ = r.tell(toExtension{T: messageReload})
}

func (r *relay) settle() {
	if len(r.pending) > 0 {
		return
	}
	r.idle.Reset(idleAfter)
	if r.draining {
		select {
		case r.restart <- struct{}{}:
		default:
		}
	}
}

func (r *relay) show(now status) {
	if now != r.shown {
		r.shown = now
		_ = r.tell(toExtension{T: messageStatus, State: now})
	}
}

func (r *relay) tell(message toExtension) error {
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return WriteMessage(r.extension, raw)
}

func (r *relay) serve(conn net.Conn) {
	s := &session{conn: conn, out: json.NewEncoder(conn)}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = conn.Close()
		return
	}
	r.sessions[s] = true
	r.mu.Unlock()

	requests := json.NewDecoder(conn)
	for {
		var req request
		if requests.Decode(&req) != nil {
			break
		}
		if err := recover.Call(r.report, "browser request", func() error {
			r.handle(s, req)
			return nil
		}); err != nil {
			s.send(result{ID: req.ID, Error: err.Error()})
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, s)
	maps.DeleteFunc(r.claims, func(_ int, owner *session) bool { return owner == s })
	maps.DeleteFunc(r.pending, func(_ int64, to route) bool { return to.session == s })
	r.settle()
	if len(r.sessions) == 0 {
		r.sleepSoon()
	}
	_ = conn.Close()
}

func (r *relay) handle(s *session, req request) {
	if !slices.Contains([]string{opHello, opBuilds, opRelay}, req.Op) {
		if err := r.awaitTabs(); err != nil {
			s.send(result{ID: req.ID, Error: err.Error()})
			return
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch req.Op {
	case opTabs:
		tabs := slices.SortedFunc(maps.Values(r.tabs), func(a, b Tab) int { return a.ID - b.ID })
		listed, _ := json.Marshal(slices.DeleteFunc(tabs, func(tab Tab) bool { return !reachable(tab.URL) }))
		s.send(result{ID: req.ID, OK: true, Value: listed})
	case opHello:
		var hello struct {
			Build string `json:"build"`
		}
		if json.Unmarshal(req.Args, &hello) != nil || hello.Build == r.builds.Tofu {
			s.send(result{ID: req.ID, OK: true})
			return
		}
		current, err := r.installed(r.installedTofu())
		if err != nil || current == r.builds.Tofu {
			s.send(result{ID: req.ID, Error: fmt.Sprintf("this tofu is older than the relay: restart it (the relay runs build %s, the installed tofu, and this tofu runs %s)", r.builds.Tofu, hello.Build)})
			return
		}
		if err := r.drainFor(current); err != nil {
			s.send(result{ID: req.ID, Error: err.Error()})
			return
		}
		s.send(result{ID: req.ID, Error: fmt.Sprintf("%s: this relay runs build %s, the installed tofu is %s, and the tofu that dialled it runs %s", relayRestarting, r.builds.Tofu, current, hello.Build)})
	case opBuilds:
		builds, _ := json.Marshal(r.builds)
		s.send(result{ID: req.ID, OK: true, Value: builds})
	case opRelay:
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		state, _ := json.Marshal(Relay{Awake: r.ready != nil, Builds: r.builds, HeldBytes: memory.Sys - memory.HeapReleased, OtherSessions: len(r.sessions) - 1, SleepAfter: r.sleepAfter()})
		s.send(result{ID: req.ID, OK: true, Value: state})
	default:
		if err := r.forward(s, req); err != nil {
			s.send(result{ID: req.ID, Error: err.Error()})
		}
	}
}

func (r *relay) admit(s *session, req request) (status, error) {
	switch req.Op {
	case opOpen, opNavigate:
		if _, err := openable(req.Args); err != nil {
			return "", err
		}
		if req.Op == opNavigate && !r.tabs[req.Tab].Opened {
			return "", fmt.Errorf("tab %d is the person's: tofu navigates only tabs it opened", req.Tab)
		}
		return r.shown, nil
	case opClose:
		if !r.tabs[req.Tab].Opened {
			return "", fmt.Errorf("tab %d is the person's: tofu closes only tabs it opened", req.Tab)
		}
		return r.shown, nil
	case opBack:
		if !r.tabs[req.Tab].Opened {
			return "", fmt.Errorf("tab %d is the person's: tofu navigates only tabs it opened", req.Tab)
		}
		return r.shown, nil
	}
	now, err := opStatus(req)
	if err != nil {
		return "", err
	}
	tab, open := r.tabs[req.Tab]
	switch {
	case !open:
		return "", fmt.Errorf("there is no tab %d in Chrome", req.Tab)
	case !reachable(tab.URL):
		return "", fmt.Errorf("tab %d shows %s, which tofu never reads or drives", req.Tab, tab.URL)
	case now == statusReading:
		return now, nil
	}
	if owner, claimed := r.claims[req.Tab]; claimed && owner != s {
		return "", fmt.Errorf("tab %d is driven by another tofu session", req.Tab)
	}
	r.claims[req.Tab] = s
	return now, nil
}

func (r *relay) forward(s *session, req request) error {
	if r.draining {
		return fmt.Errorf("%s: this relay runs build %s and exits once its last call is answered", relayRestarting, r.builds.Tofu)
	}
	if r.builds.Problem != "" {
		return errors.New(r.builds.Problem)
	}
	now, err := r.admit(s, req)
	if err != nil {
		return err
	}
	r.show(now)
	r.lastID++
	sent := time.Now()
	if err := r.tell(toExtension{T: messageCall, ID: r.lastID, TabID: req.Tab, Op: req.Op, Args: req.Args, Cursor: r.cursor && (req.Op == opCDP || req.Op == opNavigate)}); err != nil {
		return err
	}
	r.pending[r.lastID] = route{session: s, id: req.ID, sent: sent}
	return nil
}
