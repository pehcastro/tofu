package widget

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	kilobyte = 1 << 10
	megabyte = 1 << 20
	thousand = 1000
)

const (
	ellipsis       = "…"
	maskFill       = "····"
	maskedRunes    = 4
	tailShownAbove = maskedRunes * 2
)

func Mask(secret string) string {
	runes := []rune(secret)
	if len(runes) <= tailShownAbove {
		return maskFill
	}
	return maskFill + string(runes[len(runes)-maskedRunes:])
}

func Cells(text string) int { return ansi.StringWidth(text) }

func Fit(text string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(text, width, ellipsis)
}

func Pad(text string, width int) string {
	if missing := width - Cells(text); missing > 0 {
		return text + strings.Repeat(" ", missing)
	}
	return text
}

func Lead(text string, width int) string {
	if missing := width - Cells(text); missing > 0 {
		return strings.Repeat(" ", missing) + text
	}
	return text
}

func Count(tokens int) string {
	if tokens < thousand {
		return strconv.Itoa(tokens)
	}
	return strconv.Itoa(tokens/thousand) + "k"
}

func Size(bytes int) string {
	switch {
	case bytes >= megabyte:
		return strconv.FormatFloat(float64(bytes)/megabyte, 'f', 1, 64) + " MB"
	case bytes >= kilobyte:
		return strconv.FormatFloat(float64(bytes)/kilobyte, 'f', 1, 64) + " KB"
	}
	return strconv.Itoa(bytes) + " bytes"
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
			case Cells(line)+1+Cells(word) <= width:
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
