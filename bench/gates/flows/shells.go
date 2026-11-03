package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	keptShells      = 10000
	seedWriters     = 8
	memoryCeilingMB = 300
	quitWithin      = 3 * time.Second
	memoryEvery     = 500 * time.Millisecond
	quitAsked       = "no turn has sent a request yet"
)

func keptShellsQuit(s session) (string, error) {
	seeded := time.Now()
	if err := seedShells(s); err != nil {
		return "", err
	}
	seedTook := time.Since(seeded).Round(time.Second)
	cmd, err := s.command("8-shells", "", 4*time.Minute, []string{"--host"})
	if err != nil {
		return "", err
	}
	out := &heldOutput{marker: quitAsked}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() { _ = s.keep("8-shells", out.String()) }()
	started, hostPID := time.Now(), 0
	var peak, hostPeak, memberPeak float64
	var ended time.Time
	var driven error
	for ended.IsZero() {
		select {
		case driven = <-exited:
			ended = time.Now()
		case <-time.After(memoryEvery):
			if hostPID == 0 {
				hostPID = s.hostPID()
			}
			member, _ := memoryMB(cmd.Process.Pid)
			hosted, _ := memoryMB(hostPID)
			peak, hostPeak, memberPeak = max(peak, hosted+member), max(hostPeak, hosted), max(memberPeak, member)
			if peak > memoryCeilingMB {
				_ = cmd.Process.Kill()
				<-exited
				return "", fmt.Errorf("host plus member %s memory reached %.0f MB after %s with %d kept shells, over %d MB", memoryKind, peak, time.Since(started).Round(time.Second), keptShells, memoryCeilingMB)
			}
		}
	}
	asked := out.markedAt()
	if driven != nil {
		return "", fmt.Errorf("8-shells: %s", failure(out.String(), driven))
	}
	if hostPID == 0 {
		return "", fmt.Errorf("no tofu host answered while 8-shells ran under --host, so only the member was measured")
	}
	lead, err := s.lastLeadRequest()
	if err != nil {
		return "", err
	}
	if shown, err := s.tofu("session", "request", lead.session, lead.id, "--json").CombinedOutput(); err != nil || !strings.Contains(string(shown), "SHELLSREPORT") {
		return "", fmt.Errorf("tofu session request %s %s does not carry SHELLSREPORT: %v", lead.session, lead.id, err)
	}
	quit := ended.Sub(asked).Round(100 * time.Millisecond)
	if asked.IsZero() || quit > quitWithin {
		return "", fmt.Errorf("the app took %s to quit after ctrl+c, over %s, or never reached it", quit, quitWithin)
	}
	return fmt.Sprintf("%d shells seeded in %s; host pid %d plus member %s memory peak %.0f MB (host peak %.0f, member peak %.0f) over %s; the lead's last request %s carries SHELLSREPORT; quit %s after ctrl+c",
		keptShells, seedTook, hostPID, memoryKind, peak, hostPeak, memberPeak, ended.Sub(started).Round(time.Second), lead.id, quit), nil
}

type storedRequest struct {
	session, id string
}

func (s session) lastLeadRequest() (storedRequest, error) {
	logs, err := filepath.Glob(filepath.Join(s.home, ".tofu", "projects", "*", "sessions", "*", "requests.jsonl"))
	if err != nil || len(logs) != 1 {
		return storedRequest{}, fmt.Errorf("%d sessions stored requests, want 1: %v", len(logs), err)
	}
	body, err := os.ReadFile(logs[0])
	if err != nil {
		return storedRequest{}, err
	}
	last := storedRequest{session: filepath.Base(filepath.Dir(logs[0]))}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var sent struct{ Request, Agent string }
		if json.Unmarshal([]byte(line), &sent) == nil && sent.Agent == "" {
			last.id = sent.Request
		}
	}
	if last.id == "" {
		return storedRequest{}, fmt.Errorf("the session %s stored no request from the lead", last.session)
	}
	return last, nil
}

func seedShells(s session) error {
	if out, err := s.tofu("session", "list").CombinedOutput(); err != nil {
		return fmt.Errorf("tofu session list: %v: %s", err, out)
	}
	projects, err := filepath.Glob(filepath.Join(s.home, ".tofu", "projects", "*"))
	if err != nil || len(projects) != 1 {
		return fmt.Errorf("tofu session list left %d project folders in the fresh home, want 1: %v", len(projects), err)
	}
	dir := filepath.Join(projects[0], "shells")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ended := time.Now().UTC().Format(time.RFC3339)
	failed := make([]error, seedWriters)
	var writing sync.WaitGroup
	for writer := range seedWriters {
		writing.Go(func() {
			for n := writer + 1; n <= keptShells && failed[writer] == nil; n += seedWriters {
				failed[writer] = writeShell(dir, s.project, ended, n)
			}
		})
	}
	writing.Wait()
	return errors.Join(failed...)
}

func writeShell(dir, project, ended string, n int) error {
	scratch := fmt.Sprintf("/mock/scratchpad/sessions/mock-session-%d", n)
	shell, err := json.Marshal(map[string]any{
		"name": fmt.Sprintf("bash-%d", n), "command": fmt.Sprintf("go test ./internal/pkg%d/...", n), "dir": project, "owner": "lead",
		"pid": 900000 + n, "state": "exited", "started": ended, "ended": ended, "exit_code": 0, "kept": "background",
		"env": []string{"TMPDIR=" + scratch + "/tmp", "GOTMPDIR=" + scratch + "/tmp", "CARGO_TARGET_DIR=" + scratch + "/cache/cargo",
			"PYTHONPYCACHEPREFIX=" + scratch + "/cache/pycache", "RUFF_CACHE_DIR=" + scratch + "/cache/ruff", "MYPY_CACHE_DIR=" + scratch + "/cache/mypy",
			"TMP=" + scratch + "/tmp", "TEMP=" + scratch + "/tmp", "PATH=/mock/git/cmd:/mock/go/bin:/mock/system32:/mock/home/.local/bin:/mock/home/go/bin:/mock/nodejs"},
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fmt.Sprintf("bash-%d.json", n)), shell, 0o644)
}
