package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"tofu/internal/llm"
	"tofu/internal/sys"
	"tofu/internal/transform"
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
		Name: "write",
		Description: "writes a file inside the turn's working directory, replacing it whole. " +
			"the result says the file was created when it was not there before, " +
			"and is a unified diff of what changed when it was",
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
	held, readErr := os.ReadFile(resolved)
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return Result{}, fmt.Errorf("write: reading %s before replacing it: %w", args.Path, readErr)
	}
	if err := sys.WriteFile(resolved, []byte(args.Content), writePerm); err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	before := string(held)
	preview := transform.Preview{
		Path:    args.Path,
		Existed: readErr == nil,
		Before:  before,
		After:   args.Content,
		Diff:    transform.Unified(args.Path, before, args.Content),
	}
	return Result{Content: preview.Result(), Command: "write " + args.Path}, nil
}
