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
	"slices"
	"strconv"
	"time"

	"tofu/internal/host"
	"tofu/internal/recall"
)

const (
	turnLimit   = 15 * time.Minute
	callLimit   = 2 * time.Minute
	exitLimit   = time.Minute
	lineBytes   = 64 << 20
	approvalAsk = "tofu/requestApproval"
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
	refused int
	used    []int
	sentIn  string
}

func serve(opts options, env []string, project, log string) (*served, error) {
	args := []string{"serve", "--stdio", "--dir", project}
	if opts.cassette != "" {
		args = append(args, "--cassette", opts.cassette)
	}
	cmd := exec.Command(opts.tofu, args...)
	cmd.Env = append(slices.Clone(env), recall.CeilingVariable+"="+strconv.Itoa(opts.ceiling))
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

func (s *served) write(message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = s.in.Write(append(body, '\n'))
	return err
}

func (s *served) read(deadline <-chan time.Time) (wireLine, error) {
	for {
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
			if line.Method != approvalAsk || line.ID == nil {
				return line, nil
			}
			s.refused++
			if err := s.write(map[string]any{"jsonrpc": "2.0", "id": line.ID, "result": host.ApprovalAnswer{Decision: host.RejectOnce}}); err != nil {
				return wireLine{}, err
			}
		case <-deadline:
			return wireLine{}, errors.New("tofu serve went quiet past the time limit")
		}
	}
}

func (s *served) call(method string, params, result any) error {
	s.next++
	id := strconv.Itoa(s.next)
	if err := s.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
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

func (s *served) turn(text string) (string, error) {
	var state host.SessionState
	if err := s.call("session.state", struct{}{}, &state); err != nil {
		return "", err
	}
	s.sentIn = state.Session
	if state.Context != nil {
		s.used = append(s.used, state.Context.Used)
	}
	var started host.TurnResult
	if err := s.call("turn.send", host.TurnSendParams{Session: state.Session, Text: text}, &started); err != nil {
		return "", err
	}
	deadline := time.After(turnLimit)
	answer, failure := "", ""
	for {
		line, err := s.read(deadline)
		if err != nil {
			return answer, fmt.Errorf("turn %s: %w", started.Turn, err)
		}
		switch line.Method {
		case "failure":
			var said host.Said
			if json.Unmarshal(line.Params, &said) == nil {
				failure = said.Text
			}
		case "message.completed":
			var said host.Text
			if json.Unmarshal(line.Params, &said) == nil && said.Agent == "" {
				answer = said.Text
			}
		case "turn.completed":
			var done host.TurnCompleted
			if json.Unmarshal(line.Params, &done) != nil || done.Agent != "" {
				continue
			}
			if done.Status != host.StatusFinished {
				return answer, fmt.Errorf("turn %s ended %s: %s", started.Turn, done.Status, failure)
			}
			return answer, nil
		}
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

type chainRun struct {
	chain       chain
	answer      string
	askedIn     string
	generations []string
	traces      []host.SessionTrace
	refused     int
	used        []int
}

func driveChain(opts options, env []string, dir string, a arm, ch chain) (chainRun, error) {
	project := filepath.Join(dir, "project")
	if err := writeFiles(project, map[string]string{".tofu/settings.json": `{"autoMemory": 0}`}); err != nil {
		return chainRun{}, err
	}
	s, err := serve(opts, env, project, filepath.Join(dir, "serve.log"))
	if err != nil {
		return chainRun{}, err
	}
	run := chainRun{chain: ch}
	driven := func() error {
		steps := []struct {
			method string
			params any
		}{
			{"initialize", map[string]string{"client": "bench-memory"}},
			{"session.open", host.SessionOpenParams{Asking: host.AskingAuto}},
			{"session.set", host.SessionSetParams{ModelPick: host.ModelPick{Wire: opts.wire, Model: opts.model}}},
		}
		for _, step := range steps {
			if err := s.call(step.method, step.params, &json.RawMessage{}); err != nil {
				return err
			}
		}
		if a == armToday {
			for _, said := range ch.Sessions {
				if err := writeFiles(project, said.Files); err != nil {
					return err
				}
				for _, text := range said.Says {
					if _, err := s.turn(text); err != nil {
						return err
					}
				}
				for path := range said.Files {
					if err := os.Remove(filepath.Join(project, filepath.FromSlash(path))); err != nil {
						return err
					}
				}
			}
		}
		run.answer, err = s.turn(ch.ask())
		return err
	}
	err = driven()
	run.askedIn, run.refused, run.used = s.sentIn, s.refused, s.used
	if closed := s.close(); err == nil && closed != nil {
		err = fmt.Errorf("tofu serve: %w, its log is %s", closed, filepath.Join(dir, "serve.log"))
	}
	if err != nil {
		return run, err
	}
	return run, run.traced(opts, env, project)
}

func writeFiles(project string, files map[string]string) error {
	for path, body := range files {
		full := filepath.Join(project, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (r *chainRun) traced(opts options, env []string, project string) error {
	var family struct {
		Generations []struct {
			Session string `json:"session"`
		} `json:"generations"`
	}
	if err := verb(opts, env, project, &family, "session", r.askedIn); err != nil {
		return err
	}
	for _, generation := range family.Generations {
		var trace host.SessionTrace
		if err := verb(opts, env, project, &trace, "session", "trace", generation.Session); err != nil {
			return err
		}
		r.generations = append(r.generations, generation.Session)
		r.traces = append(r.traces, trace)
	}
	return nil
}

func verb(opts options, env []string, project string, into any, args ...string) error {
	cmd := exec.Command(opts.tofu, append(args, "--json")...)
	cmd.Dir, cmd.Env = project, env
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("tofu %v: %w", args, err)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return fmt.Errorf("tofu %v printed no json: %w", args, err)
	}
	return json.Unmarshal(envelope.Data, into)
}
