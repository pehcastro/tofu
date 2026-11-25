package jobs

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/cli/render"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/shell/bash/sift"
	"tofu/internal/tools/shell/process"
)

type Shells struct {
	Seen *Seen
}

type Seen struct {
	mu     sync.Mutex
	shells map[seenShell]*seenState
}

type seenState struct {
	mu      sync.Mutex
	logs    process.Cursor
	watched process.Watcher
	waited  time.Time
	idle    bool
}

type seenShell struct {
	name    string
	started int64
}

func (Shells) Name() string { return loop.ShellToolName }

func (Shells) Definition() core.Tool {
	return core.Tool{
		Name: loop.ShellToolName,
		Description: fmt.Sprintf("controls a process bash kept running, by the name that call returned, such as bash-1: a background: true start, or a command bash moved to a background shell because it was still running. "+
			"wait returns as soon as it ends, as soon as its last lines hold the until text, as soon as it or a file it writes to prints a failure line, as soon as it sits %s with no output and no CPU, or after %d s, with whether it is still running, the CPU its process tree used, the files it writes to, its exit code and its last lines: one wait covers a long build, never sleep in bash. "+
			"wait with for returns as soon as the thing is ready, as JSON with met, after_ms and the matched line, or met false and its last lines: for is a regex over every line it printed, such as listening on, or port:3000, file:dist/app.js, or an http:// address that must answer 200. "+
			"logs folds what it printed into templates with counts, and the end of a log file its command names, such as -Log build.log or > build.log; a later logs prints only new_since_last_call. "+
			"stop kills its whole process tree, children included, so the port it held is free when this returns. "+
			"restart stops it and starts the same command in the same folder under the same name. never stop a server with kill, taskkill or pkill. "+
			"ports needs no name and says, one JSON line per port, who listens on port, or on every port when port is left out: pid, command, the shell holding it and started_by this_session, other_session or outside, or free; it never kills", process.QuietBeforeIdle, konst.ShellWaitMillis/1000),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"op":         map[string]any{"type": "string", "enum": []string{"wait", "logs", "stop", "restart", "ports"}},
				"name":       map[string]any{"type": "string", "description": "the shell's name, as bash returned it; every op but ports needs it"},
				"port":       map[string]any{"type": "integer", "description": "only for ports: the one port to ask about"},
				"until":      map[string]any{"type": "string", "description": "only for wait: text the command prints once the thing waited for is ready, such as Listening on, so a server that never exits ends the wait"},
				"for":        map[string]any{"type": "string", "description": "only for wait: a regex over its lines, port:N, file:path, or an http:// or https:// address answering 200"},
				"timeout_ms": map[string]any{"type": "integer", "description": fmt.Sprintf("only for wait with for: how long to wait, at most %d", konst.ShellWaitMillis)},
			},
			"required": []string{"op"},
		},
	}
}

type shellArgs struct {
	Op        string `json:"op"`
	Name      string `json:"name"`
	Until     string `json:"until"`
	For       string `json:"for"`
	TimeoutMS int64  `json:"timeout_ms"`
	Port      int    `json:"port"`
}

func (s Shells) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args shellArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("shell: arguments are not the expected shape: %w", err)
	}
	registry := loop.ShellRegistryFrom(ctx)
	if registry == nil {
		return contract.Result{}, errors.New("shell: no shell registry is attached to this turn")
	}
	command := args.Op + " " + args.Name
	content, exitCode, err := s.runOp(ctx, registry, args)
	var held process.HeldError
	switch {
	case errors.As(err, &held):
		return contract.Result{Content: held.Error(), Command: command}, nil
	case err != nil:
		return contract.Result{}, fmt.Errorf("shell: %w", err)
	}
	return contract.Result{Content: content, Command: command, ExitCode: exitCode}, nil
}

