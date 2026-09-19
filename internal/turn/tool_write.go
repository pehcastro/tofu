package turn

import (
	"context"
	"encoding/json"
	"fmt"

	"boji/internal/llm"
	"boji/internal/sys"
)

const writePerm = 0o644

type WriteTool struct {
	root Root
}

func NewWriteTool(root string) (*WriteTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	return &WriteTool{root: resolved}, nil
}

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Definition() llm.Tool {
	return llm.Tool{
		Name:        "write",
		Description: "writes a file inside the turn's working directory, replacing it whole",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args writeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("write: arguments are not the expected shape: %w", err)
	}
	resolved, err := t.root.Resolve(args.Path)
	if err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	if err := sys.WriteFile(resolved, []byte(args.Content), writePerm); err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	return Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path), Command: "write " + args.Path}, nil
}
