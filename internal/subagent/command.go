package subagent

import (
	"path"
	"strings"
)

const shellOperatorChars = "|;&<>()"

func commandFields(command string) []string {
	var fields []string
	word := &strings.Builder{}
	flush := func() {
		if word.Len() > 0 {
			fields = append(fields, word.String())
			word.Reset()
		}
	}
	for i := 0; i < len(command); i++ {
		character := command[i]
		switch {
		case character == ' ' || character == '\t' || character == '\n' || character == '\r':
			flush()
		case character == '\'' || character == '"':
			continue
		case strings.IndexByte(shellOperatorChars, character) >= 0:
			flush()
			if character == '>' && i+1 < len(command) && command[i+1] == '>' {
				i++
			}
			fields = append(fields, string(character))
		default:
			word.WriteByte(character)
		}
	}
	flush()
	return fields
}

func isOperator(field string) bool {
	return len(field) == 1 && strings.IndexByte(shellOperatorChars, field[0]) >= 0
}

func cleanedField(field string) string {
	return path.Clean(strings.ReplaceAll(field, `\`, "/"))
}

func toolName(field string) string {
	return strings.TrimSuffix(strings.ToLower(path.Base(field)), ".exe")
}

var recursesOnCurrentDirectory = map[string]bool{
	"gofmt":     true,
	"goimports": true,
}

var walksTheModuleWithNoPath = map[string]bool{
	"golangci-lint": true,
	"staticcheck":   true,
}

func namesTheWholeTree(tool, field string) bool {
	switch cleanedField(field) {
	case "...":
		return true
	case "all":
		return tool == "go"
	case ".":
		return recursesOnCurrentDirectory[tool]
	default:
		return false
	}
}

func walksWithoutAPath(tool string, rest []string) bool {
	if !walksTheModuleWithNoPath[tool] {
		return false
	}
	for _, field := range rest {
		if field != "run" && !strings.HasPrefix(field, "-") {
			return false
		}
	}
	return true
}

func treeWideRefusal(tool, field string) string {
	named := strings.TrimSpace(tool + " " + strings.ReplaceAll(field, "/", " "))
	return named + " is a tree-wide command: the parent runs it, not a child"
}

func commandPaths(command string) []string {
	fields := commandFields(command)
	if len(fields) == 0 {
		return nil
	}
	tool := ""
	var paths []string
	for i, field := range fields {
		if isOperator(field) {
			tool = ""
			continue
		}
		if tool == "" {
			tool = toolName(field)
		}
		if namesTheWholeTree(tool, field) {
			return []string{treeWideRefusal(tool, field)}
		}
		redirected := i > 0 && fields[i-1] == ">"
		if !redirected && (strings.HasPrefix(field, "-") || !strings.ContainsAny(field, `/\`)) {
			continue
		}
		if cleaned := cleanedField(field); cleaned != "." && cleaned != "/" {
			paths = append(paths, cleaned)
		}
	}
	leading := toolName(fields[0])
	if walksWithoutAPath(leading, fields[1:]) {
		return []string{treeWideRefusal(leading, "(no path given)")}
	}
	return paths
}
