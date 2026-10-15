package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/subagent"
	"tofu/internal/transform"
)

type BudgetRefusal string

const (
	RefusedShellSource    BudgetRefusal = "shell"
	RefusedUnreadablePath BudgetRefusal = "unreadable"
	RefusedOverLines      BudgetRefusal = "lines"
)

type SourceBudgetError struct {
	Tool   string
	Path   string
	Lines  int
	Spent  int
	Spawn  []string
	Reason BudgetRefusal
	Step   subagent.ShellStep
}

func (e SourceBudgetError) Error() string {
	spawn := strings.Join(e.Spawn, " or ")
	switch e.Reason {
	case RefusedShellSource:
		return e.refusedShellWrite("the command writes the source file "+e.Path+" through the shell, where the budget cannot count it", spawn)
	case RefusedUnreadablePath:
		return e.refusedShellWrite("the command writes to "+e.Path+", and which file that is stays unknown until the shell runs, so the budget cannot count it", spawn)
	case RefusedOverLines:
		return fmt.Sprintf("%s: %s is source code, and this change of %d lines would pass the orchestrator's budget of %d changed source lines in one turn, %d of which are spent: spawn %s with this work instead, rather than another route to the same file, such as sed or a redirect",
			e.Tool, e.Path, e.Lines, konst.OrchestratorSourceLinesPerTurn, e.Spent, spawn)
	}
	panic("tofu: unknown source budget refusal " + string(e.Reason))
}

func (e SourceBudgetError) refusedShellWrite(why, spawn string) string {
	return fmt.Sprintf("%s: nothing ran and no file changed: %s. the same change is %s. send it in your next reply together with every other change the task needs. edit and write spend up to %d changed source lines a turn; past that, spawn %s with the whole change. a program's own output redirected into a file, such as a generator or a schema printer, is not counted and runs",
		e.Tool, why, sameChangeByTool(e.Step, e.Path), konst.OrchestratorSourceLinesPerTurn, spawn)
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

func shellWriter(step subagent.ShellStep) bool {
	return slices.Contains([]string{"", "echo", "printf", "cat", "tee", "sed", "perl", "cp", "mv"}, step.Program)
}

func shellRefusal(command string) (SourceBudgetError, bool) {
	steps := subagent.ShellSteps(command)
	if _, err := subagent.ShellWrites(command); err != nil {
		var unreadable subagent.UnparseablePathError
		errors.As(err, &unreadable)
		refusal := SourceBudgetError{Path: unreadable.Path, Reason: RefusedUnreadablePath}
		if at := slices.IndexFunc(steps, func(step subagent.ShellStep) bool { return slices.Contains(step.Changes, unreadable.Path) }); at >= 0 {
			refusal.Step = steps[at]
		}
		return refusal, true
	}
	for _, step := range steps {
		if at := slices.IndexFunc(step.Changes, func(path string) bool { return sourceLanguage(path) != "" }); at >= 0 && shellWriter(step) {
			return SourceBudgetError{Path: step.Changes[at], Reason: RefusedShellSource, Step: step}, true
		}
	}
	return SourceBudgetError{}, false
}

func substitution(step subagent.ShellStep) (string, string, []string, bool) {
	if step.Program != "sed" && step.Program != "perl" {
		return "", "", nil, false
	}
	for i, arg := range step.Args {
		if len(arg) < 4 || arg[0] != 's' || !unicode.IsPunct(rune(arg[1])) && !unicode.IsSymbol(rune(arg[1])) || arg[1] == '\\' {
			continue
		}
		parts := strings.Split(arg[2:], arg[1:2])
		if len(parts) != 3 {
			continue
		}
		files := slices.DeleteFunc(slices.Clone(step.Args[i+1:]), func(file string) bool { return strings.HasPrefix(file, "-") })
		return parts[0], parts[1], files, true
	}
	return "", "", nil, false
}

func sameChangeByTool(step subagent.ShellStep, path string) string {
	if !shellWriter(step) {
		return "that command with every file it writes named literally, or edit and write calls for source"
	}
	old, replacement, files, renames := substitution(step)
	if !renames {
		return fmt.Sprintf("write with path %q and the whole file as content, or edit for a change inside a file that exists", path)
	}
	if len(files) == 0 {
		files = []string{path}
	}
	var named []string
	for _, file := range files {
		switch {
		case strings.ContainsAny(file, "$`"):
			named = append(named, "each file the command would have reached")
		case strings.ContainsAny(file, "*?["):
			named = append(named, "each file "+file+" matches")
		default:
			named = append(named, strconv.Quote(file))
		}
	}
	return fmt.Sprintf("edit with old_string %q, new_string %q and replace_all true, one call per file: %s", old, replacement, strings.Join(named, ", "))
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
		if refusal, refused := shellRefusal(args.Command); refused {
			return Result{}, t.refuse(refusal)
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
