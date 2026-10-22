package subagent

import (
	"regexp"
	"strings"
)

var ticketPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,15}-\d+(?:-\d+)?\b`)

func TicketID(task string) string {
	return ticketPattern.FindString(task)
}

func AcceptanceLines(task string) []string {
	lines := strings.Split(task, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## Acceptance" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	return acceptanceBullets(lines[start:])
}

func acceptanceBullets(lines []string) []string {
	var found []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			found = append(found, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "## "):
			flush()
			return found
		case strings.HasPrefix(line, "- "):
			flush()
			current.WriteString(strings.TrimPrefix(line, "- "))
		case current.Len() > 0 && strings.HasPrefix(line, "  ") && trimmed != "":
			current.WriteString(" ")
			current.WriteString(trimmed)
		default:
			flush()
		}
	}
	flush()
	return found
}
