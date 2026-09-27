package look

import (
	"strconv"
	"strings"
	"time"
)

var asciiReplacer = strings.NewReplacer(
	"←", "<", "→", ">", "↑", "^", "↓", "v",
	"↖", "\\", "↗", "/", "↘", "\\", "↙", "/",
	"·", ".", "•", "o", "∙", ".", "●", "*", "○", "o", "◌", "o",
	"░", ".", "▒", ":", "▓", "#", "█", "#",
	"━", "=", "┃", "|", "─", "-", "│", "|",
	"╭", "+", "╮", "+", "╰", "+", "╯", "+",
	"┌", "+", "┐", "+", "└", "+", "┘", "+",
	"┏", "+", "┓", "+", "┗", "+", "┛", "+",
	"├", "+", "┤", "+", "┬", "+", "┴", "+", "┼", "+",
	"┣", "+", "┫", "+", "┳", "+", "┻", "+", "╋", "+",
	"›", ">", "▌", "|", "▯", "|", "×", "x", "…", ".", "–", "-", "−", "-", "±", "+",
	"✓", "v", "✗", "x", "▾", "v", "▸", ">", "⟩", ">", "◆", "*", "◇", "o", "▣", "#", "⟳", "@", "▀", "'", "▄", ",",
	"⠋", ".", "⠙", "o", "⠚", "O", "⠞", "O", "⠖", "O", "⠉", "o", "⠈", ".",
	"⠁", ".", "⠒", "o", "⠂", ".", "⠲", "O", "⠴", ".", "⠤", "o", "⠄", ".",
	"⠠", ".", "⠦", "o", "⠐", ".", "⠓", "O",
)

func ASCII(view string) string { return asciiReplacer.Replace(view) }

func SignedLines(n int64) string {
	sign, magnitude := "+", uint64(n)
	if n < 0 {
		sign, magnitude = "-", uint64(-(n+1))+1
	}
	magnitudes := [...]struct {
		threshold uint64
		suffix    string
	}{{1_000_000_000_000_000_000, "E"}, {1_000_000_000_000_000, "P"}, {1_000_000_000_000, "T"}, {1_000_000_000, "B"}, {1_000_000, "M"}, {1_000, "k"}}
	for _, unit := range magnitudes {
		if magnitude < unit.threshold {
			continue
		}
		whole := magnitude / unit.threshold
		value := strconv.FormatUint(whole, 10)
		if tenth := magnitude % unit.threshold / (unit.threshold / 10); whole < 10 && tenth > 0 {
			value += "." + strconv.FormatUint(tenth, 10)
		}
		return sign + value + unit.suffix
	}
	return sign + strconv.FormatUint(magnitude, 10)
}

func Age(age time.Duration) string {
	switch {
	case age < time.Second:
		return "just now"
	case age < time.Minute:
		return strconv.FormatInt(int64(age/time.Second), 10) + "s ago"
	case age < time.Hour:
		return strconv.FormatInt(int64(age/time.Minute), 10) + "m ago"
	case age < 24*time.Hour:
		return strconv.FormatInt(int64(age/time.Hour), 10) + "h ago"
	default:
		return strconv.FormatInt(int64(age/(24*time.Hour)), 10) + "d ago"
	}
}
