package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"tofu/interface/tui/theme"
	"tofu/internal/konst"
)

const (
	reportIndent = "  "
	jsonFlag     = "--json"

	quotaWarnFraction = 0.5
	quotaFullFraction = 0.8
)

type palette int

const (
	plain palette = iota
	coloured
)

func paletteOf(out io.Writer) palette {
	file, isFile := out.(*os.File)
	if isFile && term.IsTerminal(file.Fd()) {
		return coloured
	}
	return plain
}

func (p palette) settled(text string) string {
	switch p {
	case plain:
		return text
	case coloured:
		return theme.Accent().Render(text)
	}
	panic("tofu: unknown palette")
}

func (p palette) unsettled(text string) string {
	switch p {
	case plain:
		return text
	case coloured:
		return theme.Warn().Render(text)
	}
	panic("tofu: unknown palette")
}

func (p palette) full(fraction float64, text string) string {
	switch {
	case p == plain, fraction < quotaWarnFraction:
		return text
	case fraction < quotaFullFraction:
		return theme.Warn().Render(text)
	}
	return theme.Fail().Render(text)
}

func headline(left, painted string, plainWidth int) string {
	pad := konst.ReportWidthChars - len(left) - plainWidth
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + painted
}

func labelled(label, text string) string {
	pad := konst.ReportLabelColumnChars - len(label)
	if pad < 1 {
		pad = 1
	}
	return reportIndent + label + strings.Repeat(" ", pad) + text
}

func wrapped(label, text string) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= konst.ReportWidthChars:
			line += " " + word
		default:
			lines = append(lines, labelled(label, line))
			label, line = "", reportIndent+word
		}
	}
	return append(lines, labelled(label, line))
}

func blockerLines(label, what, command string) []string {
	return append(wrapped(label, what), labelled("", "run "+command))
}

func relativeToRoot(root, text string) string {
	return strings.ReplaceAll(text, root+string(os.PathSeparator), "")
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
