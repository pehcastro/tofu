package rule

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

const indentedBlockWidth = 4

func withoutQuotations(path string, lines []string) []string {
	switch {
	case strings.HasSuffix(path, ".md"):
		return withoutMarkdownQuotations(lines)
	case strings.HasSuffix(path, ".go"):
		return withoutGoQuotations(lines)
	default:
		return lines
	}
}

func withoutGoQuotations(lines []string) []string {
	source := []byte(strings.Join(lines, "\n"))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", source, parser.ParseComments)
	if err != nil {
		return lines
	}
	authored := append([]byte(nil), source...)
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) {
			return true
		}
		offset := fset.Position(lit.Pos()).Offset
		for i := offset; i < offset+len(lit.Value); i++ {
			if authored[i] != '\n' {
				authored[i] = ' '
			}
		}
		return true
	})
	return strings.Split(string(authored), "\n")
}

func withoutMarkdownQuotations(lines []string) []string {
	authored := make([]string, len(lines))
	openFence := ""
	previousBlank := true
	previousIndentedBlock := false
	for i, line := range lines {
		body := strings.TrimLeft(line, " \t")
		indent := indentWidth(line[:len(line)-len(body)])
		fence := fenceMarker(body)
		indentedBlock := false
		switch {
		case openFence != "":
			if strings.HasPrefix(body, openFence) {
				openFence = ""
			}
		case fence != "" && indent < indentedBlockWidth:
			openFence = fence
		case indent >= indentedBlockWidth && (previousBlank || previousIndentedBlock):
			indentedBlock = true
		default:
			authored[i] = withoutInlineSpans(withoutInlineSpans(line, '`'), '"')
		}
		previousBlank = body == ""
		previousIndentedBlock = indentedBlock
	}
	return authored
}

func fenceMarker(body string) string {
	switch {
	case strings.HasPrefix(body, "```"):
		return "```"
	case strings.HasPrefix(body, "~~~"):
		return "~~~"
	}
	return ""
}

func indentWidth(indent string) int {
	return len(indent) + (indentedBlockWidth-1)*strings.Count(indent, "\t")
}

func withoutInlineSpans(line string, delimiter byte) string {
	var authored strings.Builder
	rest := line
	for {
		open := strings.IndexByte(rest, delimiter)
		if open < 0 {
			authored.WriteString(rest)
			return authored.String()
		}
		authored.WriteString(rest[:open])
		run := 0
		for open+run < len(rest) && rest[open+run] == delimiter {
			run++
		}
		after := rest[open+run:]
		closed := strings.Index(after, rest[open:open+run])
		if closed < 0 {
			authored.WriteString(after)
			return authored.String()
		}
		rest = after[closed+run:]
	}
}
