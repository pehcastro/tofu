package widget

import (
	"strconv"
	"strings"
)

const (
	filledRune = "▓"
	emptyRune  = "░"
)

func Bar(fraction float64, width int) string {
	if width <= 0 {
		return ""
	}
	filled := min(max(int(fraction*float64(width)+0.5), 0), width)
	return strings.Repeat(filledRune, filled) + strings.Repeat(emptyRune, width-filled)
}

func Percent(fraction float64) string {
	return strconv.Itoa(int(fraction*100+0.5)) + "%"
}
