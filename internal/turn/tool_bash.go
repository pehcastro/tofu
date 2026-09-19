package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"boji/internal/llm"
)

type BashTool struct {
	root  Root
	shell string
}

func NewBashTool(root string) (*BashTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	for _, posix := range []string{"sh", "bash"} {
		shell, lookErr := exec.LookPath(posix)
		if lookErr == nil {
			return &BashTool{root: resolved, shell: shell}, nil
		}
	}
	return nil, errors.New("bash: no sh or bash on PATH, and cmd.exe is not a substitute: it mangles every quoted argument and understands none of the posix syntax this tool advertises")
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Definition() llm.Tool {
	return llm.Tool{
		Name:        "bash",
		Description: "runs a posix shell command with its working directory pinned to the turn's working directory",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"command": map[string]any{"type": "string"}},
			"required":   []string{"command"},
		},
	}
}

type bashArgs struct {
	Command string `json:"command"`
}

func (t *BashTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("bash: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return Result{}, errors.New("bash: command is required")
	}

	cmd := exec.CommandContext(ctx, t.shell, "-c", args.Command)
	cmd.Dir = string(t.root)

	output, runErr := cmd.CombinedOutput()
	if cmd.ProcessState == nil {
		return Result{}, fmt.Errorf("bash: %w", runErr)
	}
	code := cmd.ProcessState.ExitCode()
	return Result{Content: string(output), Command: args.Command, ExitCode: &code}, nil
}
