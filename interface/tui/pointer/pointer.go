package pointer

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type Cell struct{ X, Y int }
type Mods struct{ Alt, Shift, Ctrl bool }

func Distance(a, b Cell) int {
	dx, dy := a.X-b.X, a.Y-b.Y
	return max(dx, -dx, dy, -dy)
}

var Reference = regexp.MustCompile(`\[(?:[a-z][a-z0-9-]*#[a-zA-Z0-9-]+|#[a-zA-Z0-9-]+|&[a-zA-Z0-9-]+(?: (?:\{[0-9]+\}|[0-9]+))?)\]`)

func SplitReference(ref string) (kind, id string) {
	content := strings.Trim(ref, "[]")
	if strings.HasPrefix(content, "#") {
		return "", content
	}
	if at := strings.IndexByte(content, '#'); at > 0 {
		return content[:at], content[at:]
	}
	return "", ""
}

func ReferenceAt(view string, x, y int) string {
	lines := strings.Split(view, "\n")
	if y < 0 || y >= len(lines) {
		return ""
	}
	line := ansi.Strip(lines[y])
	for _, span := range Reference.FindAllStringIndex(line, -1) {
		ref := line[span[0]:span[1]]
		if left := ansi.StringWidth(line[:span[0]]); x >= left && x < left+ansi.StringWidth(ref) {
			return ref
		}
	}
	return ""
}

func TextHit(line, label string, x int) bool {
	at := strings.Index(line, label)
	if at < 0 {
		return false
	}
	start := ansi.StringWidth(line[:at])
	return x >= start && x < start+ansi.StringWidth(label)
}
