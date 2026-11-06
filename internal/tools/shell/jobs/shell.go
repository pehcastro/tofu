package jobs

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"tofu/internal/agent/loop"
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
	mu      sync.Mutex
	cursors map[seenShell]*process.Cursor
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
			"wait returns as soon as it ends, as soon as its last lines hold the until text, or after %d s, with whether it is still running, its exit code and its last lines: one wait covers a long build, never sleep in bash. "+
			"wait with for returns as soon as the thing is ready, as JSON with met, after_ms and the matched line, or met false and its last lines: for is a regex over every line it printed, such as listening on, or port:3000, file:dist/app.js, or an http:// address that must answer 200. "+
			"logs folds what it printed into templates with counts, and the end of a log file its command names, such as -Log build.log or > build.log; a later logs prints only new_since_last_call. "+
			"stop kills its whole process tree, children included, so the port it held is free when this returns. "+
			"restart stops it and starts the same command in the same folder under the same name. never stop a server with kill, taskkill or pkill", konst.ShellWaitMillis/1000),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"op":         map[string]any{"type": "string", "enum": []string{"wait", "logs", "stop", "restart"}},
				"name":       map[string]any{"type": "string", "description": "the shell's name, as bash returned it"},
				"until":      map[string]any{"type": "string", "description": "only for wait: text the command prints once the thing waited for is ready, such as Listening on, so a server that never exits ends the wait"},
				"for":        map[string]any{"type": "string", "description": "only for wait: a regex over its lines, port:N, file:path, or an http:// or https:// address answering 200"},
				"timeout_ms": map[string]any{"type": "integer", "description": fmt.Sprintf("only for wait with for: how long to wait, at most %d", konst.ShellWaitMillis)},
			},
			"required": []string{"op", "name"},
		},
	}
}

type shellArgs struct {
	Op        string `json:"op"`
	Name      string `json:"name"`
	Until     string `json:"until"`
	For       string `json:"for"`
	TimeoutMS int64  `json:"timeout_ms"`
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
		text, first, err := s.Seen.follow(registry, entry)
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
		ended, tail, err := awaitEndOrText(ctx, registry, args.Name, args.Until)
		if err != nil {
			return "", nil, err
		}
		state := args.Name + " is still running: wait again, or stop it"
		switch words := endWords(ended); {
		case ended.HeldByPerson():
			return args.Name + " was taken over by the person; no longer waiting", nil, nil
		case ended.State == process.Running && args.Until != "" && strings.Contains(tail, args.Until):
			state = fmt.Sprintf("%s printed %q and is still running", args.Name, args.Until)
		case words != "":
			state = args.Name + " " + words
		}
		now := time.Now()
		return state + ", " + registry.Timing(ended, now).Words(now) + ". its last lines:\n" + tail, ended.ExitCode, nil
	}
	return "", nil, fmt.Errorf("op %q is not wait, logs, stop or restart", args.Op)
}

func (s *Seen) follow(registry *process.Registry, entry process.Shell) (string, bool, error) {
	if s == nil {
		text, err := registry.Follow(entry.Name, &process.Cursor{})
		return text, true, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := seenShell{name: entry.Name, started: entry.Started.UnixNano()}
	cursor, seen := s.cursors[key]
	if !seen {
		if s.cursors == nil {
			s.cursors = map[seenShell]*process.Cursor{}
		}
		cursor = &process.Cursor{}
		s.cursors[key] = cursor
	}
	text, err := registry.Follow(entry.Name, cursor)
	return text, !seen, err
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

func awaitEndOrText(ctx context.Context, registry *process.Registry, name, until string) (process.Shell, string, error) {
	deadline := time.Now().Add(konst.ShellWaitMillis * time.Millisecond)
	for {
		step := time.Until(deadline)
		if until != "" {
			step = min(step, konst.ShellWaitMatchMillis*time.Millisecond)
		}
		ended, err := registry.AwaitEnd(ctx, name, step)
		if err != nil || ended.HeldByPerson() {
			return ended, "", err
		}
		tail, err := registry.Tail(name, process.DefaultTail)
		if err != nil || ended.State != process.Running || ctx.Err() != nil || time.Until(deadline) <= 0 || until != "" && strings.Contains(tail, until) {
			return ended, tail, err
		}
	}
}
