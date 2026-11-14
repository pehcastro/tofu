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
	goalFile     = "goal.done"
	answerWithin = 60 * time.Second
	pollEvery    = 50 * time.Millisecond
)

var verbs = [][]string{
	{"version"}, {"shells", "list"}, {"hooks"}, {"settings"}, {"boardy", "catchup"}, {"boardy", "list"},
	{"session", "list"}, {"projects"}, {"doctor"},
}

var (
	spawnedOps  = []walls.Op{walls.OpSpawn, walls.OpSpawnStep, walls.OpShell, walls.OpHook, walls.OpCron}
	siftedOps   = []walls.Op{walls.OpSift}
	expectedOps = slices.Concat(spawnedOps, siftedOps)
	laterOps    = []walls.Op{walls.OpStartup, walls.OpStartupStep}
)

type scratch struct {
	exe, home, project, cassette, steps string
}

type unixMilliWindow struct{ from, to int64 }

func fresh(exe, work, cassette, steps string) (scratch, error) {
	s := scratch{exe: exe, home: filepath.Join(work, "home"), project: filepath.Join(work, "project"),
		cassette: filepath.Join(work, cassette), steps: filepath.Join(work, steps)}
	for _, made := range []string{s.home, s.project} {
		if err := os.MkdirAll(made, 0o755); err != nil {
			return s, err
		}
	}
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
	fmt.Printf("setup, not timed, in %s: tofu boardy init --key WALL, so the project state exists and boardy has a board\n", work)
	return s, nil
}

