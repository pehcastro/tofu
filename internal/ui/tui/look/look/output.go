package look

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const tabCells = 4

func OutputLine(line string) string {
	text, styled := sanitize(overwritten(line))
	if styled {
		return text
	}
	return coloured(text, semanticColor(text))
}

func overwritten(line string) string {
	segments := strings.Split(line, "\r")
	for i := len(segments) - 1; i > 0; i-- {
		if ansi.StringWidth(segments[i]) > 0 {
			return segments[i]
		}
	}
	return segments[0]
}

func sanitize(line string) (text string, styled bool) {
	var out strings.Builder
	var state byte
	for len(line) > 0 {
		seq, width, n, next := ansi.DecodeSequence(line, state, nil)
		line, state = line[n:], next
		switch {
		case width > 0:
			out.WriteString(seq)
		case seq == "\t":
			out.WriteString(strings.Repeat(" ", tabCells))
		case isSGR(seq):
			out.WriteString(seq)
			styled = true
		}
	}
	return out.String(), styled
}

func isSGR(seq string) bool {
	return len(seq) >= 3 && strings.HasPrefix(seq, "\x1b[") && seq[len(seq)-1] == 'm' && strings.Trim(seq[2:len(seq)-1], "0123456789;:") == ""
}

func semanticColor(text string) Color {
	lower := strings.ToLower(text)
	has := func(words ...string) bool {
		return slices.ContainsFunc(words, func(word string) bool { return strings.Contains(lower, word) })
	}
	switch {
	case has("failed", "error", "panic"):
		return Red
	case has("warn", "retry"):
		return Amber
	case has("passed", "compiled", "rebuilt", "listening", "finished", "generated", " 200 "):
		return Mint
	case has("file change", "running", "incremental", "cache refreshed"):
		return Blue
	}
	return MutedColor
}
