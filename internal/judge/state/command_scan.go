package state

import (
	"regexp"
	"strings"
)

type scan struct {
	roots      roots
	base       string
	vars       map[string]string
	seen       map[string]bool
	targets    []WriteTarget
	unresolved bool
}

func scanCommand(command string, where roots) WriteTargets {
	shell, pythonBodies := splitHeredocs(command)
	s := &scan{roots: where, base: where.Cwd, vars: map[string]string{}, seen: map[string]bool{}, targets: []WriteTarget{}}
	for _, segment := range segments(tokens(shell)) {
		s.segment(segment)
	}
	for _, body := range pythonBodies {
		s.pythonWrites(body)
	}
	switch {
	case s.unresolved:
		return WriteTargets{Determination: TargetUnknown, Targets: s.targets}
	case len(s.targets) > 0:
		return WriteTargets{Determination: TargetsResolved, Targets: s.targets}
	}
	return WriteTargets{Determination: NoWriteFound, Targets: s.targets}
}

func (s *scan) segment(segment []string) {
	var args []string
	for i := 0; i < len(segment); i++ {
		token := segment[i]
		switch {
		case token == ">&" || token == ">>&":
			i++
		case token == ">" || token == ">>":
			i++
			if i >= len(segment) {
				s.unresolved = true
				continue
			}
			s.sink(segment[i])
		case assignment.MatchString(token):
			name, value, _ := strings.Cut(token, "=")
			s.vars[name] = s.expand(value)
		default:
			args = append(args, token)
		}
	}
	if len(args) == 0 {
		return
	}
	operands := notFlags(args[1:])
	switch programName(args[0]) {
	case "cd":
		if len(operands) > 0 {
			s.base = resolve(s.base, s.expand(operands[0]))
		}
	case "tee", "rm", "rmdir", "unlink", "touch", "mkdir", "ln":
		for _, operand := range operands {
			s.sink(operand)
		}
	case "cp", "mv", "install", "rsync":
		if len(operands) > 0 {
			s.sink(operands[len(operands)-1])
		}
	case "git":
		subcommand := ""
		for _, operand := range operands {
			if !strings.Contains(operand, "=") {
				subcommand = operand
				break
			}
		}
		switch subcommand {
		case "status", "log", "diff", "show", "blame", "describe", "grep", "rev-parse", "ls-files", "shortlog":
		default:
			s.unresolved = true
		}
	case "python", "python3", "py", "node", "ruby", "perl", "php", "pwsh", "powershell", "sh", "bash", "zsh":
		for _, arg := range args[1:] {
			if arg != "-" {
				s.unresolved = true
			}
		}
	case "sed":
		if !hasInPlace(args[1:]) {
			return
		}
		for _, operand := range operands[1:] {
			s.sink(operand)
		}
	}
}

func hasInPlace(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") && strings.Contains(arg, "i") {
			return true
		}
	}
	return false
}

func notFlags(args []string) []string {
	out := []string{}
	for _, arg := range args {
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			out = append(out, arg)
		}
	}
	return out
}

func (s *scan) sink(token string) {
	expanded := s.expand(token)
	if expanded == "" || strings.ContainsAny(expanded, "$*?") {
		s.unresolved = true
		return
	}
	full := resolve(s.base, expanded)
	if s.seen[strings.ToLower(full)] {
		return
	}
	s.seen[strings.ToLower(full)] = true
	s.targets = append(s.targets, describe(full, s.roots))
}

var (
	assignment    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	variableRef   = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
	heredocMarker = regexp.MustCompile(`^<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
	pythonOpen    = regexp.MustCompile(`open\(\s*([^,()]+?)\s*,\s*['"]([^'"]+)['"]`)
	pythonString  = regexp.MustCompile(`^['"]([^'"]*)['"]$`)
)

func (s *scan) expand(value string) string {
	return variableRef.ReplaceAllStringFunc(value, func(ref string) string {
		name := variableRef.FindStringSubmatch(ref)[1]
		if known, ok := s.vars[name]; ok {
			return known
		}
		return ref
	})
}

func (s *scan) pythonWrites(body string) {
	for _, match := range pythonOpen.FindAllStringSubmatch(body, -1) {
		if mode := match[2]; !strings.HasPrefix(mode, "w") && !strings.HasPrefix(mode, "a") && !strings.HasPrefix(mode, "x") {
			continue
		}
		argument := strings.TrimSpace(match[1])
		if literal := pythonString.FindStringSubmatch(argument); literal != nil {
			s.sink(literal[1])
			continue
		}
		bound := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(argument) + `\s*=\s*['"]([^'"]+)['"]`).FindStringSubmatch(body)
		if bound == nil {
			s.unresolved = true
			continue
		}
		s.sink(bound[1])
	}
}

func splitHeredocs(command string) (string, []string) {
	shell := &strings.Builder{}
	python := []string{}
	rest := command
	for {
		at, tag, end := unquotedMarker(rest)
		if at < 0 {
			shell.WriteString(rest)
			return shell.String(), python
		}
		head := rest[:at]
		shell.WriteString(head)
		after := rest[end:]
		newline := strings.Index(after, "\n")
		if newline < 0 {
			shell.WriteString(after)
			return shell.String(), python
		}
		shell.WriteString(after[:newline] + "\n")
		body, remainder := takeUntilTag(after[newline+1:], tag)
		if feedsPython(head) {
			python = append(python, body)
		}
		rest = remainder
	}
}

func unquotedMarker(text string) (int, string, int) {
	quote := byte(0)
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '<':
			if found := heredocMarker.FindStringSubmatchIndex(text[i:]); found != nil {
				return i, text[i+found[2] : i+found[3]], i + found[1]
			}
		}
	}
	return -1, "", 0
}

func feedsPython(head string) bool {
	segment := head
	if cut := strings.LastIndexAny(head, "&|;\n"); cut >= 0 {
		segment = head[cut+1:]
	}
	return strings.Contains(segment, "python") || strings.Contains(segment, "py -")
}

func takeUntilTag(text, tag string) (string, string) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == tag {
			return strings.Join(lines[:i], "\n"), strings.Join(lines[i+1:], "\n")
		}
	}
	return text, ""
}

func tokens(command string) []string {
	out := []string{}
	current := &strings.Builder{}
	quote := rune(0)
	flush := func() {
		if current.Len() > 0 {
			out = append(out, current.String())
			current.Reset()
		}
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
				continue
			}
			current.WriteRune(c)
		case c == '\'' || c == '"':
			quote = c
		case c == ' ' || c == '\t' || c == '\r':
			flush()
		case c == '>' || c == '<' || c == '&' || c == '|' || c == ';' || c == '\n':
			redirect := c == '>' || c == '<'
			pending := current.String()
			if redirect && pending != "" && strings.Trim(pending, "0123456789") == "" {
				current.Reset()
			}
			flush()
			run := string(c)
			for i+1 < len(runes) && runes[i+1] == c {
				run += string(c)
				i++
			}
			if redirect && i+1 < len(runes) && runes[i+1] == '&' {
				run += "&"
				i++
			}
			out = append(out, run)
		default:
			current.WriteRune(c)
		}
	}
	flush()
	return out
}

func segments(all []string) [][]string {
	out := [][]string{}
	current := []string{}
	for _, token := range all {
		switch {
		case strings.Trim(token, "&|;\n") == "":
			if len(current) > 0 {
				out = append(out, current)
				current = nil
			}
		case strings.Trim(token, "<") == "":
			continue
		default:
			current = append(current, token)
		}
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}
