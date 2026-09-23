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

func commandPaths(command string) []string {
	fields := commandFields(command)
	var paths []string
	for i, field := range fields {
		if len(field) == 1 && strings.IndexByte(shellOperatorChars, field[0]) >= 0 {
			continue
		}
		redirected := i > 0 && fields[i-1] == ">"
		if !redirected && (strings.HasPrefix(field, "-") || !strings.ContainsAny(field, `/\`)) {
			continue
		}
		cleaned := path.Clean(strings.ReplaceAll(field, `\`, "/"))
		if cleaned == "." || cleaned == "/" {
			continue
		}
		paths = append(paths, cleaned)
	}
	return paths
}
