package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

//go:embed script
var scripts embed.FS

const (
	screenWidth  = "200"
	screenHeight = "50"
	pollEvery    = 200 * time.Millisecond
)

var requestHeader = regexp.MustCompile(`^(the orchestrator|sub-agent c\d+) request \d+: `)

type session struct {
	exe, dir, home, project string
}

type request struct {
	header   string
	messages []string
}

type heldOutput struct {
	mu     sync.Mutex
	held   bytes.Buffer
	marker string
	marked time.Time
}

func (h *heldOutput) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	written, err := h.held.Write(p)
	if h.marker != "" && h.marked.IsZero() && bytes.Contains(h.held.Bytes(), []byte(h.marker)) {
		h.marked = time.Now()
	}
	return written, err
}

func (h *heldOutput) markedAt() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.marked
}

func (h *heldOutput) String() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.held.String()
}

func fresh(exe, dir string) (session, error) {
	s := session{exe: exe, dir: dir, home: filepath.Join(dir, "home"), project: filepath.Join(dir, "project")}
	for _, made := range []string{s.home, s.project} {
		if err := os.MkdirAll(made, 0o755); err != nil {
			return session{}, err
		}
	}
	return s, os.WriteFile(filepath.Join(s.project, "note.txt"), []byte("a mock note\n"), 0o644)
}

func (s session) env() []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		return name == "HOME" || name == "USERPROFILE" || name == "GOMAXPROCS" || strings.HasPrefix(name, "TOFU_") ||
			strings.HasPrefix(name, "CLAUDE") || strings.HasPrefix(name, "OPENROUTER") || strings.HasSuffix(name, "_API_KEY")
	})
	return append(env, "HOME="+s.home, "USERPROFILE="+s.home, "GOMAXPROCS="+childProcs, "TOFU_DRIVE_REQUESTS="+s.requests())
}

func (s session) requests() string { return filepath.Join(s.dir, "requests.txt") }

func (s session) tofu(args ...string) *exec.Cmd {
	cmd := exec.Command(s.exe, args...)
	cmd.Dir, cmd.Env = s.project, s.env()
	return cmd
}

func (s session) hostPID() int {
	var status struct {
		PID int `json:"pid"`
	}
	out, err := s.tofu("host", "status", "--json", "--dir", s.project).Output()
	if err != nil || json.Unmarshal(out, &status) != nil {
		return 0
	}
	return status.PID
}

func (s session) stopHost() { _ = s.tofu("host", "stop", "--dir", s.project).Run() }

func (s session) setting(name, value string) error {
	if out, err := s.tofu("settings", "set", name, value).CombinedOutput(); err != nil {
		return fmt.Errorf("tofu settings set %s %s: %v: %s", name, value, err, out)
	}
	return nil
}

func (s session) command(script, cassette string, timeout time.Duration, extra []string) (*exec.Cmd, error) {
	steps, err := s.unpack(script + ".steps")
	if err != nil {
		return nil, err
	}
	if cassette == "" {
		if cassette, err = s.unpack(script + ".jsonl"); err != nil {
			return nil, err
		}
	}
	args := append([]string{"drive", steps, "--plain", "--dir", s.project, "--home", s.home, "--cassette", cassette,
		"--timeout", timeout.String(), "--width", screenWidth, "--height", screenHeight}, extra...)
	return s.tofu(args...), nil
}

func (s session) unpack(name string) (string, error) {
	body, err := scripts.ReadFile("script/" + name)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.dir, name)
	return path, os.WriteFile(path, body, 0o644)
}

func (s session) drive(script, cassette string, timeout time.Duration, extra ...string) (string, error) {
	cmd, err := s.command(script, cassette, timeout, extra)
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	if kept := s.keep(script, string(out)); kept != nil {
		return "", kept
	}
	if err != nil {
		return string(out), fmt.Errorf("%s: %s", script, failure(string(out), err))
	}
	return string(out), nil
}

func (s session) keep(script, out string) error {
	return os.WriteFile(filepath.Join(s.dir, script+".out"), []byte(out), 0o644)
}

func (s session) driveUntilKilled(script, sentinel string, within time.Duration) (string, error) {
	cmd, err := s.command(script, "", within, nil)
	if err != nil {
		return "", err
	}
	out := &heldOutput{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() { _ = s.keep(script, out.String()) }()
	deadline := time.After(within)
	var missed error
	for missed == nil && !strings.Contains(out.String(), sentinel) {
		select {
		case err := <-exited:
			return out.String(), fmt.Errorf("%s: ended before %q: %s", script, sentinel, failure(out.String(), err))
		case <-deadline:
			missed = fmt.Errorf("%s: %q never appeared within %s", script, sentinel, within)
		case <-time.After(pollEvery):
		}
	}
	_ = cmd.Process.Kill()
	<-exited
	return out.String(), missed
}

func failure(out string, err error) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "tofu drive:") || strings.HasPrefix(line, "panic:") || strings.HasPrefix(line, "fatal error:") {
			return strings.TrimSpace(line)
		}
	}
	return fmt.Sprintf("exited with %v and printed no error line", err)
}

func leadRequests(out string) []request {
	var sent []request
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case requestHeader.MatchString(line):
			sent = append(sent, request{header: line})
		case len(sent) == 0:
		case strings.HasPrefix(line, "  message "):
			sent[len(sent)-1].messages = append(sent[len(sent)-1].messages, line)
		case strings.HasPrefix(line, "    ") && len(sent[len(sent)-1].messages) > 0:
			last := &sent[len(sent)-1].messages[len(sent[len(sent)-1].messages)-1]
			*last += "\n" + line
		}
	}
	return slices.DeleteFunc(sent, func(r request) bool { return !strings.HasPrefix(r.header, "the orchestrator") })
}

func (r request) text() string { return strings.Join(r.messages, "\n") }

func (r request) last() string {
	if len(r.messages) == 0 {
		return ""
	}
	return r.messages[len(r.messages)-1]
}

func (r request) sinceLastReply() string {
	from := len(r.messages)
	for from > 0 && !strings.Contains(strings.SplitN(r.messages[from-1], "\n", 2)[0], " assistant: ") {
		from--
	}
	return strings.Join(r.messages[from:], "\n")
}

func (r request) name() string { return strings.SplitN(r.header, ":", 2)[0] }

func lineWith(out, text string) string {
	for _, line := range strings.Split(out, "\n") {
		if at := strings.Index(line, text); at >= 0 {
			start := 0
			if bar := strings.LastIndex(line[:at], "│"); bar >= 0 {
				start = bar + len("│")
			}
			end := strings.Index(line[at:], "│")
			if end < 0 {
				end = len(line) - at
			}
			return strings.TrimSpace(line[start : at+end])
		}
	}
	return ""
}
