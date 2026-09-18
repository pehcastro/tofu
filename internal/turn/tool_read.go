package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"boji/internal/llm"
)

type ReadTool struct {
	root string
}

func NewReadTool(root string) (*ReadTool, error) {
	resolved, err := resolveRoot(root)
	if err != nil {
		return nil, err
	}
	return &ReadTool{root: resolved}, nil
}

func (t *ReadTool) Name() string { return "read" }

func (t *ReadTool) Definition() llm.Tool {
	return llm.Tool{
		Name:        "read",
		Description: "reads the whole content of a file inside the turn's working directory",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
	}
}

type readArgs struct {
	Path string `json:"path"`
}

func (t *ReadTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args readArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("read: arguments are not the expected shape: %w", err)
	}
	resolved, err := confine(t.root, args.Path)
	if err != nil {
		return Result{}, fmt.Errorf("read: %w", err)
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return Result{}, fmt.Errorf("read: %w", err)
	}
	return Result{Content: string(content), Command: "read " + args.Path}, nil
}
