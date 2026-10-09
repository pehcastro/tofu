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
	if t.boundary.HoldsTheTree() {
		definition.Description += ", and you hold the whole tree, so a command may write anywhere in it. A source file still changes with edit or write, never through a redirect, tee, cp, mv or sed -i, and a tree-wide check is the orchestrator's."
		return definition
	}
	definition.Description += ", and you hold only part of this project or none of it, so a command runs only when every step reads, lists, searches or inspects " +
		"(ls, cat, head, grep, rg, find without -exec or -delete, sed without w or e, git status, log, diff, show or blame) " +
		"or runs the project's own checks (go vet and go test, cargo test, check and clippy, npm, pnpm, yarn or bun test or a test, check, lint, typecheck or vet script, " +
		"make targets named the same way, pytest, mypy, ruff check, and rtk or uv run around any of these). " +
		"A redirect, tee, cp, mv, rm, mkdir, touch or sed -i lands only inside the paths your first message says you hold, your scratch folder or the temp folder, and a source file changes with edit or write. " +
		"An interpreter (python -c, node -e, bash -c, powershell -Command, eval), a variable set before a command, $(...) or backticks, a program named by its path, and anything not listed are refused before they run, with the reason: put that work in your report."
	return definition
}

func (t ownedShell) grammars() []subagent.Grammar {
	if bash, found := t.tool.(*BashTool); found {
		return []subagent.Grammar{subagent.GrammarOf(bash.choice.Path)}
	}
	return []subagent.Grammar{subagent.POSIX, subagent.PowerShell}
}

func (t ownedShell) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.Name(), err)
	}
	for _, grammar := range t.grammars() {
		if err := t.boundary.Bash(args.Command, grammar); err != nil {
			return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
		}
	}
	return t.tool.Run(ctx, raw)
}
