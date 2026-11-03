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
	callLimit = 2 * time.Minute
	turnLimit = 5 * time.Minute
	exitLimit = 30 * time.Second
	hostPoll  = 200 * time.Millisecond
	lineBytes = 64 << 20
)

type wireLine struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
	size   int
}

type served struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines chan wireLine
	clock clock
	next  int
}

func serve(tofu, project, cassette, log string, env []string) (*served, error) {
	cmd := exec.Command(tofu, "serve", "--stdio", "--dir", project, "--cassette", cassette)
	cmd.Env = env
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
	s := &served{cmd: cmd, in: in, lines: make(chan wireLine, 1024), clock: newClock()}
	go func() {
		defer close(s.lines)
		defer func() { _ = stderr.Close() }()
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 0, 1<<20), lineBytes)
		for scanner.Scan() {
			line := wireLine{size: len(scanner.Bytes())}
			if json.Unmarshal(scanner.Bytes(), &line) == nil {
				s.lines <- line
			}
		}
	}()
	return s, nil
}

func (s *served) await(deadline <-chan time.Time, wanted func(wireLine) bool) (wireLine, error) {
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

func (s *served) send(method string, params any) (string, int64, error) {
	s.next++
	id := strconv.Quote(strconv.Itoa(s.next))
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params})
	if err != nil {
		return "", 0, err
	}
	started := s.clock.now()
	_, err = s.in.Write(append(body, '\n'))
	return id, started, err
}

func (s *served) call(method string, params any) (json.RawMessage, time.Duration, error) {
	id, started, err := s.send(method, params)
	if err != nil {
		return nil, 0, err
	}
	line, err := s.await(time.After(callLimit), func(line wireLine) bool { return line.Method == "" && string(line.ID) == id })
	took := s.clock.since(started)
	switch {
	case err != nil:
		return nil, took, fmt.Errorf("%s: %w", method, err)
	case line.Error != nil:
		return nil, took, fmt.Errorf("%s: %s", method, line.Error)
	}
	return line.Result, took, nil
}

func (s *served) turn(session, text string) error {
	if _, _, err := s.call("turn.send", host.TurnSendParams{Session: session, Text: text}); err != nil {
		return err
	}
	line, err := s.await(time.After(turnLimit), func(line wireLine) bool {
		var done host.TurnCompleted
		return line.Method == "turn.completed" && json.Unmarshal(line.Params, &done) == nil && done.Agent == ""
	})
	if err != nil {
		return fmt.Errorf("turn %q: %w", text, err)
	}
	var done host.TurnCompleted
	if err := json.Unmarshal(line.Params, &done); err != nil || done.Status != host.StatusFinished {
		return fmt.Errorf("turn %q ended %s", text, done.Status)
	}
	return nil
}

func hostGone(home string) error {
	deadline := time.Now().Add(exitLimit)
	for {
		socks, err := filepath.Glob(filepath.Join(home, ".tofu", "h", "*.sock"))
		switch {
		case err != nil || len(socks) == 0:
			return err
		case time.Now().After(deadline):
			return errors.New("the tofu host still listens on " + socks[0] + " after its last member left")
		}
		time.Sleep(hostPoll)
	}
}

func (s *served) close() error {
	_ = s.in.Close()
	exited := make(chan error, 1)
	go func() { exited <- s.cmd.Wait() }()
	select {
	case err := <-exited:
		return err
	case <-time.After(exitLimit):
		_ = s.cmd.Process.Kill()
		return errors.New("tofu serve did not exit after its input closed, and was killed")
	}
}
