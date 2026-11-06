package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/tools/shell/process"
)

type Client struct {
	server  Server
	root    string
	cmd     *exec.Cmd
	tree    process.Tree
	input   io.WriteCloser
	writing sync.Mutex
	state   sync.Mutex
	next    int
	waiting map[int]chan reply
	opened  map[string]document
	status  string
	exited  chan struct{}
	quiet   chan struct{}
	settle  sync.Once
	release sync.Once
}

type document struct {
	version int
	text    string
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type reply struct {
	Result json.RawMessage
	Error  *rpcError
}

type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func start(server Server, root string) (*Client, error) {
	program, err := exec.LookPath(server.Program)
	if err != nil {
		return nil, errors.New(server.Program + " is not on PATH, and tofu never installs it: install it with " + server.Install)
	}
	cmd := exec.Command(program, server.Args...)
	printed := &stderrTail{}
	cmd.Dir, cmd.Stderr = root, printed
	input, inputErr := cmd.StdinPipe()
	output, outputErr := cmd.StdoutPipe()
	if err := errors.Join(inputErr, outputErr); err != nil {
		return nil, err
	}
	tree, err := process.StartTracked(cmd)
	if err != nil {
		return nil, fmt.Errorf("%s did not start: %w", server.Program, err)
	}
	c := &Client{server: server, root: root, cmd: cmd, tree: tree, input: input,
		waiting: map[int]chan reply{}, opened: map[string]document{}, exited: make(chan struct{}), quiet: make(chan struct{})}
	safe.Go(platformproject.Crashes(root), "lsp read "+server.Program, func() { c.read(output) })
	ctx, cancel := context.WithTimeout(context.Background(), konst.LspStartMillis*time.Millisecond)
	defer cancel()
	if err := c.initialize(ctx); err != nil {
		c.Shutdown()
		if last := printed.lines(); last != "" {
			return nil, fmt.Errorf("%s did not initialize under %s: %w. the last it printed to stderr: %s", server.Program, root, err, last)
		}
		return nil, fmt.Errorf("%s did not initialize under %s: %w, and printed nothing to stderr", server.Program, root, err)
	}
	return c, nil
}

type stderrTail struct {
	mutex sync.Mutex
	kept  []byte
}

func (t *stderrTail) Write(chunk []byte) (int, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.kept = append(t.kept, chunk...)
	t.kept = t.kept[max(0, len(t.kept)-konst.LspStderrBytes):]
	return len(chunk), nil
}

func (t *stderrTail) lines() string {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	trimmed := strings.TrimSpace(strings.ReplaceAll(string(t.kept), "\r\n", "\n"))
	if trimmed == "" {
		return ""
	}
	all := strings.Split(trimmed, "\n")
	return strings.TrimSuffix(strings.Join(all[max(0, len(all)-konst.LspStderrLines):], " | "), ".")
}

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"processId":        os.Getpid(),
		"rootUri":          fileURI(c.root),
		"workspaceFolders": []map[string]string{{"uri": fileURI(c.root), "name": filepath.Base(c.root)}},
		"capabilities": map[string]any{
			"textDocument": map[string]any{"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true}},
			"experimental": map[string]any{"serverStatusNotification": true},
		},
		"initializationOptions": c.server.Options,
	}
	if err := c.call(ctx, "initialize", params, nil); err != nil {
		return err
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	if !c.server.Quiesces {
		return nil
	}
	select {
	case <-c.quiet:
	case <-ctx.Done():
		c.state.Lock()
		c.status = "the project had not finished loading after " + strconv.Itoa(konst.LspStartMillis/1000) + " s, so this answer may miss call sites"
		c.state.Unlock()
	case <-c.exited:
		return errors.New("it exited while loading the project")
	}
	return nil
}

func (c *Client) alive() bool {
	select {
	case <-c.exited:
		return false
	default:
		return true
	}
}

func (c *Client) Status() string {
	c.state.Lock()
	defer c.state.Unlock()
	return c.status
}

func (c *Client) read(output io.Reader) {
	defer func() {
		_ = c.cmd.Wait()
		c.release.Do(c.tree.Release)
		close(c.exited)
	}()
	frames := textproto.NewReader(bufio.NewReader(output))
	for {
		header, err := frames.ReadMIMEHeader()
		if err != nil {
			return
		}
		length, err := strconv.Atoi(header.Get("Content-Length"))
		if err != nil {
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(frames.R, body); err != nil {
			return
		}
		var got message
		if json.Unmarshal(body, &got) != nil {
			continue
		}
		c.dispatch(got)
	}
}

func (c *Client) dispatch(got message) {
	switch {
	case got.Method == "" && len(got.ID) > 0:
		id, err := strconv.Atoi(string(got.ID))
		c.state.Lock()
		waiter := c.waiting[id]
		delete(c.waiting, id)
		c.state.Unlock()
		if err == nil && waiter != nil {
			waiter <- reply{Result: got.Result, Error: got.Error}
		}
	case len(got.ID) > 0:
		_ = c.write(map[string]any{"jsonrpc": "2.0", "id": got.ID, "result": answerTo(got)})
	case got.Method == "experimental/serverStatus":
		var status struct {
			Health    string `json:"health"`
			Quiescent bool   `json:"quiescent"`
			Message   string `json:"message"`
		}
		_ = json.Unmarshal(got.Params, &status)
		c.state.Lock()
		c.status = ""
		if status.Health != "ok" && status.Message != "" {
			c.status = status.Message
		}
		c.state.Unlock()
		if status.Quiescent {
			c.settle.Do(func() { close(c.quiet) })
		}
	}
}

func answerTo(request message) any {
	if request.Method != "workspace/configuration" {
		return nil
	}
	var asked struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(request.Params, &asked)
	return make([]any, len(asked.Items))
}

func (c *Client) write(body any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	c.writing.Lock()
	defer c.writing.Unlock()
	_, err = io.WriteString(c.input, "Content-Length: "+strconv.Itoa(len(encoded))+"\r\n\r\n"+string(encoded))
	return err
}

func (c *Client) notify(method string, params any) error {
	body := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		body["params"] = params
	}
	return c.write(body)
}

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	answered := make(chan reply, 1)
	c.state.Lock()
	c.next++
	id := c.next
	c.waiting[id] = answered
	c.state.Unlock()
	forget := func() {
		c.state.Lock()
		delete(c.waiting, id)
		c.state.Unlock()
	}
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		forget()
		return err
	}
	select {
	case got := <-answered:
		if got.Error != nil {
			return fmt.Errorf("%s answered %s with error %d: %s", c.server.Program, method, got.Error.Code, got.Error.Message)
		}
		if result == nil || len(got.Result) == 0 {
			return nil
		}
		return json.Unmarshal(got.Result, result)
	case <-ctx.Done():
		forget()
		return fmt.Errorf("%s did not answer %s: %w", c.server.Program, method, ctx.Err())
	case <-c.exited:
		return fmt.Errorf("%s exited before it answered %s", c.server.Program, method)
	}
}

func (c *Client) Open(paths ...string) error {
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		c.state.Lock()
		was, opened := c.opened[path]
		if was.text != text || !opened {
			c.opened[path] = document{version: was.version + 1, text: text}
		}
		c.state.Unlock()
		switch {
		case !opened:
			err = c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
				"uri": fileURI(path), "languageId": c.server.Extensions[filepath.Ext(path)], "version": 1, "text": text}})
		case was.text != text:
			err = c.notify("textDocument/didChange", map[string]any{
				"textDocument":   map[string]any{"uri": fileURI(path), "version": was.version + 1},
				"contentChanges": []map[string]string{{"text": text}}})
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), konst.LspShutdownMillis*time.Millisecond)
	defer cancel()
	if c.call(ctx, "shutdown", nil, nil) == nil {
		_ = c.notify("exit", nil)
	}
	_ = c.input.Close()
	select {
	case <-c.exited:
	case <-ctx.Done():
		_ = c.cmd.Process.Kill()
	}
	c.release.Do(c.tree.Release)
}
