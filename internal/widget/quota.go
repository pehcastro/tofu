package widget

import (
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
)

const (
	filledRune     = "▓"
	emptyRune      = "░"
	percentCells   = 4
	hoursPerDay    = 24
	minutesPerHour = 60
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

func Quota(fraction float64, resetsAt, now time.Time) string {
	meter := Bar(fraction, konst.MeterBarWidthChars) + " " + Lead(Percent(fraction), percentCells)
	if resetsAt.IsZero() {
		return meter
	}
	return meter + "  resets " + Until(resetsAt.Sub(now))
}

func Until(left time.Duration) string {
	minutes := int(left.Minutes())
	if minutes <= 0 {
		return "now"
	}
	if minutes < minutesPerHour {
		return "in " + strconv.Itoa(minutes) + "m"
	}
	hours := minutes / minutesPerHour
	if hours < hoursPerDay {
		return "in " + both(hours, "h", minutes%minutesPerHour, "m")
	}
	return "in " + both(hours/hoursPerDay, "d", hours%hoursPerDay, "h")
}

func both(whole int, unit string, part int, partUnit string) string {
	text := strconv.Itoa(whole) + unit
	if part == 0 {
		return text
	}
	return text + " " + strconv.Itoa(part) + partUnit
}
