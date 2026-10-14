package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/subagent"
)

type BudgetRefusal string

const (
	RefusedShellSource BudgetRefusal = "shell"
	RefusedSecondSlice BudgetRefusal = "slice"
	RefusedOverLines   BudgetRefusal = "lines"
)

type SourceBudgetError struct {
	Tool   string
	Path   string
	Lines  int
	Spent  int
	Slice  string
	Spawn  []string
	Reason BudgetRefusal
}

func (e SourceBudgetError) Error() string {
	spawn := strings.Join(e.Spawn, " or ")
	switch e.Reason {
	case RefusedShellSource:
		return fmt.Sprintf("%s: %s is source code written by hand through the shell, which the orchestrator never does: use edit or write within your budget of %d lines in one directory, or spawn %s. a rename is edit with replace_all in each file, and a program's own output redirected into a file, such as a generator or a schema printer, is not counted and runs",
			e.Tool, e.Path, konst.OrchestratorSourceLinesPerTurn, spawn)
	case RefusedSecondSlice:
		return fmt.Sprintf("%s: %s is source in a second directory, and this turn already changed source in %s: work in one directory is yours, a second is a separate piece, so spawn %s with it",
			e.Tool, e.Path, e.Slice, spawn)
	case RefusedOverLines:
		return fmt.Sprintf("%s: %s is source code, and this change of %d lines would pass the orchestrator's budget of %d changed source lines in one turn, %d of which are spent: spawn %s with this work instead",
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
	slice   string
	enabled []subagent.Definition
}

type budgetedTool struct {
	tool   Tool
	budget *sourceBudget
}

func WithSourceBudget(tools []Tool, defined []subagent.Definition) []Tool {
	budget := &sourceBudget{enabled: enabledSubAgents(defined)}
	wrapped := slices.Clone(tools)
	for i, tool := range wrapped {
		if slices.Contains([]string{"write", "edit", "bash"}, tool.Name()) {
			wrapped[i] = budgetedTool{tool: tool, budget: budget}
		}
	}
	return wrapped
}

func (t budgetedTool) Name() string { return t.tool.Name() }

func (t budgetedTool) Definition() llm.Tool {
	definition := t.tool.Definition()
	definition.Description += fmt.Sprintf(". as the orchestrator you change source code yourself, through write and edit, when the work stays in one directory and within %d changed lines in one turn: "+
		"do that work without spawning. source in a second directory, or past the lines, is refused and goes to a sub-agent. never write source by hand through the shell, sed -i and a heredoc included: "+
		"a rename is edit with replace_all in each file, and a program's own output redirected into a file is not counted. an exact edit of a small file applies without a read, so edit straight from what search or a shell command showed you, every file in one call. "+
		"markdown, notes, plans, configuration and data are free", konst.OrchestratorSourceLinesPerTurn)
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
	refusal.Tool, refusal.Spent, refusal.Slice = t.Name(), t.budget.spent, t.budget.slice
	return refusal
}

func (t budgetedTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Old     string `json:"old_string"`
		New     string `json:"new_string"`
		Command string `json:"command"`
	}
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
	}
	if sourceLanguage(args.Path) == "" {
		return t.tool.Run(ctx, raw)
	}
	lines := max(linesIn(args.Content), linesIn(args.Old), linesIn(args.New))
	slice, err := filepath.Abs(filepath.Dir(args.Path))
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
	}
	switch {
	case t.budget.slice != "" && t.budget.slice != slice:
		return Result{}, t.refuse(SourceBudgetError{Path: args.Path, Lines: lines, Reason: RefusedSecondSlice})
	case t.budget.spent+lines > konst.OrchestratorSourceLinesPerTurn:
		return Result{}, t.refuse(SourceBudgetError{Path: args.Path, Lines: lines, Reason: RefusedOverLines})
	}
	result, err := t.tool.Run(ctx, raw)
	if err == nil && result.FailureText == "" {
		t.budget.spent, t.budget.slice = t.budget.spent+lines, slice
	}
	return result, err
}
