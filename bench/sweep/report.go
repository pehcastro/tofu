package sweep

import (
	"fmt"
	"strings"
)

func Render(machine, date string, lines []PackageLines) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench cross-package sweep: %s\n\n", date)
	fmt.Fprintf(b, "Machine: %s. Reads bench source only, no live model call, no .tofu write. Run: `go test ./bench/sweep/ -count=1`.\n\n", machine)

	renderReaders(b, ".tofu/sessions", SessionReaders())
	renderReaders(b, ".tofu/log", LogReaders())
	renderDuplications(b)
	renderShapes(b)
	renderNotDuplications(b)
	renderLines(b, lines)

	return b.String()
}

func renderReaders(b *strings.Builder, dir string, readers []Reader) {
	fmt.Fprintf(b, "## Readers of `%s`\n\n", dir)
	fmt.Fprintf(b, "**%d.** One is the shared reader; the rest hold their own code.\n\n", len(readers))
	for _, r := range readers {
		fmt.Fprintf(b, "- **%s**: %s\n", r.Package, r.Differs)
	}
	fmt.Fprintf(b, "\n")
}

func renderDuplications(b *strings.Builder) {
	b.WriteString("## Cross-package duplication\n\n")
	for _, d := range Duplications() {
		fmt.Fprintf(b, "- **%s**\n  vs **%s**\n  recommendation: %s\n\n", d.A, d.B, d.Recommendation)
	}
}

func renderShapes(b *strings.Builder) {
	b.WriteString("## Shapes computed in more than one place, against bench/stat\n\n")
	for _, s := range RepeatedShapes() {
		fmt.Fprintf(b, "- **%s**: %s. %s\n", s.Name, strings.Join(s.Packages, ", "), s.Against)
	}
	b.WriteString("\n")
}

func renderNotDuplications(b *strings.Builder) {
	b.WriteString("## Looks like duplication and is not\n\n")
	for _, n := range NotDuplications() {
		fmt.Fprintf(b, "- **%s**: %s\n", n.What, n.Why)
	}
	b.WriteString("\n")
}

func renderLines(b *strings.Builder, lines []PackageLines) {
	b.WriteString("## Source and test lines, per bench package, 2026-09-23\n\n")
	b.WriteString("Top-level packages only; a package with its own subpackage (bench/harness/task, bench/tools/candidates, and others) is counted at its own top level here. Baseline for a later sweep to diff against.\n\n")
	b.WriteString("| Package | Source | Test |\n|---|---|---|\n")
	totalSource, totalTest := 0, 0
	for _, l := range lines {
		fmt.Fprintf(b, "| %s | %d | %d |\n", l.Package, l.Source, l.Test)
		totalSource += l.Source
		totalTest += l.Test
	}
	fmt.Fprintf(b, "| **total** | **%d** | **%d** |\n\n", totalSource, totalTest)
}
