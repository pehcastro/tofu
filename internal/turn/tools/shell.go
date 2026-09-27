package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"tofu/internal/llm"
	"tofu/internal/shell"
	"tofu/internal/turn"
)

type Shells struct{}

func (Shells) Name() string { return turn.ShellToolName }

func (Shells) Definition() llm.Tool {
	return llm.Tool{
		Name: turn.ShellToolName,
		Description: "controls a process bash started with background: true, by the name that call returned, such as bash-1. " +
			"stop kills its whole process tree, children included, so the port it held is free when this returns. " +
			"restart stops it and starts the same command in the same folder under the same name. " +
			"logs returns the last lines it printed. never stop a server with kill, taskkill or pkill",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"op":   map[string]any{"type": "string", "enum": []string{"stop", "restart", "logs"}},
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
		tail, err := registry.Tail(args.Name, shell.DefaultTail)
		if err != nil {
			return turn.Result{}, fmt.Errorf("shell: %w", err)
		}
		return turn.Result{Content: tail, Command: command}, nil
	}
	return turn.Result{}, fmt.Errorf("shell: op %q is not stop, restart or logs", args.Op)
}
