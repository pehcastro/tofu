package docs

import (
	"fmt"
	"strings"

	"tofu/internal/konst"
)

func Sections() []string {
	return []string{"What it is", "Where it lives", "Change it", "Check it", "Undo it"}
}

func Broken(name, source string) []string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	var broken []string
	if lines := strings.Count(source, "\n"); lines < konst.DocsPageLinesMin || lines > konst.DocsPageLinesMax {
		broken = append(broken, fmt.Sprintf("%s runs %d lines, and a page runs %d to %d", name, lines, konst.DocsPageLinesMin, konst.DocsPageLinesMax))
	}
	at := 0
	for _, section := range Sections() {
		found := strings.Index(source[at:], "\n## "+section+"\n")
		if found < 0 {
			broken = append(broken, fmt.Sprintf("%s has no ## %s after the sections before it", name, section))
			break
		}
		at += found + 1
	}
	if strings.Contains(source, "TOFU-") {
		broken = append(broken, name+" names a ticket")
	}
	return broken
}
