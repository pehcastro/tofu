package loop

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/agent/subagents"
	"tofu/internal/librarian/rules"
	"tofu/internal/models/llm/core"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/tools/code/test"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
)

func editedSince(project string, definition subagents.Definition, rounds []Row) ([]ToolCallRow, string) {
	var sinceEdit []ToolCallRow
	lastEdit := ""
	for _, round := range rounds {
		for _, step := range round.Steps {
			for _, call := range step.ToolCalls {
				sinceEdit = append(sinceEdit, call)
				if path := writtenPath(call); path != "" && definition.Language != "" && rules.LanguageOf(path) == definition.Language {
					sinceEdit, lastEdit = nil, path
				}
			}
		}
	}
	if lastEdit != "" && !filepath.IsAbs(lastEdit) {
		lastEdit = filepath.Join(project, lastEdit)
	}
	return sinceEdit, lastEdit
}

func gateExemptions(project string, definition subagents.Definition, rounds []Row) map[string]string {
	sinceEdit, lastEdit := editedSince(project, definition, rounds)
	exempt := map[string]string{}
	if lastEdit == "" {
		return exempt
	}
	for _, check := range []string{"test", "typecheck"} {
		last := ""
		for _, call := range sinceEdit {
			if call.Tool == check && call.Error != checkers.TypecheckUnanswered {
				last = call.Error
			}
		}
		if _, skipped := skipReason(last); skipped && slices.Contains(definition.Gate, check) {
			exempt[check] = last
		}
	}
	if project == "" {
		return exempt
	}
	if slices.Contains(definition.Gate, "test") {
		if _, refused := test.VitestPackage(filepath.Dir(lastEdit)); strings.HasPrefix(refused.FailureText, test.NoTests) {
			exempt["test"] = refused.FailureText
		}
	}
	if manifest, found := contract.FindUp(filepath.Dir(lastEdit), "Cargo.toml"); found && slices.Contains(definition.Gate, "cargo test") && !crateHoldsTests(filepath.Dir(manifest)) {
		named, _ := filepath.Rel(project, manifest)
		exempt["cargo test"] = "cargo test is not needed: the crate of " + filepath.ToSlash(named) + " has no #[test] and no tests directory, so there is nothing to run"
	}
	return exempt
}