func (s Shells) runOp(ctx context.Context, registry *process.Registry, args shellArgs) (string, *int, error) {
	switch args.Op {
	case "ports":
		held, err := ports(ctx, registry, args.Port)
		return held, nil, err
	case "stop":
		return args.Name + " stopped, with every process it started", nil, registry.AgentKill(args.Name)
	case "restart":
		started, err := registry.AgentRestart(args.Name)
		return fmt.Sprintf("%s restarted as pid %d in %s: %s. check it with bash check_port once it should be up", started.Name, started.PID, started.Dir, started.Command), nil, err
	case "logs":
		entry, err := registry.AgentRead(args.Name)
		if err != nil {
			return "", nil, err
		}
		state, first := s.Seen.of(entry)
		state.mu.Lock()
		text, err := registry.Follow(entry.Name, &state.logs)
		state.mu.Unlock()
		if err != nil {
			return "", nil, err
		}
		now := time.Now()
		head := fmt.Sprintf("%s is %s, %s.", args.Name, entry.State, registry.Timing(entry, now).Words(now))
		switch {
		case first:
			return head + " everything it printed, folded:\n" + folded(text), nil, nil
		case strings.TrimSpace(text) == "":
			return head + " new_since_last_call: nothing", nil, nil
		}
		return head + " new_since_last_call:\n" + folded(text), nil, nil
	case "wait":
		entry, err := registry.AgentRead(args.Name)
		if err != nil {
			return "", nil, err
		}
		if args.For != "" {
			met, err := parseFor(registry, entry, args.For)
			if err != nil {
				return "", nil, err
			}
			within := konst.ShellWaitMillis * time.Millisecond
			if args.TimeoutMS > 0 {
				within = min(within, time.Duration(args.TimeoutMS)*time.Millisecond)
			}
			return awaitFor(ctx, registry, entry, met, within)
		}
		if loop.ExitComesBack(ctx, args.Name) {
			return args.Name + " is your own command, moved to the background, and its exit comes back to you as a message when it ends, so this does not wait: answer the person, go on with other work, or end your turn", nil, nil
		}
		state, _ := s.Seen.of(entry)
		state.mu.Lock()
		defer state.mu.Unlock()
		return waitOn(ctx, registry, entry, args.Until, state)
	}
	return "", nil, fmt.Errorf("op %q is not wait, logs, stop, restart or ports", args.Op)
}

