package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
			"wait returns as soon as it ends, as soon as its last lines hold the until text, or after %d s, with whether it is still running, its exit code and its last lines: one wait covers a long build, never sleep in bash. "+
			"logs returns the last lines it printed now, and the end of a log file its command names, such as -Log build.log or > build.log. "+
			"stop kills its whole process tree, children included, so the port it held is free when this returns. "+
			"restart stops it and starts the same command in the same folder under the same name. never stop a server with kill, taskkill or pkill", konst.ShellWaitMillis/1000),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"op":    map[string]any{"type": "string", "enum": []string{"wait", "logs", "stop", "restart"}},
				"name":  map[string]any{"type": "string", "description": "the shell's name, as bash returned it"},
				"until": map[string]any{"type": "string", "description": "only for wait: text the command prints once the thing waited for is ready, such as Listening on, so a server that never exits ends the wait"},
			},
			"required": []string{"op", "name"},
		},
	}
}

type shellArgs struct {
	Op    string `json:"op"`
	Name  string `json:"name"`
	Until string `json:"until"`
}

func (Shells) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args shellArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("shell: arguments are not the expected shape: %w", err)
	}
	registry := turn.ShellRegistryFrom(ctx)
	if registry == nil {
		return turn.Result{}, errors.New("shell: no shell registry is attached to this turn")
	}
	command := args.Op + " " + args.Name
	content, exitCode, err := runShellOp(ctx, registry, args)
	var held shell.HeldError
	switch {
	case errors.As(err, &held):
		return turn.Result{Content: held.Error(), Command: command}, nil
	case err != nil:
		return turn.Result{}, fmt.Errorf("shell: %w", err)
	}
	return turn.Result{Content: content, Command: command, ExitCode: exitCode}, nil
}

func runShellOp(ctx context.Context, registry *shell.Registry, args shellArgs) (string, *int, error) {
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
		tail, err := registry.Tail(args.Name, shell.DefaultTail)
		now := time.Now()
		return fmt.Sprintf("%s is %s, %s. its last lines:\n%s", args.Name, entry.State, registry.Timing(entry, now).Words(now), tail), nil, err
	case "wait":
		if _, err := registry.AgentRead(args.Name); err != nil {
			return "", nil, err
		}
		if turn.ExitComesBack(ctx, args.Name) {
			return args.Name + " is your own command, moved to the background, and its exit comes back to you as a message when it ends, so this does not wait: answer the person, go on with other work, or end your turn", nil, nil
		}
		ended, tail, err := awaitEndOrText(ctx, registry, args.Name, args.Until)
		if err != nil {
			return "", nil, err
		}
		state := args.Name + " is still running: wait again, or stop it"
		switch {
		case ended.HeldByPerson():
			return args.Name + " was taken over by the person; no longer waiting", nil, nil
		case ended.State == shell.Running && args.Until != "" && strings.Contains(tail, args.Until):
			state = fmt.Sprintf("%s printed %q and is still running", args.Name, args.Until)
		case ended.State == shell.Killed:
			state = args.Name + " was stopped"
		case ended.ExitCode != nil:
			state = fmt.Sprintf("%s exited %d", args.Name, *ended.ExitCode)
		case ended.State == shell.Exited:
			state = args.Name + " ended, with an exit code tofu did not see"
		}
		now := time.Now()
		return state + ", " + registry.Timing(ended, now).Words(now) + ". its last lines:\n" + tail, ended.ExitCode, nil
	}
	return "", nil, fmt.Errorf("op %q is not wait, logs, stop or restart", args.Op)
}

func awaitEndOrText(ctx context.Context, registry *shell.Registry, name, until string) (shell.Shell, string, error) {
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
		tail, err := registry.Tail(name, shell.DefaultTail)
		if err != nil || ended.State != shell.Running || ctx.Err() != nil || time.Until(deadline) <= 0 || until != "" && strings.Contains(tail, until) {
			return ended, tail, err
		}
	}
}
