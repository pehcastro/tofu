package markdown

import "strings"

func Boundary(content string) int {
	boundary := 0
	fenceOpen := false
	lastRealLine := ""
	start := 0
	for index := 0; index < len(content); index++ {
		if content[index] != '\n' {
			continue
		}
		line := content[start:index]
		fenceLine := isFenceLine(line)
		wasFenceOpen := fenceOpen
		if fenceLine {
			fenceOpen = !fenceOpen
		}
		end := index + 1
		start = end
		switch {
		case fenceOpen:
		case fenceLine:
			if !opensConstruct(lastRealLine) {
				boundary = end
			}
		case strings.TrimSpace(line) == "" && safeBlankBoundary(lastRealLine, content, end):
			boundary = end
		case isHeading(line) && !opensConstruct(lastRealLine):
			boundary = end
		}
		if !wasFenceOpen && !fenceLine && strings.TrimSpace(line) != "" {
			lastRealLine = line
		}
	}
	return boundary
}

func safeBlankBoundary(lastRealLine, content string, boundary int) bool {
	if opensConstruct(lastRealLine) {
		return false
	}
	return !isSetextUnderline(firstNonBlankLine(content[boundary:]))
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

func firstNonBlankLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
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
	case strings.ContainsRune(line, '|'):
		return true
	case isSetextUnderline(trimmed):
		return true
	}
	return false
}

func isSetextUnderline(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	char := trimmed[0]
	if char != '=' && char != '-' {
		return false
	}
	for index := 0; index < len(trimmed); index++ {
		if trimmed[index] != char {
			return false
		}
	}
	return true
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