func (s scratch) hooked() error {
	body, err := scripts.ReadFile("script/hook.json")
	if err != nil {
		return err
	}
	settings := filepath.Join(s.project, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(settings, body, 0o644); err != nil {
		return err
	}
	if out, err := s.tofu("hooks", "trust").CombinedOutput(); err != nil {
		return fmt.Errorf("setup tofu hooks trust: %v: %s", err, out)
	}
	fmt.Println("setup, not timed: a project PostToolUse hook on bash running exit 0 in .claude/settings.json, then tofu hooks trust")
	return nil
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

func (s scratch) drive(arm ...string) error {
	args := append([]string{"drive", s.steps, "--plain", "--fresh", "--dir", s.project, "--home", s.home, "--cassette", s.cassette, "--timeout", "30s"}, arm...)
	if out, err := s.tofu(args...).CombinedOutput(); err != nil {
		return fmt.Errorf("tofu drive %s: %v: %s", s.steps, err, out)
	}
	return nil
}

func measure(first, more, sifted scratch, runs int) ([]measured, error, error) {
	since := time.Now()
	var later []unixMilliWindow
	var stopped error
	for run := 0; run < runs && stopped == nil; run++ {
		if err := os.WriteFile(filepath.Join(first.project, "note.txt"), []byte(noteBody), 0o644); err != nil {
			return nil, nil, err
		}
		for _, verb := range verbs {
			_ = first.tofu(verb...).Run()
		}
		stopped = cmp.Or(first.drive(), first.serve(func(w *wire) error { return lead(w, &later) }), more.serve(more.spawnShellCron), sifted.drive("--sift", "free"))
		if stopped != nil {
			stopped = fmt.Errorf("run %d of %d: %w", run+1, runs, stopped)
		}
	}
	own, err := first.read(since, func(wall walls.Wall) bool {
		return slices.Contains(laterOps, wall.Op) || !slices.ContainsFunc(later, func(w unixMilliWindow) bool {
			return wall.At >= w.from && wall.At < w.to
		})
	})
	for _, extra := range []struct {
		home scratch
		ops  []walls.Op
	}{{more, spawnedOps}, {sifted, siftedOps}} {
		read, readErr := extra.home.read(since, func(wall walls.Wall) bool { return slices.Contains(extra.ops, wall.Op) })
		for key, ms := range read {
			kind := opName{key.op, strings.TrimRight(key.name, "0123456789-")}
			own[kind] = append(own[kind], ms...)
		}
		err = cmp.Or(err, readErr)
	}
	return rowsOf(own), stopped, err
}

func (s scratch) walls(since time.Time) ([]walls.Wall, error) {
	states, err := filepath.Glob(filepath.Join(platformfs.StateDir(s.home), platformfs.ProjectsDirName, "*"))
	if err != nil || len(states) != 1 {
		return nil, fmt.Errorf("the fresh home holds %d project states, not one: %v %v", len(states), states, err)
	}
	reading, err := walls.ReadProject(platformproject.Places{State: states[0], Log: filepath.Join(states[0], "log")}, since)
	if err == nil && reading.Bad+reading.Unread > 0 {
		err = fmt.Errorf("%d wall lines and %d files could not be read", reading.Bad, reading.Unread)
	}
	return reading.Walls, err
}

func (s scratch) read(since time.Time, kept func(walls.Wall) bool) (map[opName][]float64, error) {
	read, err := s.walls(since)
	own := map[opName][]float64{}
	for _, wall := range slices.DeleteFunc(read, func(wall walls.Wall) bool { return !kept(wall) }) {
		key := opName{string(wall.Op), wall.Name}
		own[key] = append(own[key], float64(wall.MS-*cmp.Or(wall.OutsideMS, new(int64))))
	}
	return own, err
}

func (s scratch) await(since time.Time, ops ...walls.Op) error {
	for deadline := time.Now().Add(answerWithin); ; time.Sleep(pollEvery) {
		read, err := s.walls(since)
		if err != nil {
			return err
		}
		missing := slices.DeleteFunc(slices.Clone(ops), func(op walls.Op) bool {
			return slices.ContainsFunc(read, func(wall walls.Wall) bool { return wall.Op == op })
		})
		if len(missing) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no %v wall within %s", missing, answerWithin)
		}
	}
}

type wire struct {
	in    io.WriteCloser
	lines chan string
	next  int
}

func (s scratch) serve(script func(*wire) error) error {
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
	err = script(w)
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

func (w *wire) open() (string, error) {
	if _, err := w.call("initialize", map[string]any{"client": "walls", "versions": []string{"tofu.host/1"}}, ""); err != nil {
		return "", err
	}
	opened, err := w.call("session.open", map[string]any{}, "")
	if err != nil {
		return "", err
	}
	var first struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(opened, &first); err != nil || first.Session == "" {
		return "", fmt.Errorf("session.open answered no session: %s", opened)
	}
	return first.Session, nil
}

func lead(w *wire, later *[]unixMilliWindow) error {
	id, err := w.open()
	if err != nil {
		return err
	}
	steps := []struct {
		method    string
		params    map[string]any
		until     string
		laterTurn bool
	}{
		{"turn.send", map[string]any{"session": id, "text": "read, glob, search and edit the note"}, "turn.completed", false},
		{"turn.send", map[string]any{"session": id, "text": "a second turn in the same session"}, "turn.completed", true},
		{"session.compact", map[string]any{"session": id, "focus": "the note"}, "", false},
		{"session.open", map[string]any{}, "", false},
		{"session.open", map[string]any{"session": id}, "", false},
	}
	for _, step := range steps {
		ran := unixMilliWindow{from: time.Now().UnixMilli()}
		if _, err := w.call(step.method, step.params, step.until); err != nil {
			return err
		}
		if ran.to = time.Now().UnixMilli(); step.laterTurn {
			*later = append(*later, ran)
		}
	}
	return nil
}

func (s scratch) spawnShellCron(w *wire) error {
	began := time.Now()
	if err := os.Remove(filepath.Join(s.project, goalFile)); err != nil && !os.IsNotExist(err) {
		return err
	}
	id, err := w.open()
	if err != nil {
		return err
	}
	if _, err := w.call("turn.send", map[string]any{"session": id, "text": "run a shell and hand it to a worker"}, "tool.started"); err != nil {
		return err
	}
	if _, err := w.call("turn.send", map[string]any{"session": id, "text": "typed while the shell runs, so it moves to the background"}, ""); err != nil {
		return err
	}
	if err := s.await(began, walls.OpSpawn, walls.OpShell, walls.OpHook); err != nil {
		return err
	}
	if _, err := w.call("cron.command", map[string]any{"line": `/goal close the goal --until "cat ` + goalFile + `"`}, ""); err != nil {
		return err
	}
	if err := w.fired(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.project, goalFile), []byte("done\n"), 0o644); err != nil {
		return err
	}
	return s.await(began, walls.OpCron)
}

func (w *wire) fired() error {
	for deadline := time.Now().Add(answerWithin); ; time.Sleep(pollEvery) {
		got, err := w.call("query.cron", map[string]any{}, "")
		if err != nil {
			return err
		}
		var state struct {
			Jobs []struct {
				Fires int `json:"fires"`
			} `json:"jobs"`
		}
		if err := json.Unmarshal(got, &state); err != nil {
			return fmt.Errorf("query.cron: %w: %s", err, got)
		}
		for _, job := range state.Jobs {
			if job.Fires > 0 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the goal did not fire within %s: %s", answerWithin, got)
		}
	}
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
