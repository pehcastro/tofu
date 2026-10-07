package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/shell"
	"tofu/internal/turn"
)

type Shells struct{}

func (Shells) Name() string { return turn.ShellToolName }

func (Shells) Definition() llm.Tool {
	return llm.Tool{
		Name: turn.ShellToolName,
		Description: fmt.Sprintf("controls a process bash kept running, by the name that call returned, such as bash-1: a background: true start, or a command bash moved to a background shell because it was still running. "+
			"wait returns as soon as it ends, or after %d ms, with whether it is still running, its exit code and its last lines: call it again to keep waiting on a long build, never sleep in bash. "+
			"logs returns the last lines it printed now, and the end of a log file its command names, such as -Log build.log or > build.log. "+
			"stop kills its whole process tree, children included, so the port it held is free when this returns. "+
			"restart stops it and starts the same command in the same folder under the same name. never stop a server with kill, taskkill or pkill", konst.BashSoftLimitMillis),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"op":   map[string]any{"type": "string", "enum": []string{"wait", "logs", "stop", "restart"}},
				"name": map[string]any{"type": "string", "description": "the shell's name, as bash returned it"},
			},
			"required": []string{"op", "name"},
		},
	}
}

func (Shells) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Op   string `json:"op"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("shell: arguments are not the expected shape: %w", err)
	}
	registry := turn.ShellRegistryFrom(ctx)
	if registry == nil {
		return turn.Result{}, errors.New("shell: no shell registry is attached to this turn")
	}
	command := args.Op + " " + args.Name
	switch args.Op {
	case "stop":
		if err := registry.Kill(args.Name); err != nil {
			return turn.Result{}, fmt.Errorf("shell: %w", err)
		}
		return turn.Result{Content: args.Name + " stopped, with every process it started", Command: command}, nil
	case "restart":
		started, err := registry.Restart(args.Name)
		if err != nil {
			return turn.Result{}, fmt.Errorf("shell: %w", err)
		}
		return turn.Result{Content: fmt.Sprintf("%s restarted as pid %d in %s: %s. check it with bash check_port once it should be up", started.Name, started.PID, started.Dir, started.Command), Command: command}, nil
	case "logs":
		entry, err := registry.Read(args.Name)
		if err != nil {
			return turn.Result{}, fmt.Errorf("shell: %w", err)
		}
		tail, err := registry.Tail(args.Name, shell.DefaultTail)
		if err != nil {
			return turn.Result{}, fmt.Errorf("shell: %w", err)
		}
		now := time.Now()
		return turn.Result{Content: fmt.Sprintf("%s is %s, %s. its last lines:\n%s", args.Name, entry.State, registry.Timing(entry, now).Words(now), tail), Command: command}, nil
	case "wait":
		ended, err := registry.AwaitEnd(ctx, args.Name, konst.BashSoftLimitMillis*time.Millisecond)
		if err != nil {
			return turn.Result{}, fmt.Errorf("shell: %w", err)
		}
		tail, err := registry.Tail(args.Name, shell.DefaultTail)
		if err != nil {
			return turn.Result{}, fmt.Errorf("shell: %w", err)
		}
		state := args.Name + " is still running: wait again, or stop it"
		switch {
		case ended.State == shell.Killed:
			state = args.Name + " was stopped"
		case ended.ExitCode != nil:
			state = fmt.Sprintf("%s exited %d", args.Name, *ended.ExitCode)
		case ended.State == shell.Exited:
			state = args.Name + " ended, with an exit code tofu did not see"
		}
		now := time.Now()
		return turn.Result{Content: state + ", " + registry.Timing(ended, now).Words(now) + ". its last lines:\n" + tail, Command: command, ExitCode: ended.ExitCode}, nil
	}
	return turn.Result{}, fmt.Errorf("shell: op %q is not wait, logs, stop or restart", args.Op)
}
