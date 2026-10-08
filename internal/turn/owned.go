package turn

import (
	"context"
	"encoding/json"
	"fmt"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type ownedTool struct {
	tool     Tool
	boundary *subagent.Boundary
}

func (t ownedTool) Name() string { return t.tool.Name() }

func (t ownedTool) Definition() llm.Tool {
	definition := t.tool.Definition()
	definition.Description += ", and only inside the paths your first message says you hold, or your scratch folder"
	return definition
}

func (t ownedTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.Name(), err)
	}
	if len(t.boundary.Owns()) == 0 && !t.boundary.Scratched(args.Path) {
		return Result{}, ReadOnlyError{Tool: t.Name()}
	}
	if err := t.boundary.Write(args.Path); err != nil {
		return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
	}
	return t.tool.Run(ctx, raw)
}

type ReadOnlyError struct {
	Tool string
}

func (e ReadOnlyError) Error() string {
	if e.Tool == "spawn" {
		return "spawn refused: you were spawned without owns, so a sub-agent you spawn holds none either: leave owns out, or put the work in your report for the orchestrator"
	}
	return e.Tool + " refused: you were spawned without owns, so you write nothing: put what you found in your report, and the orchestrator writes it or spawns a sub-agent that holds the path"
}

type ownedShell struct {
	tool     Tool
	boundary *subagent.Boundary
}

func (t ownedShell) Name() string { return t.tool.Name() }

func (t ownedShell) Definition() llm.Tool {
	definition := t.tool.Definition()
	definition.Description += ", and every file the command writes, through a redirect, tee, cp or mv, has to be inside the paths your first message says you hold, or your scratch folder. " +
		"A command writing outside them is refused before it runs, and that work goes back to the orchestrator. A source file changes with edit or write, never through the shell. Reading anything is fine."
	return definition
}

func (t ownedShell) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.Name(), err)
	}
	if err := t.boundary.Shell(args.Command); err != nil {
		return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
	}
	return t.tool.Run(ctx, raw)
}
