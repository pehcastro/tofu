package subagent

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type shellWord struct {
	text    string
	expands bool
}

type shellToken int

const (
	tokenWord shellToken = iota
	tokenBreak
	tokenWritesTo
	tokenReadsFrom
	tokenNothing
)

type heredoc struct {
	delimiter string
	tabs      bool
}

type shellLexer struct {
	src      string
	at       int
	heredocs []heredoc
}

func (l *shellLexer) peek(offset int) byte {
	if l.at+offset < len(l.src) {
		return l.src[l.at+offset]
	}
	return 0
}

func (l *shellLexer) next() (shellToken, shellWord, bool) {
	for l.at < len(l.src) {
		c := l.src[l.at]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			l.at++
		case c == '\\' && l.peek(1) == '\n':
			l.at += 2
		case c == '\n':
			l.at++
			l.skipHeredocs()
			return tokenBreak, shellWord{text: "\n"}, true
		case c == '#':
			for l.at < len(l.src) && l.src[l.at] != '\n' {
				l.at++
			}
		case c == '>' || c == '<' || (c == '&' && l.peek(1) == '>'):
			return l.redirect(), shellWord{}, true
		case strings.IndexByte(";|&()", c) >= 0:
			operator := l.src[l.at : l.at+1]
			if (c == '&' || c == '|') && l.peek(1) == c {
				operator = l.src[l.at : l.at+2]
			}
			l.at += len(operator)
			return tokenBreak, shellWord{text: operator}, true
		default:
			word := l.word()
			if isDigits(word.text) && (l.peek(0) == '>' || l.peek(0) == '<') {
				continue
			}
			return tokenWord, word, true
		}
	}
	return tokenNothing, shellWord{}, false
}

func isDigits(text string) bool {
	return text != "" && strings.Trim(text, "0123456789") == ""
}

func (l *shellLexer) redirect() shellToken {
	operator := l.src[l.at:min(l.at+3, len(l.src))]
	switch {
	case strings.HasPrefix(operator, "<<<"):
		l.at += 3
		return tokenReadsFrom
	case strings.HasPrefix(operator, "<<"):
		l.at += 2
		tabs := l.peek(0) == '-'
		if tabs {
			l.at++
		}
		for l.peek(0) == ' ' || l.peek(0) == '\t' {
			l.at++
		}
		l.heredocs = append(l.heredocs, heredoc{delimiter: l.word().text, tabs: tabs})
		return tokenNothing
	case strings.HasPrefix(operator, ">&") || strings.HasPrefix(operator, "<&"):
		l.at += 2
		if strings.IndexByte("0123456789-", l.peek(0)) < 0 {
			return tokenWritesTo
		}
		for strings.IndexByte("0123456789-", l.peek(0)) >= 0 {
			l.at++
		}
		return tokenNothing
	case operator[0] == '<':
		l.at += len(operator) - len(strings.TrimLeft(operator, "<>"))
		return tokenReadsFrom
	}
	l.at += len(operator) - len(strings.TrimLeft(operator, "&>|"))
	return tokenWritesTo
}

func (l *shellLexer) skipHeredocs() {
	for _, doc := range l.heredocs {
		for l.at < len(l.src) {
			line, _, _ := strings.Cut(l.src[l.at:], "\n")
			l.at = min(l.at+len(line)+1, len(l.src))
			if doc.tabs {
				line = strings.TrimLeft(line, "\t")
			}
			if strings.TrimSuffix(line, "\r") == doc.delimiter {
				break
			}
		}
	}
	l.heredocs = nil
}

func (l *shellLexer) word() shellWord {
	var text strings.Builder
	expands := false
	for l.at < len(l.src) {
		c := l.src[l.at]
		switch {
		case strings.IndexByte(" \t\r\n;|&()<>", c) >= 0:
			return shellWord{text: text.String(), expands: expands}
		case c == '\'':
			end := strings.IndexByte(l.src[l.at+1:], '\'')
			if end < 0 {
				end = len(l.src) - l.at - 1
			}
			text.WriteString(l.src[l.at+1 : l.at+1+end])
			l.at += end + 2
		case c == '"':
			l.at++
			for l.at < len(l.src) && l.src[l.at] != '"' {
				if l.src[l.at] == '$' || l.src[l.at] == '`' {
					expands = true
					text.WriteString(l.expansion())
					continue
				}
				if l.src[l.at] == '\\' && strings.IndexByte("$`\"\\\n", l.peek(1)) >= 0 {
					l.at++
				}
				text.WriteByte(l.peek(0))
				l.at++
			}
			l.at++
		case c == '\\':
			text.WriteByte(l.peek(1))
			l.at += 2
		case c == '$' || c == '`':
			expands = true
			text.WriteString(l.expansion())
		default:
			text.WriteByte(c)
			l.at++
		}
	}
	return shellWord{text: text.String(), expands: expands}
}

