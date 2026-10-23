package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
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
	RefusedSpentScript    BudgetRefusal = "script"
)

type ScriptSpend struct {
	Command string
	Lines   int
}

type SourceBudgetError struct {
	Tool    string
	Path    string
	Lines   int
	Spent   int
	Spawn   []string
	Reason  BudgetRefusal
	Step    subagent.ShellStep
	Scripts []ScriptSpend
}

func (e SourceBudgetError) Error() string {
	spawn := strings.Join(e.Spawn, " or ")
	switch e.Reason {
	case RefusedShellSource:
		return e.refusedShellWrite("the command writes the source file "+e.Path+" through the shell, where the budget cannot count it", spawn)
	case RefusedUnreadablePath:
		return e.refusedShellWrite("the command writes to "+e.Path+", and which file that is stays unknown until the shell runs, so the budget cannot count it", spawn)
	case RefusedOverLines:
		return fmt.Sprintf("%s: %s is source code, and this change of %d lines would pass the orchestrator's budget of %d changed source lines in one turn, %d of which are spent%s: spawn %s with this work instead, rather than another route to the same file, such as sed, a redirect or a script",
			e.Tool, e.Path, e.Lines, konst.OrchestratorSourceLinesPerTurn, e.Spent, spentByScripts(e.Scripts), spawn)
	case RefusedSpentScript:
		return fmt.Sprintf("%s: nothing ran and no file changed: the command runs a script, and the orchestrator's budget of %d changed source lines in one turn is spent, %d lines%s: spawn %s with this work instead. a script file kept outside the project's source, such as one in an ignored folder like .local or outside the project, still runs",
			e.Tool, konst.OrchestratorSourceLinesPerTurn, e.Spent, spentByScripts(e.Scripts), spawn)
	}
	panic("tofu: unknown source budget refusal " + string(e.Reason))
}

func spentByScripts(scripts []ScriptSpend) string {
	var named []string
	for _, script := range scripts {
		named = append(named, fmt.Sprintf("%d by the script %q", script.Lines, script.Command))
	}
	if len(named) == 0 {
		return ""
	}
	return ", " + strings.Join(named, ", ")
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
	return fmt.Sprintf("one edit call with old_string %q, new_string %q and replace_all true for each file, the rest in edits: %s", old, replacement, strings.Join(named, ", "))
}

func scriptFile(name string) bool {
	return slices.Contains([]string{".py", ".js", ".mjs", ".cjs", ".ts", ".mts", ".sh", ".bash", ".zsh", ".rb", ".pl", ".php", ".lua", ".ps1"}, path.Ext(strings.ToLower(name)))
}

func scriptRunner(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(path.Base(filepath.ToSlash(name))), ".exe")
	return scriptFile(name) || slices.Contains([]string{"python", "py", "pypy", "node", "bun", "deno", "tsx", "ts-node", "bash", "sh", "zsh", "dash", "ksh", "fish", "ruby", "perl", "php", "lua", "pwsh", "powershell", "awk", "gawk"}, strings.TrimRight(name, "0123456789."))
}

func runsScript(step subagent.ShellStep) bool {
	launched := slices.Contains([]string{"env", "xargs", "timeout", "nice", "time", "uv", "uvx", "npx", "bunx"}, step.Program) && slices.ContainsFunc(step.Args, scriptRunner)
	return scriptRunner(step.Program) || launched || step.Program == "go" && len(step.Args) > 0 && step.Args[0] == "run"
}

func (r Root) scriptsOutsideSource(steps []subagent.ShellStep) bool {
	for _, step := range steps {
		if !runsScript(step) {
			continue
		}
		words := append([]string{step.Program}, step.Args...)
		at := slices.IndexFunc(words, scriptFile)
		if at < 0 {
			return false
		}
		script := words[at]
		if !filepath.IsAbs(script) {
			script = filepath.Join(string(r), script)
		}
		relative, err := filepath.Rel(string(r), script)
		if err != nil {
			return false
		}
		outside := relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
		if !outside && exec.Command("git", "-C", string(r), "check-ignore", "-q", filepath.ToSlash(relative)).Run() != nil {
			return false
		}
	}
	return true
}

func generatedFiles(steps []subagent.ShellStep) []string {
	var generated []string
	for _, step := range steps {
		if !shellWriter(step) {
			for _, target := range step.Changes {
				generated = append(generated, filepath.ToSlash(filepath.Clean(target)))
			}
		}
	}
	return generated
}

func (r Root) sources() map[string]string {
	listed, err := r.gitListing()
	if err != nil {
		listed, _ = r.walkListing()
	}
	held := map[string]string{}
	for _, file := range listed {
		if sourceLanguage(file) == "" {
			continue
		}
		if body, err := os.ReadFile(filepath.Join(string(r), file)); err == nil {
			held[file] = string(body)
		}
	}
	return held
}

func changedLines(before, after string) int {
	changed := 0
	for _, hunk := range transform.Hunks(before, after) {
		changed += max(len(hunk.Removed), len(hunk.Added))
	}
	return changed
}

type wroteBySubAgent struct {
	tool   Tool
	writes *subagent.Writes
}

func (t wroteBySubAgent) Name() string { return t.tool.Name() }

func (t wroteBySubAgent) Definition() llm.Tool { return t.tool.Definition() }

func (t wroteBySubAgent) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	paths, _ := TargetPaths(t.Name(), raw)
	t.writes.Wrote(paths)
	defer t.writes.Wrote(paths)
	return t.tool.Run(ctx, raw)
}

