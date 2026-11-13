package main

import (
	"bufio"
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	platformfs "tofu/internal/platform/fs"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/store/walls"
)

//go:embed script
var scripts embed.FS

const (
	noteBody     = "a mock note\n"
	answerWithin = 60 * time.Second
)

var verbs = [][]string{
	{"version"}, {"shells", "list"}, {"hooks"}, {"settings"}, {"boardy", "catchup"}, {"boardy", "list"},
	{"session", "list"}, {"projects"}, {"doctor"},
}

type scratch struct {
	exe, home, project, cassette, steps string
}

func fresh(exe, work string) (scratch, error) {
	s := scratch{exe: exe, home: filepath.Join(work, "home"), project: filepath.Join(work, "project")}
	for _, made := range []string{s.home, s.project} {
		if err := os.MkdirAll(made, 0o755); err != nil {
			return s, err
		}
	}
	s.cassette, s.steps = filepath.Join(work, "turn.jsonl"), filepath.Join(work, "frame.steps")
	for _, path := range []string{s.cassette, s.steps} {
		body, err := scripts.ReadFile("script/" + filepath.Base(path))
		if err != nil {
			return s, err
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return s, err
		}
	}
	if out, err := s.tofu("boardy", "init", "--key", "WALL").CombinedOutput(); err != nil {
		return s, fmt.Errorf("setup tofu boardy init: %v: %s", err, out)
	}
	fmt.Println("setup, not timed: tofu boardy init --key WALL, so the project state exists and boardy has a board")
	return s, nil
}

func (s scratch) env() []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		return name == "HOME" || name == "USERPROFILE" || name == "GOMAXPROCS" || strings.HasPrefix(name, "TOFU_") ||
			strings.HasPrefix(name, "CLAUDE") || strings.HasPrefix(name, "OPENROUTER") || strings.HasSuffix(name, "_API_KEY")
	})
	return append(env, "HOME="+s.home, "USERPROFILE="+s.home, "GOMAXPROCS="+childProcs)
}

func (s scratch) tofu(args ...string) *exec.Cmd {
	cmd := exec.Command(s.exe, args...)
	cmd.Dir, cmd.Env = s.project, s.env()
	return cmd
}

func (s scratch) measure(runs int) ([]measured, error) {
	since := time.Now()
	for range runs {
		if err := os.WriteFile(filepath.Join(s.project, "note.txt"), []byte(noteBody), 0o644); err != nil {
			return nil, err
		}
		for _, verb := range verbs {
			_ = s.tofu(verb...).Run()
		}
		drive := s.tofu("drive", s.steps, "--plain", "--fresh", "--dir", s.project, "--home", s.home, "--cassette", s.cassette, "--timeout", "30s")
		if out, err := drive.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("tofu drive first frame: %v: %s", err, out)
		}
		if err := s.serve(); err != nil {
			return nil, err
		}
	}
	return s.read(since)
}

func (s scratch) read(since time.Time) ([]measured, error) {
	states, err := filepath.Glob(filepath.Join(platformfs.StateDir(s.home), platformfs.ProjectsDirName, "*"))
	if err != nil || len(states) != 1 {
		return nil, fmt.Errorf("the fresh home holds %d project states, not one: %v %v", len(states), states, err)
	}
	reading, err := walls.ReadProject(platformproject.Places{State: states[0], Log: filepath.Join(states[0], "log")}, since)
	if err != nil {
		return nil, err
	}
	if reading.Bad+reading.Unread > 0 {
		return nil, fmt.Errorf("%d wall lines and %d files could not be read", reading.Bad, reading.Unread)
	}
	own := map[opName][]float64{}
	for _, wall := range reading.Walls {
		key := opName{string(wall.Op), wall.Name}
		own[key] = append(own[key], float64(wall.MS-*cmp.Or(wall.OutsideMS, new(int64))))
	}
	return rowsOf(own), nil
}

type wire struct {
	in    io.WriteCloser
	lines chan string
	next  int
}

func (s scratch) serve() error {
	cmd := s.tofu("serve", "--stdio", "--dir", s.project, "--cassette", s.cassette)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	w := &wire{in: in, lines: make(chan string, 1024)}
	go func() {
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 1<<20), 64<<20)
		for scanner.Scan() {
			w.lines <- scanner.Text()
		}
		close(w.lines)
	}()
	err = w.script()
	_ = in.Close()
	for range w.lines {
	}
	if waited := cmd.Wait(); err == nil {
		err = waited
	}
	_ = s.tofu("host", "stop", "--dir", s.project).Run()
	if err != nil {
		return fmt.Errorf("tofu serve: %w", err)
	}
	return nil
}

func (w *wire) script() error {
	if _, err := w.call("initialize", map[string]any{"client": "walls", "versions": []string{"tofu.host/1"}}, ""); err != nil {
		return err
	}
	opened, err := w.call("session.open", map[string]any{}, "")
	if err != nil {
		return err
	}
	var first struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(opened, &first); err != nil || first.Session == "" {
		return fmt.Errorf("session.open answered no session: %s", opened)
	}
	steps := []struct {
		method string
		params map[string]any
		until  string
	}{
		{"turn.send", map[string]any{"session": first.Session, "text": "read, glob, search and edit the note"}, "turn.completed"},
		{"session.compact", map[string]any{"session": first.Session, "focus": "the note"}, ""},
		{"session.open", map[string]any{}, ""},
		{"session.open", map[string]any{"session": first.Session}, ""},
	}
	for _, step := range steps {
		if _, err := w.call(step.method, step.params, step.until); err != nil {
			return err
		}
	}
	return nil
}

func (w *wire) call(method string, params map[string]any, until string) (json.RawMessage, error) {
	w.next++
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": w.next, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := w.in.Write(append(line, '\n')); err != nil {
		return nil, err
	}
	var result json.RawMessage
	answered, seen := false, until == ""
	deadline := time.After(answerWithin)
	for !answered || !seen {
		select {
		case got, open := <-w.lines:
			if !open {
				return nil, fmt.Errorf("%s: serve closed its output", method)
			}
			var message struct {
				ID     int             `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal([]byte(got), &message) != nil {
				continue
			}
			switch {
			case message.Method == "" && message.ID == w.next && message.Error != nil:
				return nil, fmt.Errorf("%s: %s", method, message.Error)
			case message.Method == "" && message.ID == w.next:
				result, answered = message.Result, true
			case message.Method == until:
				seen = true
			}
		case <-deadline:
			return nil, fmt.Errorf("%s: no answer within %s", method, answerWithin)
		}
	}
	return result, nil
}
