package subagents

import (
	"strings"
)

func awkReads(args []shellWord) error {
	var programs []string
	positional := true
	for i := 0; i < len(args); i++ {
		text := args[i].text
		switch {
		case text == "-F" || text == "-v" || text == "--field-separator" || text == "--assign":
			i++
		case text == "-e" || text == "--source":
			if i+1 < len(args) {
				programs, positional = append(programs, args[i+1].text), false
			}
			i++
		case strings.HasPrefix(text, "--source="):
			programs, positional = append(programs, strings.TrimPrefix(text, "--source=")), false
		case text == "--file" || text == "--include" || text == "--load" || text == "--exec" ||
			len(text) > 1 && text[0] == '-' && text[1] != '-' && strings.IndexByte("filE", text[1]) >= 0:
			return ReadListError{Program: "awk", Why: "with " + text + " reads a program or an extension from a file this check cannot see"}
		case strings.HasPrefix(text, "-") && text != "-":
		case positional:
			programs, positional = append(programs, text), false
		}
	}
	for _, program := range programs {
		if err := awkProgram(program); err != nil {
			return err
		}
	}
	return nil
}

func awkProgram(program string) error {
	refused := func(why string) error { return ReadListError{Program: "awk", Why: why} }
	letter := func(c byte) bool { return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
	printing, depth, last := false, 0, byte('\n')
	for i := 0; i < len(program); i++ {
		c := program[i]
		switch {
		case c == '"':
			for i++; i < len(program) && program[i] != '"'; i++ {
				if program[i] == '\\' {
					i++
				}
			}
		case c == '/' && strings.IndexByte("\n(,{};!~&|=?:", last) >= 0:
			for i++; i < len(program) && program[i] != '/'; i++ {
				if program[i] == '\\' {
					i++
				}
			}
		case c == '#':
			for i+1 < len(program) && program[i+1] != '\n' {
				i++
			}
		case c == '@':
			return refused("with @ loads an extension or calls a function by name")
		case c == '|' && i+1 < len(program) && program[i+1] == '|':
			i++
		case c == '|':
			return refused("with | pipes to or from a command")
		case c == '>' && printing && depth == 0:
			return refused("with print > writes a file")
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ';' || c == '{' || c == '}' || c == '\n':
			printing, depth = false, 0
		case letter(c):
			start := i
			for i+1 < len(program) && (letter(program[i+1]) || '0' <= program[i+1] && program[i+1] <= '9') {
				i++
			}
			switch program[start : i+1] {
			case "system":
				return refused("with system() runs a shell command")
			case "print", "printf":
				printing = true
			}
		}
		if c != ' ' && c != '\t' {
			last = program[i]
		}
	}
	return nil
}
