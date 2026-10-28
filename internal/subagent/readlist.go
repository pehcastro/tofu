package subagent

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"tofu/internal/sys"
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
	mentioned, err := writesOf(commands, simpleCommand.mentions, nil)
	if err != nil {
		return err
	}
	pad := b.scratchpad()
	for _, named := range mentioned {
		if inTofuState(named) && !b.Scratched(named) && (pad == "" || !under(pad, named)) {
			return ReadListError{Program: named, Why: "is tofu's own state, and of .tofu a sub-agent reads only .tofu/rules"}
		}
	}
	left := false
	for _, step := range commands {
		if err := step.treeWide(b.Owns()); err != nil {
			return err
		}
		if err := b.reads(grammar, step, left); err != nil {
			return err
		}
		left = left || step.leaves()
	}
	if err := b.builds(commands); err != nil {
		return err
	}
	if err := b.writes(commands, grammar.changes, grammar.devices()); err != nil {
		return err
	}
	scripts, err := writesOf(commands, simpleCommand.script, nil)
	if err != nil {
		return err
	}
	for _, script := range scripts {
		if Allow(script, b.Owns()) != nil {
			return ReadListError{Program: script, Why: "is not inside a path you hold, so powershell does not run it: name it in your report to ask the orchestrator to grant its folder"}
		}
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
	for tool == "rtk" || tool == "timeout" {
		switch {
		case tool == "timeout":
			args = timed(args)
		case first(args) == "proxy":
			args = args[1:]
		}
		if len(args) == 0 {
			break
		}
		tool, args = toolName(args[0].text), args[1:]
	}
	return tool, args
}

func timed(args []shellWord) []shellWord {
	for strings.HasPrefix(first(args), "-") {
		if slices.Contains([]string{"-s", "-k", "--signal", "--kill-after"}, args[0].text) {
			args = args[1:]
		}
		args = args[min(1, len(args)):]
	}
	return args[min(1, len(args)):]
}

func scriptFile(args []shellWord) (shellWord, bool) {
	for i := 0; i < len(args); i++ {
		switch flag := strings.ToLower(args[i].text); {
		case (flag == "-file" || flag == "-f") && i+1 < len(args):
			return args[i+1], true
		case flag == "-executionpolicy" || flag == "-ep":
			i++
		case !slices.Contains([]string{"-noprofile", "-nop", "-noninteractive", "-nologo"}, flag):
			return shellWord{}, false
		}
	}
	return shellWord{}, false
}

func (c simpleCommand) script() []shellWord {
	tool, args := c.unwrapped()
	if file, found := scriptFile(args); found && (tool == "powershell" || tool == "pwsh") {
		return []shellWord{file}
	}
	return nil
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

const sharedTargetWhy = "builds into the project's one shared target, which $CARGO_TARGET_DIR names, so a target folder of your own is refused: private targets filled the drive"

func (c simpleCommand) cargoBuilds() (string, []shellWord, bool) {
	tool, args := c.unwrapped()
	if strings.HasPrefix(first(args), "+") {
		args = args[1:]
	}
	if tool != "cargo" || first(args) != "build" && first(args) != "run" {
		return "", nil, false
	}
	if end := slices.IndexFunc(args, func(arg shellWord) bool { return arg.text == "--" }); end >= 0 {
		args = args[:end]
	}
	return "cargo " + args[0].text, args[1:], true
}

func packages(args []shellWord) []shellWord {
	var named []shellWord
	for i, arg := range args {
		value, joined := strings.CutPrefix(arg.text, "--package=")
		switch {
		case joined:
			named = append(named, shellWord{text: value, expands: arg.expands})
		case (arg.text == "-p" || arg.text == "--package") && i+1 < len(args):
			named = append(named, args[i+1])
		case strings.HasPrefix(arg.text, "-p") && len(arg.text) > 2:
			named = append(named, shellWord{text: arg.text[2:], expands: arg.expands})
		}
	}
	return named
}

func (c simpleCommand) manifests() []shellWord {
	_, args, builds := c.cargoBuilds()
	if !builds || len(packages(args)) > 0 {
		return nil
	}
	for i, arg := range args {
		if value, joined := strings.CutPrefix(arg.text, "--manifest-path="); joined {
			return []shellWord{{text: value, expands: arg.expands}}
		}
		if arg.text == "--manifest-path" && i+1 < len(args) {
			return args[i+1 : i+2]
		}
	}
	return []shellWord{{text: "Cargo.toml"}}
}

func packageName(manifest string) (string, bool) {
	body, err := os.ReadFile(manifest)
	var parsed struct {
		Package struct {
			Name string `toml:"name"`
		} `toml:"package"`
	}
	if err != nil || toml.Unmarshal(body, &parsed) != nil {
		return "", false
	}
	return parsed.Package.Name, parsed.Package.Name != ""
}

func crateOf(manifest string) (string, error) {
	for dir := path.Dir(path.Clean(manifest)); ; dir = path.Dir(dir) {
		nearest := path.Join(dir, "Cargo.toml")
		if _, err := os.Stat(nearest); err == nil {
			if _, crate := packageName(nearest); !crate {
				return "", ReadListError{Program: nearest, Why: "is a workspace with no crate of its own, so cargo builds every crate in it: name one with -p"}
			}
			return dir, nil
		}
		if dir == path.Dir(dir) {
			return "", ReadListError{Program: manifest, Why: "names no crate: no Cargo.toml is in or above it"}
		}
	}
}

func crates(match func(dir, name string) bool) []string {
	var found []string
	for queue := []string{"."}; len(queue) > 0; queue = queue[1:] {
		entries, err := os.ReadDir(queue[0])
		if err != nil || slices.ContainsFunc(entries, func(entry os.DirEntry) bool { return entry.Name() == "CACHEDIR.TAG" }) {
			continue
		}
		if name, crate := packageName(path.Join(queue[0], "Cargo.toml")); crate && match(queue[0], name) {
			found = append(found, queue[0])
		}
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && entry.Name() != "node_modules" {
				queue = append(queue, path.Join(queue[0], entry.Name()))
			}
		}
	}
	return found
}

