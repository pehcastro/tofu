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

type bounded interface {
	refusal(raw json.RawMessage) error
}

func boundaryRefusal(tools Registry, call llm.ToolCall) string {
	if checked, checks := tools.byName[call.Name].(bounded); checks {
		if err := checked.refusal(call.Arguments); err != nil {
			return err.Error()
		}
	}
	return ""
}

func (t ownedTool) refusal(raw json.RawMessage) error {
	paths, err := TargetPaths(t.Name(), raw)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if len(t.boundary.Owns()) == 0 && !t.boundary.Scratched(path) {
			return ReadOnlyError{Tool: t.Name()}
		}
		if err := t.boundary.Write(path); err != nil {
			return fmt.Errorf("%s: %w%s", t.Name(), err, NoneWritten(len(paths)))
		}
	}
	return nil
}

func (t ownedTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := t.refusal(raw); err != nil {
		return Result{}, err
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
		definition.Description += ", and you hold the whole tree, so a command may write anywhere in it. A source file still changes with edit or write, never through a redirect, tee, cp, mv or sed -i, and go test all is the orchestrator's. " +
			"One python or node call with -c or -e, a heredoc, or a script in your scratch folder is inline code: it asks the person, and runs only if the person allows it."
		return definition
	}
	definition.Description += ", and you hold only part of this project or none of it, so a command runs only when every step reads, lists, searches or inspects " +
		"(ls, cat, head, grep, rg, find without -exec or -delete, sed without w or e, git status, log, diff, show or blame) " +
		"or runs the project's own checks (go vet and go test, gofmt -l, cargo test, check and clippy, npm, pnpm, yarn or bun test or a test, check, lint, typecheck or vet script, " +
		"make targets named the same way, pytest, mypy, ruff check, rustfmt --check, and rtk or uv run around any of these). " +
		"gofmt -w, rustfmt and cargo build run on files and a target folder you hold. A check over . or ./... runs when you hold a file in every folder it reaches, and is the orchestrator's otherwise. " +
		"A redirect, tee, cp, mv, rm, mkdir, touch or sed -i lands only inside the paths your first message says you hold or your scratch folder, which $TMPDIR names; the system temp folder is refused, and a source file changes with edit or write. " +
		"Inline code asks the person: one python or node call with -c or -e in single quotes, a heredoc (python - <<'EOF'), or a script in your scratch folder ($TMPDIR/x.py), with nothing piped or chained. " +
		"The orchestrator may refuse it, only the person may allow it, and it runs with cwd in your scratch folder and no secret names in its environment. " +
		"Any other interpreter call (bash -c, powershell -Command, eval), a variable set before a command, $(...) or backticks, a program named by its path, and anything not listed are refused before they run, with the reason: put that work in your report."
	return definition
}

func (t ownedShell) grammars() []subagent.Grammar {
	if bash, found := underlyingBash(t.tool); found {
		return []subagent.Grammar{subagent.GrammarOf(bash.choice.Path)}
	}
	return []subagent.Grammar{subagent.POSIX, subagent.PowerShell}
}

func underlyingBash(tool Tool) (*BashTool, bool) {
	for {
		switch inner := tool.(type) {
		case *BashTool:
			return inner, true
		case interface{ Uncached() Tool }:
			tool = inner.Uncached()
		default:
			return nil, false
		}
	}
}

func (t ownedShell) refusal(raw json.RawMessage) error {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return fmt.Errorf("%s: arguments are not the expected shape: %w", t.Name(), err)
	}
	for _, grammar := range t.grammars() {
		if err := t.boundary.Bash(args.Command, grammar); err != nil {
			return fmt.Errorf("%s: %w", t.Name(), err)
		}
	}
	return nil
}

func (t ownedShell) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := t.refusal(raw); err != nil {
		return Result{}, err
	}
	return t.tool.Run(ctx, raw)
}
