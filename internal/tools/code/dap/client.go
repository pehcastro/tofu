package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"tofu/internal/platform/outside"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/tools/code/jsonrpc"
	"tofu/internal/tools/shell/process"
)

type Adapter struct {
	Program string
	Args    []string
	Install string
	Probe   []string
	Listens bool
}

type Event struct {
	Name string
	Body json.RawMessage
}

type Client struct {
	adapter Adapter
	cmd     *outside.Cmd
	tree    process.Tree
	closer  io.WriteCloser
	wire    *jsonrpc.Wire
	socket  string
	pending jsonrpc.Pending[message]
	state   sync.Mutex
	events  []Event
	arrived chan struct{}
	exited  chan struct{}
	release sync.Once
}

type message struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command"`
	RequestSeq int             `json:"request_seq"`
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
	Event      string          `json:"event"`
	Body       json.RawMessage `json:"body"`
}

type Pending struct {
	client  *Client
	command string
	seq     int
	answer  chan message
}

var ErrExited = errors.New("the debug adapter exited")

func Start(ctx context.Context, adapter Adapter, dir string) (*Client, error) {
	program, err := exec.LookPath(adapter.Program)
	if err == nil && adapter.Probe != nil {
		err = outside.Command(ctx, program, adapter.Probe...).Run()
	}
	if err != nil {
		return nil, errors.New(strings.Join(append([]string{adapter.Program}, adapter.Args...), " ") + " is not installed, and tofu never installs it: install it with " + adapter.Install)
	}
	c := &Client{adapter: adapter, arrived: make(chan struct{}, 1), exited: make(chan struct{})}
	args := adapter.Args
	if adapter.Listens {
		socketDir, err := os.MkdirTemp("", "tofu-dap-")
		if err != nil {
			return nil, err
		}
		c.socket = filepath.Join(socketDir, "dap.sock")
		args = append(slices.Clone(args), "--listen", "unix:"+c.socket)
	}
	c.cmd = outside.Command(context.Background(), program, args...)
	c.cmd.Dir = dir
	input, inputErr := c.cmd.StdinPipe()
	output, outputErr := c.cmd.StdoutPipe()
	if err := errors.Join(inputErr, outputErr); err != nil {
		return nil, err
	}
	if c.tree, err = process.StartClocked(c.cmd); err != nil {
		return nil, fmt.Errorf("%s did not start: %w", adapter.Program, err)
	}
	var reader io.Reader = output
	c.closer = input
	if adapter.Listens {
		conn, err := c.dial(ctx, output)
		if err != nil {
			c.kill()
			return nil, err
		}
		reader, c.closer = conn, conn
	}
	c.wire = jsonrpc.NewWire(c.closer)
	safe.Go(platformproject.Crashes(dir), "dap read "+adapter.Program, func() {
		jsonrpc.Read(reader, c.dispatch)
		_ = c.cmd.Wait()
		c.release.Do(c.tree.Release)
		close(c.exited)
	})
	return c, nil
}

func (c *Client) dial(ctx context.Context, output io.Reader) (net.Conn, error) {
	listening := make(chan struct{}, 1)
	safe.Go(platformproject.Crashes(c.cmd.Dir), "dap stdout "+c.adapter.Program, func() {
		lines := bufio.NewScanner(output)
		announced := false
		for lines.Scan() {
			if !announced && strings.Contains(lines.Text(), "listening at") {
				announced = true
				listening <- struct{}{}
			}
		}
		_, _ = io.Copy(io.Discard, output)
		if !announced {
			close(listening)
		}
	})
	select {
	case _, said := <-listening:
		if !said {
			return nil, fmt.Errorf("%s exited before it listened", c.adapter.Program)
		}
	case <-ctx.Done():
		return nil, fmt.Errorf("%s did not listen: %w", c.adapter.Program, ctx.Err())
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", c.socket)
}

func (c *Client) Pid() int { return c.cmd.Process.Pid }

func (c *Client) dispatch(body []byte) {
	var got message
	if json.Unmarshal(body, &got) != nil {
		return
	}
	switch got.Type {
	case "response":
		c.pending.Answer(got.RequestSeq, got)
	case "event":
		c.state.Lock()
		c.events = append(c.events, Event{Name: got.Event, Body: got.Body})
		c.state.Unlock()
		select {
		case c.arrived <- struct{}{}:
		default:
		}
	case "request":
		_ = c.wire.Send(map[string]any{"seq": c.pending.Number(), "type": "response", "request_seq": got.Seq, "command": got.Command,
			"success": false, "message": "tofu does not answer " + got.Command})
	}
}

func (c *Client) Send(command string, arguments any) (Pending, error) {
	seq, answer := c.pending.Open()
	pending := Pending{client: c, command: command, seq: seq, answer: answer}
	request := map[string]any{"seq": seq, "type": "request", "command": command}
	if arguments != nil {
		request["arguments"] = arguments
	}
	if err := c.wire.Send(request); err != nil {
		c.pending.Forget(seq)
		return pending, fmt.Errorf("%s could not be asked %s: %w", c.adapter.Program, command, err)
	}
	return pending, nil
}

func (p Pending) Wait(ctx context.Context, body any) error {
	defer outside.Wait(ctx, outside.Process)()
	got, err := p.client.pending.Await(ctx, p.seq, p.answer, p.client.exited)
	switch {
	case errors.Is(err, jsonrpc.ErrGone):
		return fmt.Errorf("%s exited before it answered %s", p.client.adapter.Program, p.command)
	case err != nil:
		return fmt.Errorf("%s did not answer %s: %w", p.client.adapter.Program, p.command, err)
	case !got.Success:
		var detail struct {
			Error struct {
				Format string `json:"format"`
			} `json:"error"`
		}
		_ = json.Unmarshal(got.Body, &detail)
		return fmt.Errorf("%s refused %s: %s", p.client.adapter.Program, p.command, strings.TrimSpace(got.Message+" "+detail.Error.Format))
	case body == nil || len(got.Body) == 0:
		return nil
	}
	return json.Unmarshal(got.Body, body)
}

func (c *Client) Call(ctx context.Context, command string, arguments, body any) error {
	pending, err := c.Send(command, arguments)
	if err != nil {
		return err
	}
	return pending.Wait(ctx, body)
}

func (c *Client) Next(ctx context.Context) (Event, error) {
	defer outside.Wait(ctx, outside.Process)()
	for {
		c.state.Lock()
		if len(c.events) > 0 {
			next := c.events[0]
			c.events = c.events[1:]
			c.state.Unlock()
			return next, nil
		}
		c.state.Unlock()
		select {
		case <-c.arrived:
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case <-c.exited:
			c.state.Lock()
			left := len(c.events)
			c.state.Unlock()
			if left == 0 {
				return Event{}, ErrExited
			}
		}
	}
}

func (c *Client) Stop(ctx context.Context) bool {
	select {
	case <-c.exited:
	default:
		_ = c.Call(ctx, "disconnect", map[string]any{"terminateDebuggee": true}, nil)
	}
	_ = c.closer.Close()
	select {
	case <-c.exited:
		c.kill()
		return true
	case <-ctx.Done():
		c.kill()
		return false
	}
}

func (c *Client) kill() {
	_ = c.cmd.Process.Kill()
	c.release.Do(c.tree.Release)
	if c.socket != "" {
		_ = os.RemoveAll(filepath.Dir(c.socket))
	}
}
