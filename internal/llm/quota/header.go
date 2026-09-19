package quota

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	fiveHourWindow   = "5h"
	sevenDayWindow   = "7d"
	percentFull      = 100
	week             = 7 * 24 * time.Hour
	fiveHours        = 5 * time.Hour
	epochMillisFloor = 1e12
)

func FromHeaders(provider Provider, header http.Header, now time.Time) Report {
	report := Report{Provider: provider, Source: SourceHeaders, FetchedAt: now}
	switch provider {
	case Anthropic:
		report.Windows = anthropicHeaderWindows(header)
	case Codex:
		report.Windows = codexHeaderWindows(header)
	default:
		panic("quota: unknown provider " + string(provider))
	}
	return report
}

func anthropicHeaderWindows(header http.Header) []Window {
	windows := make([]Window, 0, 3)
	for _, name := range []string{fiveHourWindow, sevenDayWindow, "7d_oi"} {
		prefix := "anthropic-ratelimit-unified-" + name + "-"
		fraction, reported := headerNumber(header, prefix+"utilization")
		seconds, hasReset := headerNumber(header, prefix+"reset")
		if !reported && !hasReset {
			continue
		}
		duration := week
		if name == fiveHourWindow {
			duration = fiveHours
		}
		windows = append(windows, Window{
			ID:       name,
			Duration: duration,
			Used:     Used{Fraction: fraction, Reported: reported},
			ResetsAt: epochTime(seconds),
		})
	}
	return windows
}

func codexHeaderWindows(header http.Header) []Window {
	windows := make([]Window, 0, 2)
	for _, name := range []string{"primary", "secondary"} {
		percent, reported := headerNumber(header, "x-codex-"+name+"-used-percent")
		minutes, hasMinutes := headerNumber(header, "x-codex-"+name+"-window-minutes")
		resetAt, hasReset := headerNumber(header, "x-codex-"+name+"-reset-at")
		if !reported && !hasMinutes && !hasReset {
			continue
		}
		window := Window{
			ID:       name,
			Used:     Used{Fraction: percent / percentFull, Reported: reported},
			ResetsAt: epochTime(resetAt),
		}
		if hasMinutes {
			window.Duration = time.Duration(minutes) * time.Minute
			window.ID = windowID(window.Duration)
		}
		windows = append(windows, window)
	}
	return windows
}

func windowID(duration time.Duration) string {
	if duration >= 24*time.Hour {
		return strconv.Itoa(int(math.Round(duration.Hours()/24))) + "d"
	}
	return strconv.Itoa(max(1, int(math.Round(duration.Hours())))) + "h"
}

func headerNumber(header http.Header, name string) (float64, bool) {
	raw := strings.TrimSpace(header.Get(name))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func epochTime(value float64) time.Time {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return time.Time{}
	}
	if value >= epochMillisFloor {
		return time.UnixMilli(int64(value)).UTC()
	}
	return time.Unix(int64(value), 0).UTC()
}
