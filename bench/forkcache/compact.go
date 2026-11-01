package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"tofu/internal/host"
)

const (
	compactArm       = "compact"
	turnAfterCompact = "MOCK one more turn after compact"
	serveCallLimit   = 2 * time.Minute
	serveTurnLimit   = 5 * time.Minute
	serveExitLimit   = 30 * time.Second
	serveLineBytes   = 64 << 20
)

type compactRow struct {
	session       string
	compaction    host.Compaction
	before, after usage
	asides        usage
	shared        sharedPrefix
	first         host.Compaction
}

type wireLine struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type served struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines chan wireLine
	next  int
}

func startServe(exe, dir, log string, env []string) (*served, error) {
	cmd := exec.Command(exe, "serve", "--stdio", "--dir", dir)
	cmd.Dir, cmd.Env = dir, env
	stderr, err := os.Create(log)
	if err != nil {
		return nil, err
	}
	cmd.Stderr = stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &served{cmd: cmd, in: in, lines: make(chan wireLine, 1024)}
	go func() {
		defer close(s.lines)
		defer func() { _ = stderr.Close() }()
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 0, 1<<20), serveLineBytes)
		for scanner.Scan() {
			var line wireLine
			if json.Unmarshal(scanner.Bytes(), &line) == nil {
				s.lines <- line
			}
		}
	}()
	return s, nil
}

func (s *served) await(limit time.Duration, wanted func(wireLine) bool) (wireLine, error) {
	deadline := time.After(limit)
	for {
		select {
		case line, open := <-s.lines:
			if !open {
				return wireLine{}, errors.New("tofu serve closed its output")
			}
			if wanted(line) {
				return line, nil
			}
		case <-deadline:
			return wireLine{}, os.ErrDeadlineExceeded
		}
	}
}

func (s *served) call(method string, params, result any) error {
	s.next++
	id := strconv.Quote(strconv.Itoa(s.next))
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := s.in.Write(append(body, '\n')); err != nil {
		return err
	}
	line, err := s.await(serveCallLimit, func(line wireLine) bool { return line.Method == "" && string(line.ID) == id })
	switch {
	case err != nil:
		return fmt.Errorf("%s: %w", method, err)
	case line.Error != nil:
		return fmt.Errorf("%s: %s", method, line.Error)
	case result == nil:
		return nil
	}
	return json.Unmarshal(line.Result, result)
}

func (s *served) turn(session, text string) error {
	if err := s.call("turn.send", host.TurnSendParams{Session: session, Text: text}, nil); err != nil {
		return err
	}
	var done host.TurnCompleted
	_, err := s.await(serveTurnLimit, func(line wireLine) bool {
		return line.Method == "turn.completed" && json.Unmarshal(line.Params, &done) == nil && done.Agent == ""
	})
	if err == nil && done.Status != host.StatusFinished {
		err = fmt.Errorf("ended %s", done.Status)
	}
	if err != nil {
		return fmt.Errorf("turn %.40q: %w", text, err)
	}
	return nil
}

func (s *served) close() error {
	_ = s.in.Close()
	exited := make(chan error, 1)
	go func() { exited <- s.cmd.Wait() }()
	select {
	case err := <-exited:
		return err
	case <-time.After(serveExitLimit):
		_ = s.cmd.Process.Kill()
		return errors.New("tofu serve did not exit after its input closed, and was killed")
	}
}

func compactOnce(exe, work string, played script) (compactRow, error) {
	s, err := stage(exe, work, played, nil)
	if err != nil {
		return compactRow{}, err
	}
	defer s.stood.Close()
	serving, err := startServe(exe, s.dir, filepath.Join(work, "serve.log"), s.env)
	if err != nil {
		return compactRow{}, err
	}
	row := compactRow{session: played.name}
	compactedAt, err := compactThrough(serving, s, &row)
	if err = errors.Join(err, serving.close()); err != nil {
		return compactRow{}, err
	}
	if err := s.kept(work, compactArm); err != nil {
		return compactRow{}, err
	}
	var before []block
	for at, e := range s.stood.sent {
		switch {
		case at < compactedAt && e.step:
			row.before, before = e.billed, e.blocks
		case at < compactedAt:
		case !e.step:
			row.asides = row.asides.plus(e.billed)
		default:
			row.after, row.shared = e.billed, sharedWith(before, e.blocks)
			return row, nil
		}
	}
	return row, nil
}

func compactThrough(serving *served, s staged, row *compactRow) (int, error) {
	var state host.SessionState
	for _, opening := range []struct {
		method         string
		params, result any
	}{{"initialize", host.InitializeParams{Client: "bench-forkcache"}, nil}, {"session.open", host.SessionOpenParams{Asking: host.AskingAuto}, nil}, {"session.state", struct{}{}, &state}} {
		if err := serving.call(opening.method, opening.params, opening.result); err != nil {
			return 0, err
		}
	}
	if err := serving.call("session.compact", host.SessionParams{Session: state.Session}, &row.first); err != nil {
		return 0, err
	}
	if err := serving.turn(state.Session, s.task); err != nil {
		return 0, err
	}
	s.stood.held.Lock()
	compactedAt := len(s.stood.sent)
	s.stood.held.Unlock()
	if err := serving.call("session.compact", host.SessionParams{Session: state.Session}, &row.compaction); err != nil {
		return 0, err
	}
	if row.compaction.Into == "" {
		return compactedAt, nil
	}
	return compactedAt, serving.turn(row.compaction.Into, turnAfterCompact)
}
