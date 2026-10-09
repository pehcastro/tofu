package subagent

import (
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type Grammar int

const (
	POSIX Grammar = iota
	PowerShell
)

func GrammarOf(shellPath string) Grammar {
	if name := toolName(cleanedField(shellPath)); name == "pwsh" || name == "powershell" {
		return PowerShell
	}
	return POSIX
}

type ReadListError struct {
	Program string
	Why     string
}

func (e ReadListError) Error() string {
	return e.Program + " " + e.Why + ". Outside the paths you hold, bash runs only commands that read, list, search or inspect, or run the project's own checks: a file you hold changes with write or edit, and anything else goes in your report"
}

func (b *Boundary) HoldsTheTree() bool {
	return slices.Contains(b.Owns(), "**")
}

func (b *Boundary) Bash(command string, grammar Grammar) error {
	if b.HoldsTheTree() {
		return b.Shell(command)
	}
	if grammar == PowerShell {
		if strings.Contains(command, "{") || strings.Contains(command, "<#") {
			return ReadListError{Program: "this command", Why: "has a PowerShell script block or block comment, which this check cannot see into"}
		}
		command = strings.ReplaceAll(command, `\`, "/")
	}
	commands := shellCommands(command)
	left := false
	for _, step := range commands {
		if err := step.treeWide(); err != nil {
			return err
		}
		if err := grammar.reads(step, left); err != nil {
			return err
		}
		left = left || step.leaves()
	}
	return b.writes(commands, grammar.changes, grammar.devices())
}

func (g Grammar) devices() []string {
	switch {
	case g == POSIX:
		return []string{"/dev/null", "/dev/stdout", "/dev/stderr"}
	case runtime.GOOS == "windows":
		return []string{"/dev/null", "$null", "nul"}
	}
	return []string{"/dev/null", "$null"}
}

func (g Grammar) changes(c simpleCommand) []shellWord {
	tool, args := c.named()
	changed := append(c.targets(), c.relinks()...)
	if tool == "touch" || tool == "mkdir" || g == PowerShell && tool == "cp" {
		changed = append(changed, operands(args)...)
	}
	return changed
}

func (c simpleCommand) leaves() bool {
	tool, args := c.named()
	found := operands(args)
	return tool == "cd" && (len(found) != 1 || found[0].expands || leavesTheProject(found[0].text))
}

func leavesTheProject(arg string) bool {
	value := arg
	if strings.HasPrefix(arg, "-") {
		_, value, _ = strings.Cut(arg, "=")
		if !strings.Contains(arg, "=") && !strings.HasPrefix(arg, "--") {
			value = arg[min(2, len(arg)):]
		}
	}
	named := path.Clean(strings.ReplaceAll(value, `\`, "/"))
	return path.IsAbs(named) || filepath.VolumeName(value) != "" || strings.HasPrefix(named, "~") ||
		named == ".." || strings.HasPrefix(named, "../") || named == ".tofu" || strings.HasPrefix(named, ".tofu/")
}

func bare(program shellWord) error {
	if program.expands || strings.ContainsAny(program.text, `/\`) {
		return ReadListError{Program: program.text, Why: "is not a bare program name, so what it runs cannot be known"}
	}
	return nil
}

func (g Grammar) reads(c simpleCommand, left bool) error {
	words := c.words
	for len(words) > 0 && shellAssignment.MatchString(words[0].text) {
		name, _, _ := strings.Cut(words[0].text, "=")
		if !slices.Contains([]string{"CI", "NO_COLOR", "FORCE_COLOR", "TERM", "TZ", "LANG", "LC_ALL", "GOMAXPROCS", "GOOS", "GOARCH",
			"CGO_ENABLED", "RUST_BACKTRACE", "RUST_LOG", "NODE_ENV", "PYTHONDONTWRITEBYTECODE", "PYTHONUNBUFFERED"}, name) {
			return ReadListError{Program: name, Why: "is set before the command, and a variable can change what a program runs"}
		}
		words = words[1:]
	}
	if len(words) == 0 {
		return nil
	}
	if slices.ContainsFunc(c.words, func(word shellWord) bool { return word.runs }) {
		return ReadListError{Program: words[0].text, Why: "has $(...), backticks or a ${...} beyond a plain name in an argument, which runs a command this check never sees"}
	}
	if err := bare(words[0]); err != nil {
		return err
	}
	tool := toolName(words[0].text)
	runs, err := g.readOnly(tool, words[1:])
	if err != nil || !runs {
		return err
	}
	if left {
		return ReadListError{Program: tool, Why: "runs after a cd out of the project, where it could run code this check never read"}
	}
	for _, arg := range words[1:] {
		if leavesTheProject(arg.text) {
			return ReadListError{Program: tool, Why: "names " + arg.text + ", outside the project or in a scratch folder, where it could run code this check never read"}
		}
	}
	return nil
}

func runnerWrites() []string {
	return []string{"-u", "-w", "-o", "--update", "--updateSnapshot", "--update-snapshots", "--fix", "--fix-only", "--unsafe-fixes",
		"--write", "--output", "--output-file", "--outputFile", "--junitxml", "--junit-xml", "--add-noqa", "--basetemp", "--config", "-Z", "--script-shell"}
}

func checkName(name string) bool {
	return slices.ContainsFunc([]string{"test", "check", "lint", "typecheck", "vet"}, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}

func first(args []shellWord) string {
	if len(args) == 0 {
		return ""
	}
	return args[0].text
}

func given(args []shellWord, flags ...string) bool {
	return slices.ContainsFunc(args, func(arg shellWord) bool { return slices.Contains(flags, arg.text) })
}

func (g Grammar) readOnly(tool string, args []shellWord) (bool, error) {
	switch tool {
	case "ls", "cat", "head", "tail", "wc", "grep", "egrep", "fgrep", "diff", "cmp", "stat", "du", "df", "pwd",
		"echo", "printf", "true", "false", "test", "[", "which", "basename", "dirname", "realpath", "readlink",
		"cut", "tr", "nl", "od", "hexdump", "sha256sum", "sha1sum", "md5sum", "uname", "whoami", "jq", "sleep", "cd",
		"tee", "rm", "rmdir", "unlink", "mkdir", "touch", "mv",
		"get-childitem", "gci", "dir", "get-content", "gc", "type", "select-string", "sls", "get-item", "gi", "test-path",
		"resolve-path", "get-location", "measure-object", "measure", "sort-object", "select-object", "format-table", "ft",
		"format-list", "fl", "out-string", "write-output", "write-host", "get-filehash", "compare-object", "get-command", "gcm":
		return false, nil
	case "cp":
		return false, g.refuses(tool, args, "-s", "-l", "--link", "--symbolic-link")
	case "find":
		return false, g.refuses(tool, args, "-exec", "-execdir", "-ok", "-okdir", "-delete", "-fprint", "-fprint0", "-fprintf", "-fls")
	case "rg":
		return false, g.refuses(tool, args, "--pre")
	case "sort":
		return false, g.refuses(tool, args, "-o", "--output", "--compress-program")
	case "uniq":
		if len(operands(args)) > 1 {
			return false, ReadListError{Program: tool, Why: "with a second file writes to it"}
		}
		return false, nil
	case "sed":
		return false, sedReads(args)
	case "git":
		return true, g.gitReads(args)
	case "go":
		return true, goChecks(args)
	case "rtk":
		if first(args) == "proxy" {
			args = args[1:]
		}
		return g.wrapped(tool, args)
	case "uv":
		if first(args) != "run" {
			return true, ReadListError{Program: tool, Why: "runs here only as uv run around a check"}
		}
		args = args[1:]
		for slices.Contains([]string{"--frozen", "--locked", "--offline", "--no-sync", "-q", "--quiet"}, first(args)) {
			args = args[1:]
		}
		return g.wrapped(tool, args)
	case "python", "python3", "py":
		if first(args) != "-m" || !slices.Contains([]string{"pytest", "mypy", "ruff", "unittest"}, first(args[1:])) {
			return true, ReadListError{Program: tool, Why: "runs here only as -m pytest, mypy, ruff or unittest, never inline code or a script"}
		}
		return g.wrapped(tool, args[1:])
	case "npx", "pnpx", "bunx":
		if !slices.Contains([]string{"tsc", "vue-tsc", "svelte-check", "vitest", "jest", "eslint", "prettier"}, first(args)) {
			return true, ReadListError{Program: tool, Why: "runs here only a type checker, test runner or linter"}
		}
		return g.wrapped(tool, args)
	case "cargo":
		if strings.HasPrefix(first(args), "+") {
			args = args[1:]
		}
		if !slices.Contains([]string{"test", "check", "clippy", "tree", "metadata"}, first(args)) && (first(args) != "fmt" || !given(args, "--check")) {
			return true, ReadListError{Program: tool, Why: "runs here only as cargo test, check, clippy, tree, metadata or fmt --check"}
		}
		return true, g.refuses(tool, args, runnerWrites()...)
	case "npm", "pnpm", "yarn", "bun":
		script := first(args)
		if script == "run" || script == "run-script" {
			script = first(args[1:])
		}
		if script != "t" && !checkName(script) {
			return true, ReadListError{Program: tool, Why: "runs here only test, and scripts named test, check, lint, typecheck or vet"}
		}
		return true, g.refuses(tool, args, runnerWrites()...)
	case "make":
		targets := operands(args)
		if len(targets) == 0 || slices.ContainsFunc(targets, func(target shellWord) bool { return !checkName(target.text) }) ||
			slices.ContainsFunc(args, func(arg shellWord) bool { return strings.Contains(arg.text, "=") }) {
			return true, ReadListError{Program: tool, Why: "runs here only named targets starting test, check, lint, typecheck or vet, with no variable set"}
		}
		return true, g.refuses(tool, args, "-f", "--file", "--makefile", "-e", "--environment-overrides", "--eval")
	case "pytest", "unittest", "mypy", "svelte-check", "vitest", "jest", "eslint":
		return true, g.refuses(tool, args, runnerWrites()...)
	case "tsc", "vue-tsc":
		if !given(args, "--noEmit") {
			return true, ReadListError{Program: tool, Why: "without --noEmit writes JavaScript into the project"}
		}
		return true, g.refuses(tool, args, runnerWrites()...)
	case "prettier":
		if !given(args, "--check", "-c", "--list-different", "-l") {
			return true, ReadListError{Program: tool, Why: "without --check rewrites files"}
		}
		return true, g.refuses(tool, args, runnerWrites()...)
	case "ruff":
		if first(args) != "check" && (first(args) != "format" || !given(args, "--check", "--diff")) {
			return true, ReadListError{Program: tool, Why: "runs here only as ruff check, or ruff format --check"}
		}
		return true, g.refuses(tool, args, runnerWrites()...)
	}
	return false, ReadListError{Program: tool, Why: "is not on the read list"}
}

func (g Grammar) wrapped(wrapper string, args []shellWord) (bool, error) {
	if len(args) == 0 {
		return true, ReadListError{Program: wrapper, Why: "names no command to run"}
	}
	if err := bare(args[0]); err != nil {
		return true, err
	}
	_, err := g.readOnly(toolName(args[0].text), args[1:])
	return true, err
}

func (g Grammar) refuses(tool string, args []shellWord, flags ...string) error {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg.text, "=")
		clustered := g == POSIX && len(name) > 2 && name[0] == '-' && name[1] != '-' &&
			slices.ContainsFunc(flags, func(flag string) bool { return len(flag) == 2 && strings.Contains(name[1:], flag[1:]) })
		if clustered || slices.Contains(flags, name) {
			return ReadListError{Program: tool, Why: "with " + arg.text + " writes a file or runs another program"}
		}
	}
	return nil
}

func (g Grammar) gitReads(args []shellWord) error {
	for slices.Contains([]string{"-C", "--no-pager", "-P", "--no-optional-locks"}, first(args)) {
		if args[0].text == "-C" && len(args) > 1 {
			args = args[1:]
		}
		args = args[1:]
	}
	sub, rest := first(args), args[min(1, len(args)):]
	switch {
	case slices.Contains([]string{"", "status", "log", "diff", "show", "blame", "ls-files", "ls-tree", "rev-parse", "rev-list", "describe", "shortlog", "cat-file", "merge-base", "grep"}, sub):
		return g.refuses("git "+sub, rest, "--output", "-O", "--open-files-in-pager")
	case sub == "branch" && !slices.ContainsFunc(rest, func(arg shellWord) bool {
		return !slices.Contains([]string{"--show-current", "-a", "-r", "-v", "-vv", "--all", "--remotes", "--list"}, arg.text)
	}):
		return nil
	}
	return ReadListError{Program: "git " + sub, Why: "is not one of git's read commands here (status, log, diff, show, blame, ls-files, grep and the like)"}
}

func goChecks(args []shellWord) error {
	if !slices.Contains([]string{"vet", "test", "list", "version", "doc", "env"}, first(args)) {
		return ReadListError{Program: "go " + first(args), Why: "is not go vet, test, list, version, doc or env, which check without writing the project"}
	}
	for _, arg := range args[1:] {
		if !strings.HasPrefix(arg.text, "-") {
			continue
		}
		name, value, _ := strings.Cut(strings.TrimLeft(arg.text, "-"), "=")
		known := slices.Contains([]string{"run", "skip", "v", "count", "race", "short", "timeout", "p", "cpu", "bench", "benchtime", "benchmem",
			"failfast", "json", "list", "parallel", "shuffle", "tags", "cover", "covermode", "coverpkg", "vet", "mod", "x", "n", "a",
			"trimpath", "buildvcs", "f", "m", "deps", "e", "test", "find", "compiled", "versions", "retracted", "all", "src", "cmd"}, name)
		if !known || name == "mod" && value != "readonly" && value != "vendor" {
			return ReadListError{Program: "go " + args[0].text, Why: "with " + arg.text + " can write a file or run another program"}
		}
	}
	return nil
}