func (b *Boundary) holdsEvery(what string, homes []string) error {
	if len(homes) == 0 {
		return ReadListError{Program: what, Why: "names no crate in this project, so what it builds or runs is held by nobody"}
	}
	for _, home := range homes {
		if !slices.ContainsFunc(b.Owns(), func(glob string) bool { return Overlap(glob, home) }) {
			return ReadListError{Program: what, Why: "builds or runs the crate in " + home + ", which this agent holds nothing in: name it in your report to ask for a path in it"}
		}
	}
	return nil
}

func (b *Boundary) builds(commands []simpleCommand) error {
	for _, command := range commands {
		cargo, args, builds := command.cargoBuilds()
		switch {
		case !builds:
			continue
		case b.sharedTarget() == "":
			return ReadListError{Program: cargo, Why: "runs only with a scratch folder, whose cache holds the project's shared target"}
		case given(args, "--workspace", "--all"):
			return ReadListError{Program: cargo, Why: "with --workspace builds every crate in the workspace: name the crate you hold with -p"}
		}
		for _, name := range packages(args) {
			if name.expands {
				return UnparseablePathError{Path: name.text}
			}
			if err := b.holdsEvery(cargo+" -p "+name.text, crates(func(_, crate string) bool { return crate == name.text })); err != nil {
				return err
			}
		}
	}
	manifests, err := writesOf(commands, simpleCommand.manifests, nil)
	if err != nil {
		return err
	}
	for _, manifest := range manifests {
		home, err := crateOf(manifest)
		if err != nil {
			return err
		}
		if err := b.holdsEvery("cargo", []string{home}); err != nil {
			return err
		}
	}
	return nil
}

func (b *Boundary) built(program shellWord) (bool, error) {
	target := b.sharedTarget()
	if target == "" || program.expands || !under(target, program.text) {
		return false, nil
	}
	segments := strings.Split(strings.TrimPrefix(path.Clean(normalizePath(program.text)), target+"/"), "/")
	name := strings.TrimSuffix(segments[len(segments)-1], ".exe")
	source := func(dir, folder string) bool {
		return slices.ContainsFunc([]string{name + ".rs", name + "/main.rs"}, func(file string) bool {
			_, err := os.Stat(path.Join(dir, folder, file))
			return err == nil
		})
	}
	var homes []string
	switch {
	case len(segments) == 2:
		homes = crates(func(dir, crate string) bool { return strings.EqualFold(crate, name) || source(dir, "src/bin") })
	case len(segments) == 3 && segments[1] == "examples":
		homes = crates(func(dir, _ string) bool { return source(dir, "examples") })
	}
	return true, b.holdsEvery(program.text, homes)
}

func (b *Boundary) scratchpad() string {
	agents := path.Dir(normalizePath(b.Scratch))
	if b.Scratch == "" || path.Base(agents) != sys.ScratchAgents {
		return ""
	}
	return path.Dir(path.Dir(path.Dir(agents)))
}