func (s *Seen) of(entry process.Shell) (*seenState, bool) {
	if s == nil {
		return &seenState{}, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := seenShell{name: entry.Name, started: entry.Started.UnixNano()}
	state, seen := s.shells[key]
	if !seen {
		if s.shells == nil {
			s.shells = map[seenShell]*seenState{}
		}
		state = &seenState{}
		s.shells[key] = state
	}
	return state, !seen
}

func waitOn(ctx context.Context, registry *process.Registry, entry process.Shell, until string, state *seenState) (string, *int, error) {
	deadline := time.Now().Add(konst.ShellWaitMillis * time.Millisecond)
	cpuFrom, err := entry.TreeCPU()
	use, cpuAt, ended, tail := treeUse{err: err}, time.Now(), entry, ""
	var news process.News
	for {
		if ended, err = registry.AwaitEnd(ctx, entry.Name, min(time.Until(deadline), konst.ShellWaitMatchMillis*time.Millisecond)); err != nil {
			return "", nil, err
		}
		if ended.HeldByPerson() {
			return entry.Name + " was taken over by the person; no longer waiting", nil, nil
		}
		got, err := registry.Watch(entry.Name, &state.watched)
		if err != nil {
			return "", nil, err
		}
		news.Lines, news.Trouble = news.Lines+got.Lines, append(news.Trouble, got.Trouble...)
		if until != "" {
			if tail, err = registry.Tail(entry.Name, process.DefaultTail); err != nil {
				return "", nil, err
			}
		}
		now := time.Now()
		if use.err == nil && now.Sub(cpuAt) >= process.QuietBeforeIdle {
			used, err := ended.TreeCPU()
			use, cpuFrom, cpuAt = treeUse{used: used - cpuFrom, over: now.Sub(cpuAt), whole: true, err: err}, used, now
		}
		if use.idle(quietFor(registry, ended, now)) || len(news.Trouble) > 0 || until != "" && strings.Contains(tail, until) ||
			ended.State != process.Running || ctx.Err() != nil || !now.Before(deadline) {
			break
		}
	}
	if tail, err = registry.Tail(entry.Name, process.DefaultTail); err != nil {
		return "", nil, err
	}
	now := time.Now()
	timing := registry.Timing(ended, now).Words(now)
	said := []string{entry.Name + " is still running, " + timing + "."}
	switch words := endWords(ended); {
	case words != "":
		said[0] = entry.Name + " " + words + ", " + timing + "."
	case until != "" && strings.Contains(tail, until):
		said[0] = fmt.Sprintf("%s printed %q and is still running, %s.", entry.Name, until, timing)
	default:
		if !use.whole && use.err == nil {
			used, err := ended.TreeCPU()
			use = treeUse{used: used - cpuFrom, over: now.Sub(cpuAt), err: err}
		}
		said = append(said, treeWords(entry.Name, use, quietFor(registry, ended, now), news, state, now)...)
	}
	if len(news.Trouble) > 0 {
		registry.Tell(entry.Name)
		said = append(said, "it printed failure lines:\n"+strings.Join(news.Trouble, "\n"))
	}
	state.waited = now
	return strings.Join(slices.Concat(said, ended.Redirects(now), []string{"its last lines:\n" + tail}), "\n"), ended.ExitCode, nil
}

type treeUse struct {
	used, over time.Duration
	whole      bool
	err        error
}

func (u treeUse) idle(quiet time.Duration) bool {
	return u.whole && u.err == nil && u.used < process.IdleCPU && quiet >= process.QuietBeforeIdle
}

func quietFor(registry *process.Registry, entry process.Shell, now time.Time) time.Duration {
	return now.Sub(cmp.Or(registry.Timing(entry, now).Last, entry.Started))
}

func treeWords(name string, use treeUse, quiet time.Duration, news process.News, state *seenState, now time.Time) []string {
	printed := fmt.Sprintf("%d lines printed since it started.", news.Lines)
	if !state.waited.IsZero() {
		printed = fmt.Sprintf("%d lines printed since your last wait %s ago.", news.Lines, render.Until(now.Sub(state.waited)))
	}
	if use.err != nil {
		state.idle = false
		return []string{printed, "tofu could not read the CPU its process tree used: " + use.err.Error()}
	}
	cpu := fmt.Sprintf("its process tree used %.1f s of CPU in the last %s", use.used.Seconds(), render.Until(use.over))
	stuck := " if it should be working it is stuck: stop it with shell stop " + name + " and run the step on its own, without a pipe or redirect, to see where it waits."
	wasIdle := state.idle
	state.idle = use.idle(quiet)
	switch {
	case state.idle && wasIdle && news.Lines == 0:
		return []string{printed, cpu + " and printed nothing, the same answer as your last wait: another wait answers the same again." + stuck}
	case state.idle:
		return []string{printed, cpu + " and printed nothing: it is waiting on something, not working." + stuck}
	case use.whole:
		return []string{printed, cpu + ": it is working."}
	}
	return []string{printed, cpu + "."}
}

func folded(text string) string {
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
	if len(lines) > konst.ShellDrainLinesFloor {
		lines = templates(lines)
	}
	if hidden := len(lines) - process.DefaultTail; hidden > 0 {
		return fmt.Sprintf("[%d earlier lines not shown]\n", hidden) + strings.Join(lines[hidden:], "\n")
	}
	return strings.Join(lines, "\n")
}

func templates(lines []string) []string {
	digits := regexp.MustCompile(`[0-9]+`)
	unmasked, masked := map[string]string{}, make([]string, len(lines))
	for i, line := range lines {
		masked[i] = digits.ReplaceAllString(line, "<n>")
		if key := strings.TrimRight(masked[i], " \t"); unmasked[key] == "" {
			unmasked[key] = strings.TrimRight(line, " \t")
		}
	}
	joined := strings.Join(masked, "\n")
	drained := sift.Drain(joined, konst.ShellDrainSimilarityPercent, konst.ShellDrainTemplatesCap)
	if drained == joined {
		return lines
	}
	folded := strings.Split(strings.TrimRight(drained, "\n"), "\n")
	for i, line := range folded {
		folded[i] = cmp.Or(unmasked[line], line)
	}
	return folded
}