const shellParameterBytes = "_0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ?@#*!$-"

func (l *shellLexer) expansion() string {
	start := l.at
	closer, opener := byte(0), byte(0)
	switch {
	case l.src[l.at] == '`':
		closer = '`'
	case l.peek(1) == '(':
		opener, closer = '(', ')'
		l.at++
	case l.peek(1) == '{':
		opener, closer = '{', '}'
		l.at++
	default:
		l.at++
		for l.at < len(l.src) && strings.IndexByte(shellParameterBytes, l.src[l.at]) >= 0 {
			l.at++
		}
		return l.src[start:l.at]
	}
	l.at++
	for depth := 1; l.at < len(l.src) && depth > 0; l.at++ {
		switch l.src[l.at] {
		case '\\':
			l.at++
		case '\'':
			if skip := strings.IndexByte(l.src[l.at+1:], '\''); skip >= 0 {
				l.at += skip + 1
			}
		case opener:
			depth++
		case closer:
			depth--
		}
	}
	return l.src[start:min(l.at, len(l.src))]
}

type simpleCommand struct {
	words  []shellWord
	writes []shellWord
	then   string
}

func shellCommands(command string) []simpleCommand {
	lexer := shellLexer{src: command}
	var commands []simpleCommand
	var current simpleCommand
	awaiting := tokenWord
	for {
		token, word, more := lexer.next()
		if !more {
			return append(commands, current)
		}
		switch token {
		case tokenBreak:
			current.then = word.text
			commands, current, awaiting = append(commands, current), simpleCommand{}, tokenWord
		case tokenWritesTo, tokenReadsFrom:
			awaiting = token
		case tokenWord:
			switch awaiting {
			case tokenWritesTo:
				current.writes = append(current.writes, word)
			case tokenWord:
				current.words = append(current.words, word)
			}
			awaiting = tokenWord
		}
	}
}

var shellAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

func (c simpleCommand) named() (string, []shellWord) {
	words := c.words
	for len(words) > 0 && shellAssignment.MatchString(words[0].text) {
		words = words[1:]
	}
	if len(words) == 0 {
		return "", nil
	}
	return toolName(words[0].text), words[1:]
}

func operands(args []shellWord) []shellWord {
	var found []shellWord
	for _, arg := range args {
		if !strings.HasPrefix(arg.text, "-") {
			found = append(found, arg)
		}
	}
	return found
}

func inPlaceFiles(args []shellWord, scriptFlags ...string) []shellWord {
	inPlace, scripted := false, false
	var files []shellWord
	for i := 0; i < len(args); i++ {
		text := args[i].text
		switch {
		case slices.Contains(scriptFlags, text):
			scripted = true
			i++
		case strings.HasPrefix(text, "--expression=") || strings.HasPrefix(text, "--file="):
			scripted = true
		case strings.HasPrefix(text, "--in-place"):
			inPlace = true
		case strings.HasPrefix(text, "-") && !strings.HasPrefix(text, "--"):
			inPlace = inPlace || strings.Contains(text, "i")
		default:
			files = append(files, args[i])
		}
	}
	if !inPlace {
		return nil
	}
	if !scripted && len(files) > 0 {
		files = files[1:]
	}
	return files
}

func (c simpleCommand) targets() []shellWord {
	targets := slices.Clone(c.writes)
	tool, args := c.named()
	switch tool {
	case "tee":
		return append(targets, operands(args)...)
	case "cp", "mv":
		for i, arg := range args {
			if arg.text == "-t" && i+1 < len(args) {
				return append(targets, args[i+1])
			}
			if directory, given := strings.CutPrefix(arg.text, "--target-directory="); given {
				return append(targets, shellWord{text: directory, expands: arg.expands})
			}
		}
		if files := operands(args); len(files) > 1 {
			return append(targets, files[len(files)-1])
		}
	case "sed":
		return append(targets, inPlaceFiles(args, "-e", "-f", "--expression", "--file")...)
	case "perl":
		return append(targets, inPlaceFiles(args, "-e", "-E")...)
	}
	return targets
}

func (c simpleCommand) relinks() []shellWord {
	switch tool, args := c.named(); tool {
	case "rm", "rmdir", "unlink", "mv", "ln":
		return operands(args)
	}
	return nil
}

type ShellStep struct {
	Program string
	Args    []string
	Changes []string
}

func ShellSteps(command string) []ShellStep {
	var steps []ShellStep
	for _, command := range shellCommands(command) {
		if len(command.words)+len(command.writes) == 0 {
			continue
		}
		tool, args := command.named()
		step := ShellStep{Program: tool}
		for _, arg := range args {
			step.Args = append(step.Args, arg.text)
		}
		for _, changed := range append(command.targets(), command.relinks()...) {
			step.Changes = append(step.Changes, changed.text)
		}
		steps = append(steps, step)
	}
	return steps
}

