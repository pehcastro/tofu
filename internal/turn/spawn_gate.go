package turn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/subagent"
)

func gateMissed(project string, recipes map[string]string, definition subagent.Definition, owns []string, rounds []Row) []string {
	if definition.Language == "" {
		return nil
	}
	var sinceEdit []ToolCallRow
	lastEdit := ""
	for _, round := range rounds {
		for _, step := range round.Steps {
			for _, call := range step.ToolCalls {
				sinceEdit = append(sinceEdit, call)
				if path := writtenPath(call); path != "" && rule.LanguageOf(path) == definition.Language {
					sinceEdit, lastEdit = nil, path
				}
			}
		}
	}
	if lastEdit == "" {
		return nil
	}
	if !filepath.IsAbs(lastEdit) {
		lastEdit = filepath.Join(project, lastEdit)
	}
	noTests := false
	if project != "" && slices.Contains(definition.Gate, "test") {
		_, refused := vitestPackage(filepath.Dir(lastEdit))
		noTests = strings.HasPrefix(refused.FailureText, testNoTests)
	}
	var missed []string
	for _, check := range definition.Gate {
		said := check + " did not run"
		if check == "test" && noTests {
			said = ""
		}
		for _, call := range sinceEdit {
			switch {
			case !gateRan(check, call, recipes), call.Error == typecheckUnanswered:
			case checkerMissing(printedBy(rounds, call.Call)):
				said = check + " could not run: its checker is not installed"
			case call.ExitCode != nil && *call.ExitCode != 0:
				said = fmt.Sprintf("%s exited %d", check, *call.ExitCode)
			case call.Tool == "typecheck" && call.Error != "" && !typecheckNamesOwnFile(project, owns, call, rounds):
				said = ""
			case call.Error != "" && !strings.HasPrefix(call.Error, testNoTests):
				said = check + " failed"
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

func printedBy(rounds []Row, call string) string {
	printed := ""
	for _, round := range rounds {
		for _, message := range round.Conversation {
			if message.Role == llm.RoleTool && message.ToolCallID == call {
				printed = message.Content
			}
		}
	}
	return printed
}

func checkerMissing(printed string) bool {
	return slices.ContainsFunc([]string{"No module named", "command not found", "is not recognized"}, func(said string) bool { return strings.Contains(printed, said) })
}

func typecheckNamesOwnFile(project string, owns []string, call ToolCallRow, rounds []Row) bool {
	_, listed, hasList := strings.Cut(printedBy(rounds, call.Call), ":\n")
	scope := call.Command
	if !filepath.IsAbs(scope) {
		scope = filepath.Join(project, scope)
	}
	tsconfig, found := findUp(scope, tsconfigName)
	if len(owns) == 0 || !hasList || !found {
		return true
	}
	for _, line := range strings.Split(listed, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, " ") {
			continue
		}
		path, position, _ := strings.Cut(line, ":")
		if located, _, tsc := strings.Cut(line, "): error TS"); tsc {
			path = located[:max(strings.LastIndex(located, "("), 0)]
		} else if position == "" || position[0] < '0' || position[0] > '9' {
			return true
		}
		relative, err := filepath.Rel(project, filepath.Join(filepath.Dir(tsconfig), path))
		if mine, _ := subagent.Matches(filepath.ToSlash(relative), owns); path == "" || err != nil || mine {
			return true
		}
	}
	return false
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
