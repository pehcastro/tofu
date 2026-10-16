package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/sys"
)

type cdpConn struct {
	ws      *websocket.Conn
	mu      sync.Mutex
	lastID  int64
	waiting map[int64]chan cdpMessage
	events  map[string]func(json.RawMessage)
	done    chan struct{}
	lost    error
}

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    any             `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func newCDPConn(ws *websocket.Conn) *cdpConn {
	c := &cdpConn{ws: ws, waiting: map[int64]chan cdpMessage{}, events: map[string]func(json.RawMessage){}, done: make(chan struct{})}
	report := sys.Crashes(".")
	safe.Go(report, "browser reader", func() {
		defer close(c.done)
		c.lost = safe.Call(report, "browser reader", c.read)
	})
	return c
}

func (c *cdpConn) read() error {
	for {
		var in struct {
			cdpMessage
			Params json.RawMessage `json:"params"`
		}
		if websocket.JSON.Receive(c.ws, &in) != nil {
			return nil
		}
		c.mu.Lock()
		waiter, event := c.waiting[in.ID], c.events[in.SessionID+" "+in.Method]
		delete(c.waiting, in.ID)
		c.mu.Unlock()
		switch {
		case in.ID != 0 && waiter != nil:
			waiter <- in.cdpMessage
		case in.ID == 0 && event != nil:
			event(in.Params)
		}
	}
}

func (c *cdpConn) on(session, method string, handle func(json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events[session+" "+method] = handle
}

func (c *cdpConn) off(session, method string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.events, session+" "+method)
}

func (c *cdpConn) send(session, method string, params any) (json.RawMessage, error) {
	answer := make(chan cdpMessage, 1)
	c.mu.Lock()
	c.lastID++
	id := c.lastID
	c.waiting[id] = answer
	err := websocket.JSON.Send(c.ws, cdpMessage{ID: id, SessionID: session, Method: method, Params: params})
	if err != nil {
		delete(c.waiting, id)
	}
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNotConnected, err)
	}
	select {
	case got := <-answer:
		if got.Error != nil {
			return nil, errors.New(got.Error.Message)
		}
		return got.Result, nil
	case <-c.done:
		if c.lost != nil {
			return nil, fmt.Errorf("%w: the debugging connection ended during %s: %w", ErrNotConnected, method, c.lost)
		}
		return nil, fmt.Errorf("%w: Chrome closed the debugging connection during %s", ErrNotConnected, method)
	case <-time.After(konst.BrowserCallTimeoutMillis * time.Millisecond):
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("the profile's Chrome did not answer %s within %d ms", method, konst.BrowserCallTimeoutMillis)
	}
}