func (b *Boundary) sharedTarget() string {
	if pad := b.scratchpad(); pad != "" {
		return path.Join(pad, sys.ScratchCache, "cargo")
	}
	return ""
}

func (c simpleCommand) mentions() []shellWord {
	_, args := c.named()
	var named []shellWord
	for _, word := range append(slices.Clone(args), c.reads...) {
		if _, value, flagged := strings.Cut(word.text, "="); flagged && strings.HasPrefix(word.text, "-") {
			word.text = value
		}
		if !word.expands {
			named = append(named, word)
		}
	}
	return named
}

func inTofuState(named string) bool {
	segments := strings.FieldsFunc(path.Clean(normalizePath(named)), func(r rune) bool { return r == '/' || r == ':' })
	for i, segment := range segments {
		if segment == sys.StateDirName && (i+1 == len(segments) || segments[i+1] != "rules") {
			return true
		}
	}
	return false
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
		named == ".." || strings.HasPrefix(named, "../") || inTofuState(named)
}

func bare(program shellWord) error {
	if program.expands || strings.ContainsAny(program.text, `/\`) {
		return ReadListError{Program: program.text, Why: "is not a bare program name, so what it runs cannot be known"}
	}
	return nil
}

func (b *Boundary) reads(g Grammar, c simpleCommand, left bool) error {
	words := c.words
	for len(words) > 0 && shellAssignment.MatchString(words[0].text) {
		name, _, _ := strings.Cut(words[0].text, "=")
		if name == "CARGO_TARGET_DIR" {
			return ReadListError{Program: name, Why: sharedTargetWhy}
		}
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
	if built, err := b.built(words[0]); built {
		return err
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
	if _, cargoArgs, builds := c.cargoBuilds(); builds {
		words = append([]shellWord{words[0]}, cargoArgs...)
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

func findActions() []string {
	return []string{"-exec", "-execdir", "-ok", "-okdir", "-delete", "-fprint", "-fprint0", "-fprintf", "-fls"}
}

func FindActs(args []string) bool {
	return slices.ContainsFunc(args, func(arg string) bool { return slices.Contains(findActions(), arg) })
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
		return false, g.refuses(tool, args, findActions()...)
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
	case "timeout":
		return g.wrapped(tool, timed(args))
	case "powershell", "pwsh":
		if _, found := scriptFile(args); !found {
			return true, ReadListError{Program: tool, Why: "runs here only as -File with a script inside a path you hold, after -NoProfile, -NonInteractive, -NoLogo or -ExecutionPolicy: name any other use in your report to ask the orchestrator for it"}
		}
		return true, nil
	case "npx", "pnpx", "bunx":
		if !slices.Contains([]string{"tsc", "vue-tsc", "svelte-check", "vitest", "jest", "eslint", "prettier"}, first(args)) {
			return true, ReadListError{Program: tool, Why: "runs here only a type checker, test runner or linter"}
		}
		return g.wrapped(tool, args)
	case "cargo":
		if strings.HasPrefix(first(args), "+") {
			args = args[1:]
		}
		if !slices.Contains([]string{"test", "check", "clippy", "build", "run", "tree", "metadata"}, first(args)) && (first(args) != "fmt" || !given(args, "--check")) {
			return true, ReadListError{Program: tool, Why: "runs here only as cargo test, check, clippy, build or run of a crate you hold a path in, tree, metadata or fmt --check"}
		}
		if slices.ContainsFunc(args, func(arg shellWord) bool {
			return arg.text == "--target-dir" || strings.HasPrefix(arg.text, "--target-dir=")
		}) {
			return true, ReadListError{Program: tool, Why: sharedTargetWhy}
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
	if first(args) == "models" && len(operands(args)) == 1 || len(args) == 2 && first(args) == "serve" && args[1].text == "--schema" {
		return nil
	}
	for _, verb := range [][]string{{"version"}, {"session", "find"}, {"session", "trace"}, {"session", "info"}, {"boardy", "show"}, {"boardy", "list"}, {"boardy", "triage", "list"}} {
		if len(args) >= len(verb) && slices.EqualFunc(args[:len(verb)], verb, func(arg shellWord, word string) bool { return arg.text == word }) {
			return nil
		}
	}
	return ReadListError{Program: "tofu", Why: "runs here only as serve --schema, session find, trace or info, boardy show, list or triage list, models, or version"}
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
