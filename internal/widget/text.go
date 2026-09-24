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

func Column[T any](items []T, text func(T) string, least int) int {
	width := least
	for _, item := range items {
		width = max(width, Cells(text(item)))
	}
	return width
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
			if line != "" {
				if Cells(line)+1+Cells(word) <= width {
					line += " " + word
					continue
				}
				lines = append(lines, line)
			}
			for Cells(word) > width {
				head := ansi.Truncate(word, width, "")
				if head == "" {
					head, _ = ansi.FirstGraphemeCluster(word, ansi.GraphemeWidth)
				}
				rest := ansi.TruncateLeft(word, Cells(head), "")
				if rest == "" {
					break
				}
				lines = append(lines, head)
				word = rest
			}
			line = word
		}
		lines = append(lines, line)
	}
	return lines
}