func crateHoldsTests(crate string) bool {
	if info, err := os.Stat(filepath.Join(crate, "tests")); err == nil && info.IsDir() {
		return true
	}
	found := false
	_ = filepath.WalkDir(crate, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case entry.IsDir() && path != crate && (entry.Name() == "target" || strings.HasPrefix(entry.Name(), ".")):
			return filepath.SkipDir
		case entry.IsDir() || filepath.Ext(path) != ".rs":
			return nil
		}
		body, _ := os.ReadFile(path)
		if found = rustTestAttribute().Match(body); found {
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func ranNoTests(printed string) bool {
	counts := cargoRunning().FindAllStringSubmatch(printed, -1)
	return len(counts) > 0 && !slices.ContainsFunc(counts, func(count []string) bool { return count[1] != "0" })
}

func cargoFindings(project, lastEdit string, owns []string, printed string) (dead []string, foreign bool) {
	type diagnostic struct{ said, path, line string }
	var found []diagnostic
	for _, line := range strings.Split(strings.ReplaceAll(printed, "\r", ""), "\n") {
		if header := cargoHeader().FindStringSubmatch(line); header != nil && !cargoSummary().MatchString(line) {
			found = append(found, diagnostic{said: header[3]})
		} else if at := cargoLocation().FindStringSubmatch(line); at != nil && len(found) > 0 && found[len(found)-1].path == "" {
			found[len(found)-1].path, found[len(found)-1].line = at[1], at[2]
		}
	}
	foreign = len(found) > 0
	for _, each := range found {
		mine := cargoOwned(project, lastEdit, owns, each.path)
		foreign = foreign && !mine
		if mine && each.path != "" && cargoDeadCode().MatchString(each.said) {
			dead = append(dead, each.said+" at "+each.path+":"+each.line)
		}
	}
	return dead, foreign
}

func cargoOwned(project, lastEdit string, owns []string, printedPath string) bool {
	if len(owns) == 0 || printedPath == "" {
		return true
	}
	path, found := filepath.FromSlash(strings.ReplaceAll(printedPath, `\`, "/")), true
	if !filepath.IsAbs(path) {
		path, found = contract.FindUp(filepath.Dir(lastEdit), path)
	}
	relative, err := filepath.Rel(project, path)
	if !found || err != nil {
		return true
	}
	mine, _ := subagents.Matches(filepath.ToSlash(relative), owns)
	return mine
}

func gateMissed(project string, recipes map[string]string, definition subagents.Definition, owns []string, rounds []Row) []string {
	sinceEdit, lastEdit := editedSince(project, definition, rounds)
	if lastEdit == "" {
		return nil
	}
	exempt := gateExemptions(project, definition, rounds)
	var missed []string
	for _, check := range definition.Gate {
		if _, skipped := exempt[check]; skipped {
			continue
		}
		said := check + " did not run"
		for _, call := range sinceEdit {
			if !gateRan(check, call, recipes) || call.Error == checkers.TypecheckUnanswered {
				continue
			}
			printed := printedBy(rounds, call.Call)
			chained := strings.ContainsAny(call.Command, ";|&\n")
			verdict := checkPrinted(check, printed, call.ExitCode)
			cargo := strings.HasPrefix(check, "cargo ")
			dead, foreign := cargoFindings(project, lastEdit, owns, printed)
			line, mine := errorLine(printed), true
			if call.Tool == "typecheck" && call.Error != "" {
				line, mine = typecheckOwnError(project, owns, printed, call.Command)
			}
			quoted := ""
			if line != "" {
				quoted = ": " + line
			}
			switch {
			case checkerMissing(printed):
				said = check + " could not run: its checker is not installed"
			case cargo && len(dead) > 0:
				said = check + " warned " + strings.Join(dead, "; ") + ": new code nothing calls is unfinished wiring"
			case check == "cargo test" && ranNoTests(printed):
				said = "cargo test ran 0 tests: every target it built printed running 0 tests, so it proved nothing"
			case cargo && foreign:
				said = ""
			case chained && verdict == printedFailed:
				said = check + " failed" + quoted
			case chained && verdict == printedPassed:
				said = ""
			case call.ExitCode != nil && *call.ExitCode != 0:
				said = fmt.Sprintf("%s exited %d%s", check, *call.ExitCode, quoted)
			case !mine:
				said = ""
			case call.Error != "" && !strings.HasPrefix(call.Error, test.NoTests):
				said = check + " failed" + quoted
			default:
				said = ""
			}
		}
		if said != "" {
			missed = append(missed, said)
		}
	}
	return missed
}

type printedVerdict int

const (
	printedNothing printedVerdict = iota
	printedPassed
	printedFailed
)

var (
	goDiagnostic = platformfs.LazyRegexp(`(?m)^\S+\.go:\d+:\d+: `)
	goTestFailed = platformfs.LazyRegexp(`(?m)^(FAIL\b|--- FAIL|panic: |\s*\[FAIL\]|Go test: .*\bfailed\b)`)
	goTestPassed = platformfs.LazyRegexp(`(?m)^(ok\s+\S|\?\s+\S+\s+\[no test files\]|Go test: \d+ passed in )`)

	rustTestAttribute = platformfs.LazyRegexp(`#\[(\w+::)*test\b`)
	cargoRunning      = platformfs.LazyRegexp(`(?m)^running (\d+) tests?\r?$`)
	cargoHeader       = platformfs.LazyRegexp(`^(warning|error)(\[\w+\])?: (.+)$`)
	cargoSummary      = platformfs.LazyRegexp("^(warning|error): (`[^`]+` \\(.+\\) generated|could not compile|build failed|aborting due to|\\d+ warnings? emitted)")
	cargoLocation     = platformfs.LazyRegexp(`^\s*--> (.+):(\d+):\d+$`)
	cargoDeadCode     = platformfs.LazyRegexp(`\b(is|are) never (used|read|constructed)\b|^unused\b`)
	cargoTestFailed   = platformfs.LazyRegexp(`(?m)^(test result: FAILED|error(\[\w+\])?: )`)
	cargoTestPassed   = platformfs.LazyRegexp(`(?m)^test result: ok\.`)

	errorPrinted = platformfs.LazyRegexp(`^(error(\[\w+\])?: |--- FAIL|FAIL(ED)?\b|\[FAIL\]|panic: |fatal: |\S+\.go:\d+:\d+: |test \S+ \.\.\. FAILED$|test result: FAILED|thread '.+' panicked at |.*\berror TS\d+|Traceback |E {3}|\w+Error: )`)
)

func checkPrinted(check, printed string, exit *int) printedVerdict {
	tested := goTestFailed().MatchString(printed) || goTestPassed().MatchString(printed)
	switch {
	case check == "go test" && goTestFailed().MatchString(printed), check == "go vet" && goDiagnostic().MatchString(printed),
		check == "cargo test" && cargoTestFailed().MatchString(printed):
		return printedFailed
	case check == "go test" && tested, check == "go vet" && (tested || exit != nil && *exit == 0),
		check == "cargo test" && cargoTestPassed().MatchString(printed):
		return printedPassed
	}
	return printedNothing
}

func errorLine(printed string) string {
	for _, line := range strings.Split(printed, "\n") {
		if line = strings.TrimSpace(line); errorPrinted().MatchString(line) && !cargoSummary().MatchString(line) {
			return line
		}
	}
	return ""
}

func printedBy(rounds []Row, call string) string {
	printed := ""
	for _, round := range rounds {
		for _, message := range round.Conversation {
			if message.Role == core.RoleTool && message.ToolCallID == call {
				printed = message.Content
			}
		}
	}
	return printed
}

func skipReason(failure string) (string, bool) {
	for _, tool := range []string{"test", "typecheck"} {
		if reason, skipped := strings.CutPrefix(failure, tool+" skipped: "); skipped {
			return reason, true
		}
	}
	return "", false
}

func checkerMissing(printed string) bool {
	return slices.ContainsFunc([]string{"No module named", "command not found", "is not recognized"}, func(said string) bool { return strings.Contains(printed, said) })
}

func typecheckOwnError(project string, owns []string, printed, scope string) (quoted string, mine bool) {
	_, listed, hasList := strings.Cut(printed, ":\n")
	if !filepath.IsAbs(scope) {
		scope = filepath.Join(project, scope)
	}
	tsconfig, found := contract.FindUp(scope, checkers.TsconfigName)
	if len(owns) == 0 || !hasList || !found {
		return errorLine(printed), true
	}
	var unplaced []string
	for _, line := range strings.Split(listed, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, " ") {
			continue
		}
		path, position, _ := strings.Cut(line, ":")
		if located, _, tsc := strings.Cut(line, "): error TS"); tsc {
			path = located[:max(strings.LastIndex(located, "("), 0)]
		} else if position == "" || position[0] < '0' || position[0] > '9' {
			unplaced = append(unplaced, line)
			continue
		}
		relative, err := filepath.Rel(project, filepath.Join(filepath.Dir(tsconfig), path))
		if owned, _ := subagents.Matches(filepath.ToSlash(relative), owns); path == "" || err != nil {
			unplaced = append(unplaced, line)
		} else if owned {
			return line, true
		}
	}
	return errorLine(strings.Join(unplaced, "\n")), len(unplaced) > 0
}

func gateRan(check string, call ToolCallRow, recipes map[string]string) bool {
	switch {
	case call.Tool == check:
		return true
	case call.Tool != "bash":
		return false
	}
	toolCheck := check == "test" || check == "typecheck"
	if !toolCheck && commandRuns(call.Command, check) {
		return true
	}
	for invocation, recipe := range recipes {
		meetsCheck := commandRuns(recipe, check)
		if toolCheck {
			meetsCheck = strings.HasSuffix(invocation, " "+check)
		}
		if meetsCheck && commandRuns(call.Command, invocation) {
			return true
		}
	}
	return false
}

func commandRuns(command, check string) bool {
	want := strings.Fields(check)
	for _, segment := range strings.FieldsFunc(command, func(r rune) bool { return r == '&' || r == '|' || r == ';' }) {
		words := strings.Fields(segment)
		for len(words) > 0 && strings.Contains(words[0], "=") {
			words = words[1:]
		}
		for stripped := true; stripped; words, stripped = withoutRunner(words) {
			prefixed := len(words) >= len(want) && slices.Equal(words[:len(want)], want)
			made := len(want) == 2 && want[0] == "make" && len(words) > 1 && words[0] == "make" && slices.Contains(words[1:], want[1])
			if prefixed || made {
				return true
			}
		}
	}
	return false
}

func withoutRunner(words []string) ([]string, bool) {
	if len(words) > 1 && words[0] == "cargo" && strings.HasPrefix(words[1], "+") {
		return append([]string{"cargo"}, words[2:]...), true
	}
	for _, runner := range []string{"uv run", "poetry run", "pdm run", "hatch run", "rye run", "pipenv run", "python -m", "python3 -m", "py -m", "npx", "pnpm exec", "pnpm dlx", "bunx", "yarn", "rtk proxy", "rtk"} {
		prefix := strings.Fields(runner)
		if len(words) <= len(prefix) || !slices.Equal(words[:len(prefix)], prefix) {
			continue
		}
		rest := words[len(prefix):]
		for len(rest) > 1 && strings.HasPrefix(rest[0], "-") {
			rest = rest[1:]
		}
		return rest, true
	}
	return nil, false
}

func projectRecipes(project string) map[string]string {
	if project == "" {
		return nil
	}
	recipes := map[string]string{}
	add := func(invokers []string, name, command string) {
		for _, invoker := range invokers {
			recipes[invoker+" "+name] += command + " ; "
		}
	}
	read := func(file string) string {
		raw, _ := os.ReadFile(filepath.Join(project, file))
		return strings.ReplaceAll(string(raw), "\r", "")
	}
	var targets []string
	for _, line := range strings.Split(read("Makefile"), "\n") {
		names, _, isRule := strings.Cut(line, ":")
		switch {
		case strings.HasPrefix(line, "\t"):
			for _, target := range targets {
				add([]string{"make"}, target, strings.TrimLeft(strings.TrimSpace(line), "@-+"))
			}
		case isRule && !strings.Contains(names, "=") && !strings.HasPrefix(line[len(names)+1:], "="):
			targets = strings.Fields(names)
		default:
			targets = nil
		}
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal([]byte(read("package.json")), &pkg) == nil {
		for name, command := range pkg.Scripts {
			add([]string{"npm run", "npm", "pnpm run", "pnpm", "yarn", "bun run"}, name, command)
		}
	}
	scripts := false
	for _, line := range strings.Split(read("pyproject.toml"), "\n") {
		line = strings.TrimSpace(line)
		name, command, entry := strings.Cut(line, "=")
		switch {
		case strings.HasPrefix(line, "["):
			scripts = line == "[project.scripts]" || strings.HasPrefix(line, "[tool.") && strings.HasSuffix(line, ".scripts]")
		case scripts && entry:
			add([]string{"uv run", "poetry run", "pdm run", "hatch run", "rye run", "pipenv run"}, strings.TrimSpace(name), strings.Trim(strings.TrimSpace(command), `"'[]`))
		}
	}
	return recipes
}
