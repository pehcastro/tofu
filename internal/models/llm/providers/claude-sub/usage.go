package claudesub

import (
	"cmp"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"tofu/internal/models/llm/quota"
	"tofu/internal/models/llm/wire/anthropic"
	"tofu/internal/models/transport"
)

const (
	UsageURLVariable = "TOFU_CLAUDE_USAGE_URL"
	usageURL         = "https://api.anthropic.com/api/oauth/usage"
	unifiedPrefix    = "anthropic-ratelimit-unified-"
	rejected         = "rejected"
	fiveHourWindow   = "5h"
	sevenDayWindow   = "7d"
	week             = 7 * 24 * time.Hour
	fiveHours        = 5 * time.Hour
)

func UsageEndpoint() quota.Endpoint {
	return quota.Endpoint{
		URL: cmp.Or(os.Getenv(UsageURLVariable), usageURL),
		SetHeaders: func(request *http.Request, account quota.Account) {
			request.Header.Set("User-Agent", anthropic.ClaudeCodeUserAgent(cmp.Or(account.ClientVersion, anthropic.PinnedClaudeCodeVersion)))
		},
		Parse:       parseUsage,
		FromHeaders: fromHeaders,
	}
}

type usageBucket struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

type usageLimit struct {
	Kind     string   `json:"kind"`
	Percent  *float64 `json:"percent"`
	ResetsAt string   `json:"resets_at"`
	Scope    struct {
		Model struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

type usageBody struct {
	FiveHour *usageBucket `json:"five_hour"`
	SevenDay *usageBucket `json:"seven_day"`
	Limits   []usageLimit `json:"limits"`
}

func parseUsage(body []byte, now time.Time) (quota.Report, error) {
	var payload usageBody
	if err := json.Unmarshal(body, &payload); err != nil {
		return quota.Report{}, transport.Fail("quota.FromAnthropicUsage", transport.KindProvider, err,
			"the anthropic usage payload is not the shape this endpoint documents")
	}
	report := quota.Report{Provider: quota.ClaudeSub, FetchedAt: now, Source: quota.SourceEndpoint}
	appendBucket(&report, fiveHourWindow, fiveHours, payload.FiveHour)
	appendBucket(&report, sevenDayWindow, week, payload.SevenDay)
	for _, limit := range payload.Limits {
		id, duration := scopedWindow(limit)
		if id == "" {
			continue
		}
		appendBucket(&report, id, duration, &usageBucket{Utilization: limit.Percent, ResetsAt: limit.ResetsAt})
	}
	return report, nil
}

func scopedWindow(limit usageLimit) (string, time.Duration) {
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
		return sevenDayWindow + quota.WindowModelMark + slug, week
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

func appendBucket(report *quota.Report, id string, duration time.Duration, bucket *usageBucket) {
	if bucket == nil {
		return
	}
	for _, existing := range report.Windows {
		if existing.ID == id {
			return
		}
	}
	reset, _ := time.Parse(time.RFC3339, strings.TrimSpace(bucket.ResetsAt))
	if bucket.Utilization == nil && reset.IsZero() {
		return
	}
	window := quota.Window{ID: id, Duration: duration, ResetsAt: reset.UTC()}
	if bucket.Utilization != nil {
		window.Used = quota.Used{Fraction: *bucket.Utilization / quota.PercentFull, Reported: true}
	}
	report.Windows = append(report.Windows, window)
}

func fromHeaders(header http.Header, now time.Time) quota.Report {
	report := quota.Report{Provider: quota.ClaudeSub, FetchedAt: now, Source: quota.SourceHeaders}
	for _, unified := range []struct {
		id       string
		duration time.Duration
	}{{fiveHourWindow, fiveHours}, {sevenDayWindow, week}} {
		prefix := unifiedPrefix + unified.id + "-"
		heard := usageBucket{}
		if used := quota.HeaderNumber(header, prefix+"utilization"); used != nil {
			percent := *used * quota.PercentFull
			if strings.TrimSpace(header.Get(prefix+"status")) == rejected {
				percent = max(percent, quota.PercentFull)
			}
			heard.Utilization = &percent
		}
		if reset := quota.HeaderNumber(header, prefix+"reset"); reset != nil {
			if at := quota.EpochTime(*reset); !at.IsZero() {
				heard.ResetsAt = at.Format(time.RFC3339)
			}
		}
		appendBucket(&report, unified.id, unified.duration, &heard)
	}
	return report
}
