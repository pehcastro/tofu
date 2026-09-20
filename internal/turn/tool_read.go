package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"tofu/internal/llm"
)

type ReadTool struct {
	root Root
}

func NewReadTool(root string) (*ReadTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	return &ReadTool{root: resolved}, nil
}

func (t *ReadTool) Name() string { return "read" }

func (t *ReadTool) Definition() llm.Tool {
	return llm.Tool{
		Name:        "read",
		Description: "reads a file inside the turn's working directory, whole, or only the lines from start_line to end_line, counted from 1 and both included",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string"},
				"start_line": map[string]any{"type": "integer"},
				"end_line":   map[string]any{"type": "integer"},
			},
			"required": []string{"path"},
		},
	}
}

type readArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func (t *ReadTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args readArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("read: arguments are not the expected shape: %w", err)
	}
	resolved, err := t.root.Resolve(args.Path)
	if err != nil {
		return Result{}, fmt.Errorf("read: %w", err)
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return Result{}, fmt.Errorf("read: %w", err)
	}
	if args.StartLine <= 0 && args.EndLine <= 0 {
		return Result{Content: string(content), Command: "read " + args.Path}, nil
	}

	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	start, end := args.StartLine, args.EndLine
	if start <= 0 {
		start = 1
	}
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		return Result{}, fmt.Errorf("read: %s has %d lines and start_line is %d", args.Path, len(lines), start)
	}
	if end < start {
		return Result{}, fmt.Errorf("read: %s end_line %d is before start_line %d", args.Path, end, start)
	}
	span := fmt.Sprintf("%s lines %d-%d of %d", args.Path, start, end, len(lines))
	return Result{
		Content: span + "\n" + strings.Join(lines[start-1:end], "\n"),
		Command: "read " + span,
	}, nil
}
