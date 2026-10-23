package subagent

import (
	"os"
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

type InlineCode struct {
	Program string
	Flags   []string
	Script  string
	Code    string
	Args    []string
	Cwd     string
}

func (c InlineCode) Python() bool {
	return pythonName(toolName(c.Program))
}

func pythonName(program string) bool {
	return program == "py" || strings.HasPrefix(program, "python") && strings.Trim(program[len("python"):], "0123456789.") == ""
}

type CodeAskError struct {
	Code InlineCode
}

func (e CodeAskError) Error() string {
	return e.Code.Program + " runs inline code, which runs only once the person allows this exact code through the orchestrator"
}

const codeAskWhy = "is on the read list only as python -m pytest, mypy, ruff or unittest, or as inline code the person allows: one call with -c or -e in single quotes, a heredoc, or a script in your scratch folder, with nothing piped or chained"

func (b *Boundary) inlineCode(command string) (InlineCode, bool) {
	if b.Scratch == "" {
		return InlineCode{}, false
	}
	lexer := shellLexer{src: command, known: b.temp()}
	var words []shellWord
	for {
		token, word, more := lexer.next()
		switch {
		case len(lexer.heredocs) > 0:
			stdin, closed := heredocBody(command[lexer.at:], lexer.heredocs)
			if !closed {
				return InlineCode{}, false
			}
			return b.codeCall(words, stdin, true)
		case !more, token == tokenBreak && word.text == "\n" && strings.TrimSpace(command[lexer.at:]) == "":
			return b.codeCall(words, "", false)
		case token == tokenWord:
			words = append(words, word)
		case token != tokenNothing:
			return InlineCode{}, false
		}
	}
}

func heredocBody(rest string, docs []heredoc) (string, bool) {
	line, body, found := strings.Cut(rest, "\n")
	if len(docs) != 1 || !found || strings.TrimSpace(line) != "" {
		return "", false
	}
	lines := strings.Split(body, "\n")
	for i, text := range lines {
		if docs[0].tabs {
			lines[i] = strings.TrimLeft(text, "\t")
		}
		if strings.TrimSuffix(lines[i], "\r") == docs[0].delimiter {
			return strings.Join(lines[:i], "\n") + "\n", strings.TrimSpace(strings.Join(lines[i+1:], "\n")) == ""
		}
	}
	return "", false
}

func (b *Boundary) codeCall(words []shellWord, stdin string, fed bool) (InlineCode, bool) {
	if len(words) == 0 || bare(words[0]) != nil || slices.ContainsFunc(words, func(word shellWord) bool { return word.expands }) {
		return InlineCode{}, false
	}
	flags, codeFlags := []string{"-u", "-B", "-I", "-E", "-s", "-S", "-P", "-O", "-OO", "-q", "-b", "-bb"}, []string{"-c"}
	switch program := toolName(words[0].text); {
	case program == "node":
		flags, codeFlags = []string{"--no-warnings"}, []string{"-e", "--eval"}
	case !pythonName(program):
		return InlineCode{}, false
	}
	code, rest := InlineCode{Program: words[0].text, Cwd: b.Scratch}, make([]string, 0, len(words)-1)
	for _, word := range words[1:] {
		rest = append(rest, word.text)
	}
	for len(rest) > 0 && slices.Contains(flags, rest[0]) {
		code.Flags, rest = append(code.Flags, rest[0]), rest[1:]
	}
	switch {
	case fed && (len(rest) == 0 || rest[0] == "-"):
		code.Code, code.Args = stdin, rest[min(1, len(rest)):]
	case fed:
		return InlineCode{}, false
	case len(rest) > 1 && slices.Contains(codeFlags, rest[0]):
		code.Code, code.Args = rest[1], rest[2:]
	case len(rest) > 0 && !strings.HasPrefix(rest[0], "-") && b.Scratched(rest[0]):
		body, err := os.ReadFile(rest[0])
		if err != nil {
			return InlineCode{}, false
		}
		code.Script, code.Code, code.Args = rest[0], string(body), rest[1:]
	default:
		return InlineCode{}, false
	}
	return code, true
}

func (b *Boundary) HoldsTheTree() bool {
	return slices.ContainsFunc(b.Owns(), func(glob string) bool { return glob == "**" || wholeProject(glob) })
}

func (b *Boundary) Bash(command string, grammar Grammar) error {
	if code, found := b.inlineCode(command); found {
		return CodeAskError{Code: code}
	}
	if b.HoldsTheTree() {
		return b.Shell(command)
	}
	if grammar == PowerShell {
		if strings.Contains(bracedVariable.ReplaceAllString(command, ""), "{") || strings.Contains(command, "<#") {
			return ReadListError{Program: "this command", Why: "has a PowerShell script block or block comment, which this check cannot see into"}
		}
		command = strings.ReplaceAll(command, `\`, "/")
	}
	commands := shellCommands(command, b.temp())
	left := false
	for _, step := range commands {
		if err := step.treeWide(b.Owns()); err != nil {
			return err
		}
		if err := grammar.reads(step, left); err != nil {
			return err
		}
		left = left || step.leaves()
	}
	if err := b.writes(commands, grammar.changes, grammar.devices()); err != nil {
		return err
	}
	formatted, err := writesOf(commands, simpleCommand.formats, nil)
	if err != nil {
		return err
	}
	for _, written := range formatted {
		if err := b.Write(written); err != nil {
			return err
		}
	}
	return nil
}

func (c simpleCommand) unwrapped() (string, []shellWord) {
	tool, args := c.named()
	for tool == "rtk" {
		if first(args) == "proxy" {
			args = args[1:]
		}
		if len(args) == 0 {
			break
		}
		tool, args = toolName(args[0].text), args[1:]
	}
	return tool, args
}

func (c simpleCommand) formats() []shellWord {
	tool, args := c.unwrapped()
	emits := slices.ContainsFunc(args, func(arg shellWord) bool { return strings.HasPrefix(arg.text, "--emit") })
	rewrites := tool == "gofmt" && given(args, "-w") || tool == "rustfmt" && !given(args, "--check") && !emits
	if !rewrites {
		return nil
	}
	var files []shellWord
	for i := 0; i < len(args); i++ {
		switch text := args[i].text; {
		case slices.Contains([]string{"-r", "--edition", "--style-edition", "--config-path", "--config", "--color"}, text):
			i++
		case !strings.HasPrefix(text, "-"):
			files = append(files, args[i])
		}
	}
	return files
}

func (c simpleCommand) buildsInto() []shellWord {
	tool, args := c.unwrapped()
	var into []shellWord
	for _, word := range c.words {
		if !shellAssignment.MatchString(word.text) {
			break
		}
		if dir, set := strings.CutPrefix(word.text, "CARGO_TARGET_DIR="); set {
			into = append(into, shellWord{text: dir, expands: word.expands})
		}
	}
	if strings.HasPrefix(first(args), "+") {
		args = args[1:]
	}
	if tool != "cargo" || first(args) != "build" {
		return into
	}
	for i, arg := range args {
		if dir, set := strings.CutPrefix(arg.text, "--target-dir="); set {
			return append(into, shellWord{text: dir, expands: arg.expands})
		}
		if arg.text == "--target-dir" && i+1 < len(args) {
			return append(into, args[i+1])
		}
	}
	if len(into) == 0 {
		into = append(into, shellWord{text: "target"})
	}
	return into
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
	changed := append(append(c.targets(), c.relinks()...), c.buildsInto()...)
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
			"CGO_ENABLED", "CARGO_TARGET_DIR", "RUST_BACKTRACE", "RUST_LOG", "NODE_ENV", "PYTHONDONTWRITEBYTECODE", "PYTHONUNBUFFERED"}, name) {
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
	case "tofu":
		return false, tofuReads(args)
	case "gofmt":
		return false, g.refuses(tool, args, "-cpuprofile")
	case "rustfmt":
		return false, nil
	case "rtk":
		switch first(args) {
		case "read", "smart", "tree":
			return false, nil
		case "proxy":
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
			return true, ReadListError{Program: tool, Why: codeAskWhy}
		}
		return g.wrapped(tool, args[1:])
	case "node":
		return true, ReadListError{Program: tool, Why: codeAskWhy}
	case "npx", "pnpx", "bunx":
		if !slices.Contains([]string{"tsc", "vue-tsc", "svelte-check", "vitest", "jest", "eslint", "prettier"}, first(args)) {
			return true, ReadListError{Program: tool, Why: "runs here only a type checker, test runner or linter"}
		}
		return g.wrapped(tool, args)
	case "cargo":
		if strings.HasPrefix(first(args), "+") {
			args = args[1:]
		}
		if !slices.Contains([]string{"test", "check", "clippy", "build", "tree", "metadata"}, first(args)) && (first(args) != "fmt" || !given(args, "--check")) {
			return true, ReadListError{Program: tool, Why: "runs here only as cargo test, check, clippy, build into a target folder you hold, tree, metadata or fmt --check"}
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

func tofuReads(args []shellWord) error {
	if first(args) == "models" && len(operands(args)) == 1 {
		return nil
	}
	for _, verb := range [][]string{{"version"}, {"session", "find"}, {"session", "trace"}, {"session", "info"}, {"boardy", "show"}, {"boardy", "list"}, {"boardy", "triage", "list"}} {
		if len(args) >= len(verb) && slices.EqualFunc(args[:len(verb)], verb, func(arg shellWord, word string) bool { return arg.text == word }) {
			return nil
		}
	}
	return ReadListError{Program: "tofu", Why: "runs here only as session find, trace or info, boardy show, list or triage list, models, or version"}
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
