package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"tofu/internal/llm"
	"tofu/internal/turn"
)

type Test struct {
	root    turn.Root
	runners *turn.TestRunners
}

func NewTest(dir string, runners *turn.TestRunners) (Test, error) {
	root, err := turn.NewRoot(dir)
	return Test{root: root, runners: runners}, err
}

func (Test) Name() string { return "test" }

func (Test) Definition() llm.Tool {
	return llm.Tool{
		Name: "test",
		Description: "runs the vitest tests for one file and returns each test file's pass and fail counts and its failures. " +
			"path is a test file, or a source file whose tests then run: the files beside it named <stem>.test.* or <stem>.spec.*, and none if it has none. " +
			"a vitest runner stays warm for the package, so a run after the first answers in about a second where npx vitest run through the shell takes many: " +
			"run tests with this instead of through the shell. a project that does not use vitest says how it runs its tests, and those run through the shell",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
	}
}

func (t Test) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("test: arguments are not the expected shape: %w", err)
	}
	resolved, err := t.root.Resolve(args.Path)
	if err != nil {
		return turn.Result{}, fmt.Errorf("test: %w", err)
	}
	result, err := t.runners.Test(ctx, resolved)
	if err != nil {
		return turn.Result{}, fmt.Errorf("test: %w", err)
	}
	result.Command = args.Path
	return result, nil
}
