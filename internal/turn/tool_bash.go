package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"boji/internal/llm"
)

type BashTool struct {
	root string
}

func NewBashTool(root string) (*BashTool, error) {
	resolved, err := resolveRoot(root)
	if err != nil {
		return nil, err
	}
	return &BashTool{root: resolved}, nil
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Definition() llm.Tool {
	return llm.Tool{
		Name:        "bash",
		Description: "runs a shell command with its working directory pinned to the turn's working directory",
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

	shell, flag := shellFor(runtime.GOOS)
	cmd := exec.CommandContext(ctx, shell, flag, args.Command)
	cmd.Dir = t.root

	output, runErr := cmd.CombinedOutput()
	if cmd.ProcessState == nil {
		return Result{}, fmt.Errorf("bash: %w", runErr)
	}
	code := cmd.ProcessState.ExitCode()
	return Result{Content: string(output), Command: args.Command, ExitCode: &code}, nil
}

func shellFor(goos string) (string, string) {
	if goos == "windows" {
		return "cmd", "/C"
	}
	return "sh", "-c"
}
