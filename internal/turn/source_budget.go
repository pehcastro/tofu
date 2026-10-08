package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/subagent"
)

type SourceBudgetError struct {
	Tool       string
	Path       string
	Lines      int
	Spent      int
	Spawn      []string
	ShellWrite bool
}

func (e SourceBudgetError) Error() string {
	if e.ShellWrite {
		return fmt.Sprintf("%s: %s is source code, and the orchestrator never writes source through the shell: use edit for a fix of up to %d lines, or spawn %s with this work",
			e.Tool, e.Path, konst.OrchestratorSourceLinesPerTurn, strings.Join(e.Spawn, " or "))
	}
	return fmt.Sprintf("%s: %s is source code, and this change of %d lines would pass the orchestrator's budget of %d changed source lines in one turn, %d of which are spent: spawn %s with this work instead",
		e.Tool, e.Path, e.Lines, konst.OrchestratorSourceLinesPerTurn, e.Spent, strings.Join(e.Spawn, " or "))
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

type sourceBudget struct {
	spent   int
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
	definition.Description += fmt.Sprintf(". as the orchestrator you change at most %d lines of source code in one turn, through write and edit only, and never write source through the shell; "+
		"markdown, notes, plans, configuration and data are free, and past the budget a source write is refused and goes to a sub-agent", konst.OrchestratorSourceLinesPerTurn)
	return definition
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
	lines, shellWrite := max(linesIn(args.Content), linesIn(args.Old), linesIn(args.New)), false
	if t.Name() == "bash" {
		written, err := subagent.ShellWrites(args.Command)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
		}
		for _, path := range written {
			if sourceLanguage(path) != "" {
				args.Path, shellWrite = path, true
			}
		}
	}
	language := sourceLanguage(args.Path)
	if language == "" {
		return t.tool.Run(ctx, raw)
	}
	if shellWrite || t.budget.spent+lines > konst.OrchestratorSourceLinesPerTurn {
		refusal := SourceBudgetError{Tool: t.Name(), Path: args.Path, Lines: lines, Spent: t.budget.spent, ShellWrite: shellWrite}
		for _, definition := range t.budget.enabled {
			if definition.Language == language {
				refusal.Spawn = append(refusal.Spawn, definition.Name)
			}
		}
		if len(refusal.Spawn) == 0 {
			refusal.Spawn = []string{"a sub-agent"}
		}
		return Result{}, refusal
	}
	result, err := t.tool.Run(ctx, raw)
	if err == nil && result.FailureText == "" {
		t.budget.spent += lines
	}
	return result, err
}
