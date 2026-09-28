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

type session struct {
	conn net.Conn
	out  *json.Encoder
}

type route struct {
	session *session
	id      int64
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
	r := &relay{extension: stdout, tabs: map[int]Tab{}, claims: map[int]*session{}, pending: map[int64]route{}, sessions: map[*session]bool{}}
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
			_, shared := r.tabs[tab]
			return !shared
		})
	case messageShared:
		message.Tab.Mode = message.Mode
		r.tabs[message.Tab.ID] = message.Tab
	case messageUnshared:
		delete(r.tabs, message.TabID)
		delete(r.claims, message.TabID)
	case messageTabUpdated:
		if tab, shared := r.tabs[message.Tab.ID]; shared {
			tab.URL, tab.Title = message.Tab.URL, message.Tab.Title
			r.tabs[tab.ID] = tab
		}
	case messageResult:
		to, asked := r.pending[message.ID]
		if !asked {
			return
		}
		delete(r.pending, message.ID)
		message.ID = to.id
		_ = to.session.out.Encode(message.result)
	}
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
	_ = conn.Close()
}

func (r *relay) handle(s *session, req request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if req.Op == opTabs {
		tabs, _ := json.Marshal(slices.SortedFunc(maps.Values(r.tabs), func(a, b Tab) int { return a.ID - b.ID }))
		_ = s.out.Encode(result{ID: req.ID, OK: true, Value: tabs})
		return
	}
	if err := r.forward(s, req); err != nil {
		_ = s.out.Encode(result{ID: req.ID, Error: err.Error()})
	}
}

func (r *relay) admit(s *session, req request) error {
	switch req.Op {
	case opOpen:
		var args openArgs
		_ = json.Unmarshal(req.Args, &args)
		if parsed, err := url.Parse(args.URL); err != nil || !slices.Contains([]string{"http", "https", "file"}, parsed.Scheme) {
			return fmt.Errorf("tofu opens only http, https and file URLs, not %q", args.URL)
		}
		return nil
	case opClose:
		if !r.tabs[req.Tab].Opened {
			return fmt.Errorf("tab %d is the person's: tofu closes only tabs it opened", req.Tab)
		}
		return nil
	}
	mode, err := opMode(req.Op)
	if err != nil {
		return err
	}
	tab, shared := r.tabs[req.Tab]
	if !shared {
		return fmt.Errorf("tab %d is not shared with tofu", req.Tab)
	}
	if mode == ModeDrive {
		if tab.Mode != ModeDrive {
			return fmt.Errorf("tab %d is shared for reading only", req.Tab)
		}
		if owner, claimed := r.claims[req.Tab]; claimed && owner != s {
			return fmt.Errorf("tab %d is driven by another tofu session", req.Tab)
		}
		r.claims[req.Tab] = s
	}
	return nil
}

func (r *relay) forward(s *session, req request) error {
	if err := r.admit(s, req); err != nil {
		return err
	}
	r.lastID++
	raw, err := json.Marshal(extensionCall{T: messageCall, ID: r.lastID, TabID: req.Tab, Op: req.Op, Args: req.Args})
	if err != nil {
		return err
	}
	if err := WriteMessage(r.extension, raw); err != nil {
		return err
	}
	r.pending[r.lastID] = route{s, req.ID}
	return nil
}
