package recall

import (
	"strings"

	"tofu/internal/konst"
)

func DistilLastWord(raw string) string {
	return oneLineCapped(raw, konst.CarrySignpostBytes)
}

func oneLineCapped(text string, limit int) string {
	var line strings.Builder
	gap := false
	for _, letter := range text {
		if line.Len() >= limit {
			return line.String() + " ..."
		}
		if letter == '\n' || letter == '\r' || letter == '\t' || letter == ' ' {
			gap = line.Len() > 0
			continue
		}
		if gap {
			line.WriteByte(' ')
			gap = false
		}
		line.WriteRune(letter)
	}
	return line.String()
}

func lastWordCandidates(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
