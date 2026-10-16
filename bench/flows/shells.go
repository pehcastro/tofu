package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	keptShells      = 10000
	seedWriters     = 8
	memoryCeilingMB = 300
	quitWithin      = 3 * time.Second
	memoryEvery     = 500 * time.Millisecond
	shellsAsked     = "the orchestrator request 2"
)

func keptShellsQuit(s session) (string, error) {
	seeded := time.Now()
	if err := seedShells(s); err != nil {
		return "", err
	}
	seedTook := time.Since(seeded).Round(time.Second)
	cmd, err := s.command("8-shells", "", 4*time.Minute, nil)
	if err != nil {
		return "", err
	}
	out := &heldOutput{marker: shellsAsked}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() { _ = s.keep("8-shells", out.String()) }()
	started, peak := time.Now(), 0.0
	var ended time.Time
	var driven error
	for ended.IsZero() {
		select {
		case driven = <-exited:
			ended = time.Now()
		case <-time.After(memoryEvery):
			if read, err := memoryMB(cmd.Process.Pid); err == nil {
				peak = max(peak, read)
			}
			if peak > memoryCeilingMB {
				_ = cmd.Process.Kill()
				<-exited
				return "", fmt.Errorf("%s memory reached %.0f MB after %s with %d kept shells, over %d MB", memoryKind, peak, time.Since(started).Round(time.Second), keptShells, memoryCeilingMB)
			}
		}
	}
	asked := out.markedAt()
	if driven != nil {
		return "", fmt.Errorf("8-shells: %s", failure(out.String(), driven))
	}
	lead, err := lastLead(out.String())
	if err != nil {
		return "", err
	}
	if err := carries(lead, "SHELLSREPORT"); err != nil {
		return "", err
	}
	quit := ended.Sub(asked).Round(100 * time.Millisecond)
	if asked.IsZero() || quit > quitWithin {
		return "", fmt.Errorf("the app took %s to quit after ctrl+c, over %s, or never reached it", quit, quitWithin)
	}
	return fmt.Sprintf("%d shells seeded in %s; %s memory peak %.0f MB over %s; %s carries SHELLSREPORT; quit %s after ctrl+c",
		keptShells, seedTook, memoryKind, peak, ended.Sub(started).Round(time.Second), lead.name(), quit), nil
}

func seedShells(s session) error {
	cmd := exec.Command(s.exe, "session", "list")
	cmd.Dir, cmd.Env = s.project, s.env()
	if out, err := cmd.CombinedOutput(); err != nil {
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