func (b *sourceBudget) writtenBySubAgents(paths []string) []string {
	root := b.files.root
	var written []string
	for _, path := range paths {
		resolved, err := root.Resolve(path)
		if err != nil {
			continue
		}
		if relative, err := filepath.Rel(string(root), resolved); err == nil {
			written = append(written, filepath.ToSlash(relative))
		}
	}
	return written
}

func (b *sourceBudget) heldBySubAgents() []string {
	if b.roster == nil {
		return nil
	}
	var owns []string
	for _, agent := range b.roster.SubAgents() {
		if agent.Holds() {
			owns = append(owns, agent.Owns...)
		}
	}
	return owns
}

func changedSince(before, after map[string]string, counted func(file string) bool) int {
	for file := range before {
		if _, kept := after[file]; !kept {
			after[file] = ""
		}
	}
	changed := 0
	for file, body := range after {
		if before[file] != body && counted(file) {
			changed += changedLines(before[file], body)
		}
	}
	return changed
}

type sourceBudget struct {
	spent   int
	scripts []ScriptSpend
	enabled []subagent.Definition
	files   *WriteTool
	writes  *subagent.Writes
	roster  *subagent.Roster
}

func (b *sourceBudget) spendScript(command string, lines int) string {
	if lines == 0 {
		return ""
	}
	first, _, _ := strings.Cut(command, "\n")
	b.spent += lines
	b.scripts = append(b.scripts, ScriptSpend{Command: first, Lines: lines})
	told := fmt.Sprintf("\n\nthis command changed %d source lines, counted against the orchestrator's budget of %d changed source lines in one turn: %d spent", lines, konst.OrchestratorSourceLinesPerTurn, b.spent)
	if b.spent >= konst.OrchestratorSourceLinesPerTurn {
		told += ". the budget is spent: edit, write and scripts that change source are refused for the rest of this turn, so spawn a sub-agent with the rest of the change"
	}
	return told
}

type budgetArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append"`
	Old     string `json:"-"`
	New     string `json:"-"`
	Command string `json:"command"`
}

func (b *sourceBudget) linesChanged(tool string, args budgetArgs) int {
	if tool == "edit" {
		return changedLines(args.Old, args.New)
	}
	whole := linesIn(args.Content)
	if args.Append || b.files == nil {
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
	return changedLines(string(held), args.Content)
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

func (b *sourceBudget) markPrinted(ctx context.Context, command, printed string) {
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
			b.files.ledger.Mark(ctx, path, body)
		}
	}
}

type budgetedTool struct {
	tool   Tool
	budget *sourceBudget
}

func WithSourceBudget(tools []Tool, defined []subagent.Definition) []Tool {
	budget := &sourceBudget{enabled: enabledSubAgents(defined), writes: &subagent.Writes{}}
	wrapped := slices.Clone(tools)
	for i, tool := range wrapped {
		if spawner, spawns := tool.(*SpawnTool); spawns {
			budget.writes, budget.roster = spawner.writes, spawner.roster
		}
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
		if language != "" && definition.Language == language {
			refusal.Spawn = append(refusal.Spawn, definition.Name)
		}
	}
	if len(refusal.Spawn) == 0 {
		refusal.Spawn = []string{"a sub-agent"}
	}
	refusal.Tool, refusal.Spent, refusal.Scripts = t.Name(), t.budget.spent, t.budget.scripts
	return refusal
}

func (t budgetedTool) runShell(ctx context.Context, raw json.RawMessage, command string) (Result, error) {
	if refusal, refused := shellRefusal(command); refused {
		return Result{}, t.refuse(refusal)
	}
	steps := subagent.ShellSteps(command)
	if !slices.ContainsFunc(steps, runsScript) || t.budget.files == nil {
		result, err := t.tool.Run(ctx, raw)
		if err == nil {
			t.budget.markPrinted(ctx, command, result.Content)
		}
		return result, err
	}
	root := t.budget.files.root
	if t.budget.spent >= konst.OrchestratorSourceLinesPerTurn && !root.scriptsOutsideSource(steps) {
		return Result{}, t.refuse(SourceBudgetError{Reason: RefusedSpentScript})
	}
	mark, held := t.budget.writes.Begin(), t.budget.heldBySubAgents()
	before := root.sources()
	result, err := t.tool.Run(ctx, raw)
	after := root.sources()
	held = append(held, t.budget.heldBySubAgents()...)
	uncounted := append(generatedFiles(steps), t.budget.writtenBySubAgents(t.budget.writes.End(mark))...)
	result.Content += t.budget.spendScript(command, changedSince(before, after, func(file string) bool {
		owned, _ := subagent.Matches(file, held)
		return !owned && !slices.Contains(uncounted, file)
	}))
	return result, err
}

func (t budgetedTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args budgetArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return t.tool.Run(ctx, raw)
	}
	if t.Name() == "bash" {
		return t.runShell(ctx, raw, args.Command)
	}
	changes := []budgetArgs{args}
	if t.Name() == "edit" {
		edits, err := EditChanges(raw)
		if err != nil {
			return t.tool.Run(ctx, raw)
		}
		changes = make([]budgetArgs, len(edits))
		for i, edit := range edits {
			changes[i] = budgetArgs{Path: edit.Path, Old: edit.Old, New: edit.New}
		}
	}
	lines := 0
	for _, change := range changes {
		if sourceLanguage(change.Path) == "" {
			continue
		}
		lines += t.budget.linesChanged(t.Name(), change)
		if t.budget.spent+lines > konst.OrchestratorSourceLinesPerTurn {
			return Result{}, t.refuse(SourceBudgetError{Path: change.Path, Lines: lines, Reason: RefusedOverLines})
		}
	}
	result, err := t.tool.Run(ctx, raw)
	if err == nil && result.FailureText == "" {
		t.budget.spent += lines
	}
	return result, err
}
