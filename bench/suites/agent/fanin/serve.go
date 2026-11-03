package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"tofu/internal/host"
)

const (
	callLimit  = 2 * time.Minute
	exitLimit  = 30 * time.Second
	lineBytes  = 64 << 20
	agentEnded = "agent.ended"
)

type wireLine struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type served struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	lines   chan wireLine
	pending []wireLine
	next    int
}

func serve(tofu, cassette, project, log string, env []string) (*served, error) {
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
	s := &served{cmd: cmd, in: in, lines: make(chan wireLine, 256)}
	go func() {
		defer close(s.lines)
		defer func() { _ = stderr.Close() }()
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 0, 1<<20), lineBytes)
		for scanner.Scan() {
			var line wireLine
			if json.Unmarshal(scanner.Bytes(), &line) == nil {
				s.lines <- line
			}
		}
	}()
	return s, nil
}

func (s *served) read(deadline <-chan time.Time) (wireLine, error) {
	if len(s.pending) > 0 {
		line := s.pending[0]
		s.pending = s.pending[1:]
		return line, nil
	}
	select {
	case line, open := <-s.lines:
		if !open {
			return wireLine{}, errors.New("tofu serve closed its output")
		}
		return line, nil
	case <-deadline:
		return wireLine{}, os.ErrDeadlineExceeded
	}
}

func (s *served) call(method string, params, result any) error {
	s.next++
	id := strconv.Itoa(s.next)
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err == nil {
		_, err = s.in.Write(append(body, '\n'))
	}
	if err != nil {
		return err
	}
	deadline := time.After(callLimit)
	var held []wireLine
	defer func() { s.pending = append(s.pending, held...) }()
	for {
		line, err := s.read(deadline)
		if err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
		if line.Method != "" || string(line.ID) != strconv.Quote(id) {
			held = append(held, line)
			continue
		}
		if line.Error != nil {
			return fmt.Errorf("%s: %s", method, line.Error.Message)
		}
		return json.Unmarshal(line.Result, result)
	}
}

func (s *served) settle(finishing int, quiet, limit time.Duration) error {
	deadline := time.After(limit)
	ended := map[string]bool{}
	for {
		wait := deadline
		if len(ended) >= finishing {
			wait = time.After(quiet)
		}
		line, err := s.read(wait)
		switch {
		case errors.Is(err, os.ErrDeadlineExceeded) && len(ended) >= finishing:
			return nil
		case err != nil:
			return fmt.Errorf("%d of %d workers ended: %w", len(ended), finishing, err)
		}
		var done host.Identity
		if line.Method == agentEnded && json.Unmarshal(line.Params, &done) == nil {
			ended[done.Agent] = true
		}
	}
}

func (s *served) close() {
	_ = s.in.Close()
	exited := make(chan error, 1)
	go func() { exited <- s.cmd.Wait() }()
	select {
	case <-exited:
	case <-time.After(exitLimit):
		_ = s.cmd.Process.Kill()
		<-exited
	}
}
