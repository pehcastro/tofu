package replace

import (
	"strings"

	"tofu/internal/konst"
)

func indentAt[T ~string | ~[]byte](prefix T) int {
	lookback := max(len(prefix), konst.CodemodIndentLookbackBytes) - konst.CodemodIndentLookbackBytes
	indent := 0
	for i := len(prefix) - 1; i >= lookback; i-- {
		switch prefix[i] {
		case '\n':
			return indent
		case ' ':
			indent++
		default:
			indent = 0
		}
	}
	if lookback == 0 {
		return indent
	}
	return 0
}

func reindent(text string, from, to int) string {
	if from == to || !strings.Contains(text, "\n") {
		return text
	}
	lines := strings.Split(text, "\n")
	if from > to {
		cut := strings.Repeat(" ", from-to)
		for i, line := range lines {
			lines[i] = strings.TrimPrefix(line, cut)
		}
		return strings.Join(lines, "\n")
	}
	pad := strings.Repeat(" ", to-from)
	for i := 1; i < len(lines); i++ {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}
