package loop

import (
	"context"
	"encoding/json"
	"fmt"

	"tofu/internal/agent/subagents"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit"
)

type ownedTool struct {
	tool     contract.Tool
	boundary *subagents.Boundary
}

func (t ownedTool) Name() string { return t.tool.Name() }

func (t ownedTool) Definition() core.Tool {
	definition := t.tool.Definition()
	definition.Description += ", and only inside the paths your first message says you hold, or your scratch folder"
	return definition
}

type bounded interface {
	refusal(raw json.RawMessage) error
}

func boundaryRefusal(tools Registry, call core.ToolCall) string {
	if checked, checks := tools.byName[call.Name].(bounded); checks {
		if err := checked.refusal(call.Arguments); err != nil {
			return err.Error()
		}
	}
	return ""
}

func (t ownedTool) refusal(raw json.RawMessage) error {
	paths, err := edit.TargetPaths(t.Name(), raw)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if len(t.boundary.Owns()) == 0 && !t.boundary.Scratched(path) {
			return ReadOnlyError{Tool: t.Name()}
		}
		if err := t.boundary.Write(path); err != nil {
			return fmt.Errorf("%s: %w%s", t.Name(), err, edit.NoneWritten(len(paths)))
		}
	}
	return nil
}

func (t ownedTool) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	if err := t.refusal(raw); err != nil {
		return contract.Result{}, err
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
	tool     contract.Tool
	boundary *subagents.Boundary
}

func (t ownedShell) Name() string { return t.tool.Name() }

func (t ownedShell) Definition() core.Tool {
	definition := t.tool.Definition()
	if t.boundary.HoldsTheTree() {
		definition.Description += ", and you hold the whole tree, so a command may write anywhere in it. A source file still changes with edit or write, never through a redirect, tee, cp, mv or sed -i, and go test all is the orchestrator's. " +
			"One python or node call with -c or -e, a heredoc, or a script in your scratch folder is inline code: it asks the person, and runs only if the person allows it."
		return definition
	}
	definition.Description += ", and you hold only part of this project or none of it, so a command runs only when every step reads, lists, searches or inspects " +
		"(ls, cat, head, grep, rg, paste, bc, sed without w or e, git status, log, diff, show, blame or grep, awk without system(), pipes or print >, " +
		"for loops over these, and xargs or find -exec handing paths to one of these pure reads, never to rm, cp, mv, sed -i or a check; find -delete is refused) " +
		"or runs the project's own checks (go vet and go test, gofmt -l, cargo test, check and clippy, npm, pnpm, yarn or bun test or a test, check, lint, typecheck or vet script, " +
		"make targets named the same way, pytest, mypy, ruff check, rustfmt --check, zig version, zig fmt --check, zig build of steps named test, check, lint, typecheck or vet, and rtk or uv run around any of these). " +
		"zig build of any other step runs in a build root you hold a path in, and --prefix, --cache-dir and --build-file are refused. " +
		"The project's own built binaries run by their relative path under zig-out/bin, target/debug or target/release, with arguments inside the project. " +
		"gofmt -w, rustfmt and zig fmt run on files you hold. cargo build and cargo run (with --example, or -- and the program's arguments) run on a crate you hold a path in, named with -p, --manifest-path or the folder you cd into, " +
		"and so does a binary or example cargo built, run by its path under $CARGO_TARGET_DIR: every cargo command builds into that one shared target of the project, so --target-dir and a CARGO_TARGET_DIR of your own are refused. " +
		"A program you run sends its input to its own window only, never to the real cursor or keyboard, which the person is using. " +
		"Of .tofu you read only .tofu/rules, the project rules you follow, and every other .tofu path is refused. tofu runs only as serve --schema, which prints the schema tofu serve writes, version, models, session find, trace or info, boardy show, list or triage list, and drive desk: " +
		"tofu drive desk [--first-run ready] [--out DIR] click:X,Y type:TEXT key:NAME wait:500ms shot:NAME [-- FOLDER or desk flags] opens the newest desk build under $CARGO_TARGET_DIR in a home made fresh for the run, on a desktop of its own that never shows on the person's screen, " +
		"writes a screenshot after opening, at each shot and at the end, with a step log, into --out inside a path you hold or a new folder under $TMPDIR, prints that folder, and closes the desk. " +
		"powershell -File runs a script inside a path you hold, and timeout <n> wraps any command allowed here. A check over . or ./... runs when you hold a file in every folder it reaches, and is the orchestrator's otherwise. " +
		"A redirect, tee, cp, mv, rm, mkdir, touch or sed -i lands only inside the paths your first message says you hold or your scratch folder, which $TMPDIR names; the system temp folder is refused, and a source file changes with edit or write. " +
		"Inline code asks the person: one python or node call with -c or -e in single quotes, a heredoc (python - <<'EOF'), or a script in your scratch folder ($TMPDIR/x.py), with nothing piped or chained. " +
		"The orchestrator may refuse it, only the person may allow it, and it runs with cwd in your scratch folder and no secret names in its environment. " +
		"Any other interpreter call (bash -c, powershell -Command, eval), a variable set before a command, $(...) or backticks, a program named by any other path, and anything not listed are refused before they run, with the reason: put that work in your report."
	return definition
}

func (t ownedShell) grammars() []subagents.Grammar {
	if bash, found := underlyingBash(t.tool); found {
		return []subagents.Grammar{subagents.GrammarOf(bash.choice.Path)}
	}
	return []subagents.Grammar{subagents.POSIX, subagents.PowerShell}
}

func underlyingBash(tool contract.Tool) (*BashTool, bool) {
	for {
		switch inner := tool.(type) {
		case *BashTool:
			return inner, true
		case interface{ Uncached() contract.Tool }:
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

func (t ownedShell) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	if err := t.refusal(raw); err != nil {
		return contract.Result{}, err
	}
	return t.tool.Run(ctx, raw)
}
