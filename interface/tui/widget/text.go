package widget

import "strings"

const ellipsis = "…"

func Fit(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width == 1 {
		return ellipsis
	}
	return string(runes[:width-1]) + ellipsis
}

func Wrap(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			switch {
			case line == "":
				line = word
			case len([]rune(line))+1+len([]rune(word)) <= width:
				line += " " + word
			default:
				lines = append(lines, Fit(line, width))
				line = word
			}
		}
		lines = append(lines, Fit(line, width))
	}
	return lines
}
