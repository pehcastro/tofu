package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/txtar"
)

const (
	claudeModel     = "opus"
	claudeEffort    = "medium"
	claudeWallClock = 20 * time.Minute
	checkWallClock  = 5 * time.Minute
)

type plannedRun struct {
	principle string
	rule      string
	task      task
	arm       arm
	record    string
}

func plan(sets []principleSet, runsDir string) []plannedRun {
	var runs []plannedRun
	for _, set := range sets {
		for _, t := range set.tasks {
			if t.leaked != "" {
				continue
			}
			for _, a := range []arm{armOn, armOff} {
				record := filepath.Join(runsDir, set.name, string(a), t.name+".json")
				runs = append(runs, plannedRun{principle: set.name, rule: set.rule, task: t, arm: a, record: record})
			}
		}
	}
	return runs
}

func (r plannedRun) prompt() string {
	if r.arm == armOn {
		return r.rule + "\n\n" + r.task.prompt
	}
	return r.task.prompt
}

func claudeArgs(prompt string) []string {
	return []string{"-p", prompt, "--model", claudeModel, "--effort", claudeEffort, "--output-format", "json", "--permission-mode", "acceptEdits", "--allowedTools", "Read", "Glob", "Grep", "Edit", "Write", "Bash(go:*)"}
}

type record struct {
	Principle    string          `json:"principle"`
	Arm          arm             `json:"arm"`
	Task         string          `json:"task"`
	Passed       bool            `json:"passed"`
	DiffLines    int             `json:"diff_lines"`
	Models       []string        `json:"models_reported"`
	CLIVersion   string          `json:"cli_version"`
	Credential   string          `json:"credential_kind"`
	Wire         string          `json:"wire"`
	Machine      string          `json:"machine"`
	Started      time.Time       `json:"started"`
	WallMS       int64           `json:"wall_ms"`
	ClaudeError  string          `json:"claude_error,omitempty"`
	ClaudeStderr string          `json:"claude_stderr,omitempty"`
	ClaudeOutput json.RawMessage `json:"claude_output,omitempty"`
	ClaudeRaw    string          `json:"claude_raw,omitempty"`
	TestOutput   string          `json:"test_output"`
}

func execute(r plannedRun, cliVersion, machine string) (record, error) {
	dir, err := os.MkdirTemp("", "tofu-principle-")
	if err != nil {
		return record{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := writeFiles(dir, r.task.seed, ""); err != nil {
		return record{}, err
	}
	seed, err := commitSeed(dir)
	if err != nil {
		return record{}, err
	}

	rec := record{Principle: r.principle, Arm: r.arm, Task: r.task.name, CLIVersion: cliVersion, Credential: "subscription", Wire: "claude -p", Machine: machine, Started: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), claudeWallClock)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", claudeArgs(r.prompt())...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		rec.ClaudeError = err.Error()
	}
	rec.WallMS = time.Since(rec.Started).Milliseconds()
	rec.ClaudeStderr = stderr.String()
	rec.Models = reportedModels(stdout.Bytes())
	if json.Valid(stdout.Bytes()) {
		rec.ClaudeOutput = stdout.Bytes()
	} else {
		rec.ClaudeRaw = stdout.String()
	}

	if rec.DiffLines, err = diffLines(dir, seed); err != nil {
		return record{}, err
	}
	if err := writeFiles(dir, r.task.check, checkPrefix); err != nil {
		return record{}, err
	}
	rec.Passed, rec.TestOutput = goTest(dir)
	return rec, nil
}

func writeFiles(dir string, files []txtar.File, prefix string) error {
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(f.Name, prefix)))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, f.Data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=bench", "-c", "user.email=bench@localhost"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func commitSeed(dir string) (string, error) {
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-qm", "seed"}} {
		if _, err := git(dir, args...); err != nil {
			return "", err
		}
	}
	return git(dir, "rev-parse", "HEAD")
}

func diffLines(dir, seed string) (int, error) {
	if _, err := git(dir, "add", "-A"); err != nil {
		return 0, err
	}
	numstat, err := git(dir, "diff", "--cached", "--numstat", seed)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, line := range strings.Split(numstat, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		added, _ := strconv.Atoi(fields[0])
		deleted, _ := strconv.Atoi(fields[1])
		total += added + deleted
	}
	return total, nil
}

func goTest(dir string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), checkWallClock)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

func reportedModels(output []byte) []string {
	var parsed struct {
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	if json.Unmarshal(output, &parsed) != nil {
		return nil
	}
	return slices.Sorted(maps.Keys(parsed.ModelUsage))
}

func readRecord(path string) (record, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return record{}, false, nil
	}
	if err != nil {
		return record{}, false, err
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil {
		return record{}, false, fmt.Errorf("%s is a recorded run and it does not parse: %w", path, err)
	}
	return rec, true, nil
}

func writeRecord(path string, rec record) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
