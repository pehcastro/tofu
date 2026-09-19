package rule

import "strings"

const indentedBlockWidth = 4

func withoutQuotations(path string, lines []string) []string {
	if !strings.HasSuffix(path, ".md") {
		return lines
	}
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
