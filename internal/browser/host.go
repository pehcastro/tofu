package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"tofu/internal/konst"
)

const idleAfter = konst.BrowserIdleAfterMillis * time.Millisecond

type session struct {
	conn net.Conn
	out  *json.Encoder
}

type route struct {
	session *session
	id      int64
	sent    time.Time
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
}

func Host(origin string, stdin io.Reader, stdout io.Writer, home string) error {
	_, manifestPath := installPaths(home)
	var installed hostManifest
	raw, err := os.ReadFile(manifestPath)
	if err == nil {
		err = json.Unmarshal(raw, &installed)
	}
	if err != nil || !slices.Contains(installed.AllowedOrigins, origin) {
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
	r := &relay{extension: stdout, tabs: map[int]Tab{}, claims: map[int]*session{}, pending: map[int64]route{}, sessions: map[*session]bool{}, shown: statusIdle}
	r.idle = time.AfterFunc(idleAfter, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if !r.closed && len(r.pending) == 0 {
			r.show(statusIdle)
		}
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go r.serve(conn)
		}
	}()

	err = r.readExtension(stdin)
	_ = listener.Close()
	r.mu.Lock()
	r.closed = true
	for s := range r.sessions {
		_ = s.conn.Close()
	}
	r.mu.Unlock()
	return err
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
		r.receive(message)
	}
}

func (r *relay) receive(message extensionMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch message.T {
	case messageHello:
		r.tabs = map[int]Tab{}
		for _, tab := range message.Tabs {
			r.tabs[tab.ID] = tab
		}
		maps.DeleteFunc(r.claims, func(tab int, _ *session) bool {
			_, open := r.tabs[tab]
			return !open
		})
	case messageTabRemoved:
		delete(r.tabs, message.TabID)
		delete(r.claims, message.TabID)
	case messageTabUpdated:
		r.tabs[message.Tab.ID] = message.Tab
	case messageResult:
		to, asked := r.pending[message.ID]
		if !asked {
			return
		}
		delete(r.pending, message.ID)
		message.ID, message.Host = to.id, time.Since(to.sent)
		_ = to.session.out.Encode(message.result)
		r.idleSoon()
	}
}

func (r *relay) idleSoon() {
	if len(r.pending) == 0 {
		r.idle.Reset(idleAfter)
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
		r.handle(s, req)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, s)
	maps.DeleteFunc(r.claims, func(_ int, owner *session) bool { return owner == s })
	maps.DeleteFunc(r.pending, func(_ int64, to route) bool { return to.session == s })
	r.idleSoon()
	_ = conn.Close()
}

func (r *relay) handle(s *session, req request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if req.Op == opTabs {
		tabs := slices.SortedFunc(maps.Values(r.tabs), func(a, b Tab) int { return a.ID - b.ID })
		listed, _ := json.Marshal(slices.DeleteFunc(tabs, func(tab Tab) bool { return !reachable(tab.URL) }))
		_ = s.out.Encode(result{ID: req.ID, OK: true, Value: listed})
		return
	}
	if err := r.forward(s, req); err != nil {
		_ = s.out.Encode(result{ID: req.ID, Error: err.Error()})
	}
}

func (r *relay) admit(s *session, req request) (status, error) {
	switch req.Op {
	case opOpen:
		var args openArgs
		_ = json.Unmarshal(req.Args, &args)
		if parsed, err := url.Parse(args.URL); err != nil || !slices.Contains([]string{"http", "https", "file"}, parsed.Scheme) {
			return "", fmt.Errorf("tofu opens only http, https and file URLs, not %q", args.URL)
		}
		return r.shown, nil
	case opClose:
		if !r.tabs[req.Tab].Opened {
			return "", fmt.Errorf("tab %d is the person's: tofu closes only tabs it opened", req.Tab)
		}
		return r.shown, nil
	}
	now, err := opStatus(req.Op)
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
	now, err := r.admit(s, req)
	if err != nil {
		return err
	}
	r.show(now)
	r.lastID++
	sent := time.Now()
	if err := r.tell(toExtension{T: messageCall, ID: r.lastID, TabID: req.Tab, Op: req.Op, Args: req.Args}); err != nil {
		return err
	}
	r.pending[r.lastID] = route{s, req.ID, sent}
	return nil
}
