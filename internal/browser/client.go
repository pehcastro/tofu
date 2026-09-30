package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
)

const (
	NotConnected = "the tofu extension is not connected"
	InstallHint  = "tofu browser install, then load it in Chrome"
)

var (
	ErrNotConnected    = errors.New(NotConnected + ": run " + InstallHint)
	ErrRelayRestarting = errors.New("the tofu relay runs another build and restarts on this one")
)

type droppedAfterTimeout struct{ deadline error }

func (droppedAfterTimeout) Error() string {
	return "tofu dropped the browser connection and the next call reconnects"
}

func (droppedAfterTimeout) Is(target error) bool { return target == ErrNotConnected }

func (d droppedAfterTimeout) Unwrap() error { return d.deadline }

type CallTime struct {
	Wall      time.Duration
	Host      time.Duration
	Extension *Timing
}

type Client struct {
	Timed  func(CallTime)
	mu     sync.Mutex
	conn   net.Conn
	out    *json.Encoder
	in     *json.Decoder
	lastID int64
}

func Dial(home string) (*Client, error) {
	path, err := socketPath(home)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", path, konst.BrowserDialTimeoutMillis*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNotConnected, err)
	}
	client := &Client{conn: conn, out: json.NewEncoder(conn), in: json.NewDecoder(conn)}
	build, err := Build()
	var hello []byte
	if err == nil {
		hello, err = json.Marshal(map[string]string{"build": build})
	}
	if err == nil {
		_, err = client.Call(0, opHello, hello)
	}
	if err != nil && strings.HasPrefix(err.Error(), relayRestarting) {
		_ = client.Close()
		return nil, fmt.Errorf("%w (%v)", ErrRelayRestarting, err)
	}
	if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("unknown browser op %q", opHello)) {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *Client) Tabs() ([]Tab, error) {
	raw, err := c.Call(0, opTabs, nil)
	if err != nil {
		return nil, err
	}
	var tabs []Tab
	return tabs, json.Unmarshal(raw, &tabs)
}

func (c *Client) Open(url string) (int, error) {
	args, _ := json.Marshal(openArgs{URL: url})
	raw, err := c.Call(0, opOpen, args)
	if err != nil {
		return 0, err
	}
	var tab int
	if err := json.Unmarshal(raw, &tab); err != nil || tab == 0 {
		return 0, fmt.Errorf("the extension answered open with %q, not a tab id", raw)
	}
	return tab, nil
}

func (c *Client) CloseTab(tab int) error {
	_, err := c.Call(tab, opClose, nil)
	return err
}

type ScreencastFrame struct {
	Data          []byte  `json:"data"`
	ChromeSeconds float64 `json:"timestamp"`
}

func (c *Client) StartScreencast(tab int) error {
	_, err := c.Call(tab, opScreencast, json.RawMessage(`{"action":"start"}`))
	return err
}

func (c *Client) StopScreencast(tab int) ([]ScreencastFrame, error) {
	raw, err := c.Call(tab, opScreencast, json.RawMessage(`{"action":"stop"}`))
	if err != nil {
		return nil, err
	}
	var frames []ScreencastFrame
	return frames, json.Unmarshal(raw, &frames)
}

func (c *Client) Call(tab int, op string, args json.RawMessage) (json.RawMessage, error) {
	value, err := c.callBy(time.Now().Add(konst.BrowserCallTimeoutMillis*time.Millisecond), tab, op, args)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return nil, fmt.Errorf("the browser did not answer %s on tab %d within %d ms: %w", op, tab, konst.BrowserCallTimeoutMillis, err)
	}
	return value, err
}

func (c *Client) callBy(deadline time.Time, tab int, op string, args json.RawMessage) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastID++
	if err := c.conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNotConnected, err)
	}
	var answer result
	started := time.Now()
	err := c.out.Encode(request{ID: c.lastID, Op: op, Tab: tab, Args: args})
	for err == nil && answer.ID < c.lastID {
		answer = result{}
		err = c.in.Decode(&answer)
	}
	if err == nil && c.Timed != nil {
		c.Timed(CallTime{Wall: time.Since(started), Host: answer.Host, Extension: answer.Timing})
	}
	switch {
	case errors.Is(err, os.ErrDeadlineExceeded):
		_ = c.conn.Close()
		return nil, droppedAfterTimeout{err}
	case err != nil:
		return nil, fmt.Errorf("%w (%v)", ErrNotConnected, err)
	case answer.ID != c.lastID:
		return nil, fmt.Errorf("the browser host answered call %d with the answer to %d", c.lastID, answer.ID)
	case !answer.OK:
		return nil, errors.New(answer.Error)
	}
	return answer.Value, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}
