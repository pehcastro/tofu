package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/subagent"
	"tofu/internal/transform"
)

type BudgetRefusal string

const (
	RefusedShellSource BudgetRefusal = "shell"
	RefusedOverLines   BudgetRefusal = "lines"
)

type SourceBudgetError struct {
	Tool   string
	Path   string
	Lines  int
	Spent  int
	Spawn  []string
	Reason BudgetRefusal
}

func (e SourceBudgetError) Error() string {
	spawn := strings.Join(e.Spawn, " or ")
	switch e.Reason {
	case RefusedShellSource:
		return fmt.Sprintf("%s: %s is source code written by hand through the shell, which the orchestrator never does: use edit or write within your budget of %d changed source lines a turn, or spawn %s. a rename is edit with replace_all in each file, and a program's own output redirected into a file, such as a generator or a schema printer, is not counted and runs",
			e.Tool, e.Path, konst.OrchestratorSourceLinesPerTurn, spawn)
	case RefusedOverLines:
		return fmt.Sprintf("%s: %s is source code, and this change of %d lines would pass the orchestrator's budget of %d changed source lines in one turn, %d of which are spent: spawn %s with this work instead, rather than another route to the same file, such as sed or a redirect",
			e.Tool, e.Path, e.Lines, konst.OrchestratorSourceLinesPerTurn, e.Spent, spawn)
	}
	panic("tofu: unknown source budget refusal " + string(e.Reason))
}

func sourceLanguage(path string) string {
	language := rule.LanguageOf(path)
	if language == "markdown" || language == "yaml" {
		return ""
	}
	return language
}

func linesIn(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(text, "\n"), "\n") + 1
}

func handWrittenSource(command string) (string, error) {
	if _, err := subagent.ShellWrites(command); err != nil {
		return "", err
	}
	for _, step := range subagent.ShellSteps(command) {
		if !slices.Contains([]string{"", "echo", "printf", "cat", "tee", "sed", "perl", "cp", "mv"}, step.Program) {
			continue
		}
		if at := slices.IndexFunc(step.Changes, func(path string) bool { return sourceLanguage(path) != "" }); at >= 0 {
			return step.Changes[at], nil
		}
	}
	return "", nil
}

type sourceBudget struct {
	spent   int
	enabled []subagent.Definition
	files   *WriteTool
}

type budgetArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append"`
	Old     string `json:"old_string"`
	New     string `json:"new_string"`
	Command string `json:"command"`
}

func (b *sourceBudget) linesChanged(tool string, args budgetArgs) int {
	whole := max(linesIn(args.Content), linesIn(args.Old), linesIn(args.New))
	if tool != "write" || args.Append || b.files == nil {
		return whole
	}
	resolved, err := b.files.root.Resolve(args.Path)
	if err != nil {
		return whole
	}
	held, err := os.ReadFile(resolved)
	if err != nil {
		return whole
	}
	changed := 0
	for _, hunk := range transform.Hunks(string(held), args.Content) {
		changed += max(len(hunk.Removed), len(hunk.Added))
	}
	return changed
}

func wholeFilePrinters(command string) ([]string, bool) {
	var args []string
	printing := false
	for _, step := range subagent.ShellSteps(command) {
		switch {
		case step.Program == "cat":
		case step.Program == "rtk" && len(step.Args) > 0 && step.Args[0] == "read":
			step.Args = step.Args[1:]
		default:
			continue
		}
		args, printing = append(args, step.Args...), true
	}
	return args, printing
}

func (b *sourceBudget) markPrinted(command, printed string) {
	args, printing := wholeFilePrinters(command)
	if b.files == nil || !printing {
		return
	}
	root := string(b.files.root)
	named := briefPath.FindAllString(printed, -1)
	for _, arg := range args {
		matches, _ := filepath.Glob(filepath.Join(root, arg))
		for _, match := range matches {
			if relative, err := filepath.Rel(root, match); err == nil {
				named = append(named, filepath.ToSlash(relative))
			}
		}
	}
	printed = strings.ReplaceAll(printed, "\r\n", "\n")
	slices.Sort(named)
	for _, path := range slices.Compact(named) {
		resolved, err := b.files.root.Resolve(path)
		if err != nil {
			continue
		}
		body, err := os.ReadFile(resolved)
		if err == nil && len(body) > 0 && strings.Contains(printed, strings.ReplaceAll(string(body), "\r\n", "\n")) {
			b.files.ledger.Mark(path, body)
		}
	}
}

type budgetedTool struct {
	tool   Tool
	budget *sourceBudget
}

func WithSourceBudget(tools []Tool, defined []subagent.Definition) []Tool {
	budget := &sourceBudget{enabled: enabledSubAgents(defined)}
	wrapped := slices.Clone(tools)
	for i, tool := range wrapped {
		inner := tool
		if memoised, cached := tool.(interface{ Uncached() Tool }); cached {
			inner = memoised.Uncached()
		}
		if writer, writes := inner.(*WriteTool); writes {
			budget.files = writer
		}
		if slices.Contains([]string{"write", "edit", "bash"}, tool.Name()) {
			wrapped[i] = budgetedTool{tool: tool, budget: budget}
		}
	}
	return wrapped
}

func (t budgetedTool) Name() string { return t.tool.Name() }

func (t budgetedTool) Definition() llm.Tool {
	definition := t.tool.Definition()
	if definition.Name == "edit" {
		definition.Description += fmt.Sprintf(". your source budget as the orchestrator is %d changed source lines a turn, in any directory", konst.OrchestratorSourceLinesPerTurn)
	}
	return definition
}

func (t budgetedTool) refuse(refusal SourceBudgetError) SourceBudgetError {
	language := sourceLanguage(refusal.Path)
	for _, definition := range t.budget.enabled {
		if definition.Language == language {
			refusal.Spawn = append(refusal.Spawn, definition.Name)
		}
	}
	if len(refusal.Spawn) == 0 {
		refusal.Spawn = []string{"a sub-agent"}
	}
	refusal.Tool, refusal.Spent = t.Name(), t.budget.spent
	return refusal
}

func (t budgetedTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args budgetArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return t.tool.Run(ctx, raw)
	}
	if t.Name() == "bash" {
		written, err := handWrittenSource(args.Command)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
		}
		if written != "" {
			return Result{}, t.refuse(SourceBudgetError{Path: written, Reason: RefusedShellSource})
		}
		result, err := t.tool.Run(ctx, raw)
		if err == nil {
			t.budget.markPrinted(args.Command, result.Content)
		}
		return result, err
	}
	if sourceLanguage(args.Path) == "" {
		return t.tool.Run(ctx, raw)
	}
	lines := t.budget.linesChanged(t.Name(), args)
	if t.budget.spent+lines > konst.OrchestratorSourceLinesPerTurn {
		return Result{}, t.refuse(SourceBudgetError{Path: args.Path, Lines: lines, Reason: RefusedOverLines})
	}
	result, err := t.tool.Run(ctx, raw)
	if err == nil && result.FailureText == "" {
		t.budget.spent += lines
	}
	return result, err
}
