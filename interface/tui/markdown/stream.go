package markdown

import "strings"

func Boundary(content string) int {
	boundary := 0
	fenceOpen := false
	start := 0
	for index := 0; index < len(content); index++ {
		if content[index] != '\n' {
			continue
		}
		line := content[start:index]
		closedFence := isFenceLine(line)
		if closedFence {
			fenceOpen = !fenceOpen
		}
		end := index + 1
		start = end
		switch {
		case fenceOpen:
		case closedFence:
			boundary = end
		case strings.TrimSpace(line) == "" && !opensConstruct(lastNonBlankLine(content[:index])):
			boundary = end
		case isHeading(line):
			boundary = end
		}
	}
	return boundary
}

func isFenceLine(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" {
		return false
	}
	char := trimmed[0]
	if char != '`' && char != '~' {
		return false
	}
	run := 0
	for run < len(trimmed) && trimmed[run] == char {
		run++
	}
	return run >= 3
}

func isHeading(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return false
	}
	hashes := 0
	for hashes < len(trimmed) && trimmed[hashes] == '#' {
		hashes++
	}
	if hashes == 0 || hashes > 6 {
		return false
	}
	return hashes == len(trimmed) || trimmed[hashes] == ' '
}

func lastNonBlankLine(s string) string {
	lines := strings.Split(s, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if strings.TrimSpace(lines[index]) != "" {
			return lines[index]
		}
	}
	return ""
}

func opensConstruct(line string) bool {
	if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
		return true
	}
	trimmed := strings.TrimLeft(line, " \t")
	switch {
	case trimmed == "":
		return false
	case trimmed[0] == '>':
		return true
	case isListMarker(trimmed):
		return true
	}
	return false
}

func isListMarker(line string) bool {
	switch line[0] {
	case '-', '*', '+':
		return len(line) > 1 && (line[1] == ' ' || line[1] == '\t')
	}
	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits > 9 || digits >= len(line) {
		return false
	}
	if line[digits] != '.' && line[digits] != ')' {
		return false
	}
	return digits+1 < len(line) && (line[digits+1] == ' ' || line[digits+1] == '\t')
}
