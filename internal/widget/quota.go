package widget

import (
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
)

const (
	filledRune       = "▓"
	emptyRune        = "░"
	percentCells     = 4
	hoursPerDay      = 24
	minutesPerHour   = 60
	secondsPerMinute = 60
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
	if left := resetsAt.Sub(now); left > 0 {
		return meter + "  resets in " + Until(left)
	}
	return meter + "  resets now"
}

func Until(d time.Duration) string {
	seconds := max(int(d.Seconds()), 0)
	if seconds < secondsPerMinute {
		return strconv.Itoa(seconds) + "s"
	}
	minutes := seconds / secondsPerMinute
	if minutes < minutesPerHour {
		return both(minutes, "m", seconds%secondsPerMinute, "s")
	}
	hours := minutes / minutesPerHour
	if hours < hoursPerDay {
		return both(hours, "h", minutes%minutesPerHour, "m")
	}
	return both(hours/hoursPerDay, "d", hours%hoursPerDay, "h")
}

func both(whole int, unit string, part int, partUnit string) string {
	text := strconv.Itoa(whole) + unit
	if part == 0 {
		return text
	}
	return text + " " + strconv.Itoa(part) + partUnit
}
