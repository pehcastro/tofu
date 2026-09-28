package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"tofu/internal/konst"
)

var ErrNotConnected = errors.New("the tofu extension is not connected: run tofu browser install, then load it in Chrome")

type Client struct {
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
	return &Client{conn: conn, out: json.NewEncoder(conn), in: json.NewDecoder(conn)}, nil
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

func (c *Client) Call(tab int, op string, args json.RawMessage) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastID++
	if err := c.conn.SetDeadline(time.Now().Add(konst.BrowserCallTimeoutMillis * time.Millisecond)); err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNotConnected, err)
	}
	var answer result
	err := c.out.Encode(request{ID: c.lastID, Op: op, Tab: tab, Args: args})
	if err == nil {
		err = c.in.Decode(&answer)
	}
	switch {
	case errors.Is(err, os.ErrDeadlineExceeded):
		return nil, fmt.Errorf("the browser did not answer %s on tab %d within %d ms", op, tab, konst.BrowserCallTimeoutMillis)
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