func (c simpleCommand) treeWide() error {
	tool, args := c.named()
	var rest []string
	for _, arg := range args {
		if namesTheWholeTree(tool, arg.text) {
			return errors.New(treeWideRefusal(tool, arg.text))
		}
		rest = append(rest, arg.text)
	}
	if walksWithoutAPath(tool, rest) {
		return errors.New(treeWideRefusal(tool, "(no path given)"))
	}
	return nil
}

func (c simpleCommand) enters(dirs []string, after string, runs bool) ([]string, bool) {
	tool, args := c.named()
	found := operands(args)
	if tool != "cd" || len(found) != 1 || found[0].expands || found[0].text == "" || strings.HasPrefix(found[0].text, "~") {
		return dirs, false
	}
	if after == "|" || c.then == "|" || c.then == "&" {
		return dirs, false
	}
	moved := make([]string, len(dirs))
	succeeds := runs
	for i, dir := range dirs {
		moved[i] = within(dir, found[0].text)
		info, err := os.Stat(moved[i])
		succeeds = succeeds && err == nil && info.IsDir()
	}
	if succeeds {
		return moved, true
	}
	return append(moved, dirs...), false
}

func within(dir, target string) string {
	if dir == "" || path.IsAbs(target) || filepath.IsAbs(target) {
		return target
	}
	return dir + "/" + target
}

func writesOf(commands []simpleCommand) ([]string, error) {
	var paths []string
	var outer [][]string
	dirs, after, runs := []string{""}, "", true
	for _, command := range commands {
		if len(command.words)+len(command.writes) == 0 && command.then == "\n" {
			continue
		}
		for _, target := range command.targets() {
			switch {
			case slices.Contains([]string{"/dev/null", "nul", "/dev/stdout", "/dev/stderr"}, strings.ToLower(target.text)):
			case target.expands:
				return nil, UnparseablePathError{Path: target.text}
			default:
				for _, dir := range dirs {
					paths = append(paths, within(dir, target.text))
				}
			}
		}
		var succeeds bool
		dirs, succeeds = command.enters(dirs, after, runs)
		switch command.then {
		case "(":
			outer = append(outer, dirs)
		case ")":
			if len(outer) > 0 {
				dirs, outer = outer[len(outer)-1], outer[:len(outer)-1]
			}
		}
		after = command.then
		runs = after == "&&" && succeeds || slices.Contains([]string{"", ";", "\n", "(", "&"}, after)
	}
	return paths, nil
}

func ShellWrites(command string) ([]string, error) {
	return writesOf(shellCommands(command))
}

func (b *Boundary) Shell(command string) error {
	commands := shellCommands(command)
	for _, command := range commands {
		if err := command.treeWide(); err != nil {
			return err
		}
	}
	paths, err := writesOf(commands)
	if err != nil {
		return err
	}
	paths = slices.DeleteFunc(paths, inTempDirectory)
	for _, written := range paths {
		if err := b.Write(written); err != nil {
			return err
		}
	}
	for _, written := range paths {
		switch strings.ToLower(path.Ext(written)) {
		case ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".go", ".java", ".js", ".jsx", ".py", ".pyi", ".rb", ".rs", ".sh", ".bash", ".ps1", ".ts", ".tsx":
			return ShellSourceWriteError{Path: written}
		}
	}
	return nil
}

func inTempDirectory(written string) bool {
	return under("/tmp", written) || under(os.TempDir(), written)
}

func under(root, written string) bool {
	root, target := path.Clean(normalizePath(root)), path.Clean(normalizePath(written))
	return target == root || strings.HasPrefix(target, root+"/")
}

func (b *Boundary) Scratched(written string) bool {
	return b.Scratch != "" && under(b.Scratch, written)
}

func (b *Boundary) KeepsToScratch(command string) bool {
	touched := 0
	for _, step := range shellCommands(command) {
		tool, args := step.named()
		changed := step.targets()
		switch {
		case len(step.words)+len(step.writes) == 0:
			continue
		case slices.Contains([]string{"rm", "rmdir", "mkdir", "touch", "unlink"}, tool):
			changed = append(changed, operands(args)...)
		case tool == "mv":
			changed = append(changed, step.relinks()...)
		case !slices.Contains([]string{"", "cp", "tee", "echo", "printf", "cat", "ls"}, tool):
			return false
		}
		for _, word := range changed {
			if word.expands || !b.Scratched(word.text) {
				return false
			}
		}
		touched += len(changed)
	}
	return touched > 0
}
