package quota

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	"tofu/internal/transport"
)

const (
	fiveHourWindow   = "5h"
	sevenDayWindow   = "7d"
	percentFull      = 100
	week             = 7 * 24 * time.Hour
	fiveHours        = 5 * time.Hour
	epochMillisFloor = 1e12
)

func windowID(duration time.Duration) string {
	if duration >= 24*time.Hour {
		return strconv.Itoa(int(math.Round(duration.Hours()/24))) + "d"
	}
	return strconv.Itoa(max(1, int(math.Round(duration.Hours())))) + "h"
}

func epochTime(seconds float64) time.Time {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return time.Time{}
	}
	if seconds >= epochMillisFloor {
		return time.UnixMilli(int64(seconds)).UTC()
	}
	return time.Unix(int64(seconds), 0).UTC()
}

type anthropicBucket struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

type anthropicLimit struct {
	Kind     string   `json:"kind"`
	Percent  *float64 `json:"percent"`
	ResetsAt string   `json:"resets_at"`
	Scope    struct {
		Model struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

type anthropicUsageBody struct {
	FiveHour *anthropicBucket `json:"five_hour"`
	SevenDay *anthropicBucket `json:"seven_day"`
	Limits   []anthropicLimit `json:"limits"`
}

func FromAnthropicUsage(body []byte, now time.Time) (Report, error) {
	var payload anthropicUsageBody
	if err := json.Unmarshal(body, &payload); err != nil {
		return Report{}, transport.Fail("quota.FromAnthropicUsage", transport.KindProvider, nil,
			"the anthropic usage payload is not the shape this endpoint documents")
	}
	report := Report{Provider: Anthropic, FetchedAt: now}
	report.appendBucket(fiveHourWindow, fiveHours, payload.FiveHour)
	report.appendBucket(sevenDayWindow, week, payload.SevenDay)
	for _, limit := range payload.Limits {
		id, duration := scopedWindow(limit)
		if id == "" {
			continue
		}
		report.appendBucket(id, duration, &anthropicBucket{Utilization: limit.Percent, ResetsAt: limit.ResetsAt})
	}
	return report, nil
}

func scopedWindow(limit anthropicLimit) (string, time.Duration) {
	switch limit.Kind {
	case "session":
		return fiveHourWindow, fiveHours
	case "weekly_all":
		return sevenDayWindow, week
	case "weekly_scoped":
		slug := slugify(limit.Scope.Model.DisplayName)
		if slug == "" {
			return "", 0
		}
		return sevenDayWindow + ":" + slug, week
	}
	return "", 0
}

func slugify(name string) string {
	var out strings.Builder
	for _, letter := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case letter >= 'a' && letter <= 'z', letter >= '0' && letter <= '9':
			out.WriteRune(letter)
		default:
			out.WriteByte('-')
		}
	}
	return strings.Trim(out.String(), "-")
}

func (r *Report) appendBucket(id string, duration time.Duration, bucket *anthropicBucket) {
	if bucket == nil {
		return
	}
	for _, existing := range r.Windows {
		if existing.ID == id {
			return
		}
	}
	reset, _ := time.Parse(time.RFC3339, strings.TrimSpace(bucket.ResetsAt))
	if bucket.Utilization == nil && reset.IsZero() {
		return
	}
	window := Window{ID: id, Duration: duration, ResetsAt: reset.UTC()}
	if bucket.Utilization != nil {
		window.Used = Used{Fraction: *bucket.Utilization / percentFull, Reported: true}
	}
	r.Windows = append(r.Windows, window)
}

type codexWindowBody struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds *float64 `json:"limit_window_seconds"`
	ResetAfterSeconds  *float64 `json:"reset_after_seconds"`
	ResetAt            *float64 `json:"reset_at"`
}

type codexUsageBody struct {
	PlanType  string `json:"plan_type"`
	RateLimit *struct {
		LimitReached *bool            `json:"limit_reached"`
		Primary      *codexWindowBody `json:"primary_window"`
		Secondary    *codexWindowBody `json:"secondary_window"`
	} `json:"rate_limit"`
	Credits *struct {
		HasCredits          bool `json:"has_credits"`
		Unlimited           bool `json:"unlimited"`
		OverageLimitReached bool `json:"overage_limit_reached"`
	} `json:"credits"`
	SpendControl *struct {
		Reached bool `json:"reached"`
	} `json:"spend_control"`
}

func FromCodexUsage(body []byte, now time.Time) (Report, error) {
	var payload codexUsageBody
	if err := json.Unmarshal(body, &payload); err != nil {
		return Report{}, transport.Fail("quota.FromCodexUsage", transport.KindProvider, nil,
			"the codex usage payload is not the shape this endpoint documents")
	}
	report := Report{Provider: Codex, Plan: payload.PlanType, FetchedAt: now}
	if payload.RateLimit == nil {
		return report, nil
	}
	report.LimitReached = payload.RateLimit.LimitReached != nil && *payload.RateLimit.LimitReached
	if window, ok := codexWindow("primary", payload.RateLimit.Primary, now); ok {
		report.Windows = append(report.Windows, window)
	}
	if window, ok := codexWindow("secondary", payload.RateLimit.Secondary, now); ok {
		report.Windows = append(report.Windows, window)
	}
	report.CreditOverage = report.LimitReached &&
		payload.Credits != nil &&
		(payload.Credits.Unlimited || payload.Credits.HasCredits) &&
		!payload.Credits.OverageLimitReached &&
		(payload.SpendControl == nil || !payload.SpendControl.Reached)
	return report, nil
}

func codexWindow(name string, body *codexWindowBody, now time.Time) (Window, bool) {
	if body == nil {
		return Window{}, false
	}
	window := Window{ID: name}
	if body.LimitWindowSeconds != nil {
		window.Duration = time.Duration(*body.LimitWindowSeconds) * time.Second
		window.ID = windowID(window.Duration)
	}
	if body.UsedPercent != nil {
		window.Used = Used{Fraction: *body.UsedPercent / percentFull, Reported: true}
	}
	switch {
	case body.ResetAt != nil:
		window.ResetsAt = epochTime(*body.ResetAt)
	case body.ResetAfterSeconds != nil:
		window.ResetsAt = now.Add(time.Duration(*body.ResetAfterSeconds) * time.Second).UTC()
	}
	if !window.Used.Reported && window.ResetsAt.IsZero() && window.Duration == 0 {
		return Window{}, false
	}
	return window, true
}
