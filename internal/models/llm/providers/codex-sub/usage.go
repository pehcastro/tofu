package codexsub

import (
	"cmp"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"tofu/internal/models/llm/quota"
	"tofu/internal/models/transport"
)

const (
	UsageURLVariable = "TOFU_CODEX_USAGE_URL"
	usageURL         = "https://chatgpt.com/backend-api/wham/usage"
	headerPrefix     = "x-codex-"
)

func UsageEndpoint() quota.Endpoint {
	return quota.Endpoint{
		URL: cmp.Or(os.Getenv(UsageURLVariable), usageURL),
		SetHeaders: func(request *http.Request, account quota.Account) {
			request.Header.Set("User-Agent", "tofu")
			if account.AccountID != "" {
				request.Header.Set("ChatGPT-Account-Id", account.AccountID)
			}
		},
		Parse:       parseUsage,
		FromHeaders: fromHeaders,
	}
}

type windowBody struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds *float64 `json:"limit_window_seconds"`
	ResetAfterSeconds  *float64 `json:"reset_after_seconds"`
	ResetAt            *float64 `json:"reset_at"`
}

type usageBody struct {
	PlanType  string `json:"plan_type"`
	RateLimit *struct {
		LimitReached *bool       `json:"limit_reached"`
		Primary      *windowBody `json:"primary_window"`
		Secondary    *windowBody `json:"secondary_window"`
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

func parseUsage(body []byte, now time.Time) (quota.Report, error) {
	var payload usageBody
	if err := json.Unmarshal(body, &payload); err != nil {
		return quota.Report{}, transport.Fail("quota.FromCodexUsage", transport.KindProvider, err,
			"the codex usage payload is not the shape this endpoint documents")
	}
	report := quota.Report{Provider: quota.CodexSub, Plan: payload.PlanType, FetchedAt: now, Source: quota.SourceEndpoint}
	if payload.RateLimit == nil {
		return report, nil
	}
	report.LimitReached = payload.RateLimit.LimitReached != nil && *payload.RateLimit.LimitReached
	if window, ok := windowOf("primary", payload.RateLimit.Primary, now); ok {
		report.Windows = append(report.Windows, window)
	}
	if window, ok := windowOf("secondary", payload.RateLimit.Secondary, now); ok {
		report.Windows = append(report.Windows, window)
	}
	report.CreditOverage = report.LimitReached &&
		payload.Credits != nil &&
		(payload.Credits.Unlimited || payload.Credits.HasCredits) &&
		!payload.Credits.OverageLimitReached &&
		(payload.SpendControl == nil || !payload.SpendControl.Reached)
	return report, nil
}

func windowID(duration time.Duration) string {
	if duration >= 24*time.Hour {
		return strconv.Itoa(int(math.Round(duration.Hours()/24))) + "d"
	}
	return strconv.Itoa(max(1, int(math.Round(duration.Hours())))) + "h"
}

func windowOf(name string, body *windowBody, now time.Time) (quota.Window, bool) {
	if body == nil {
		return quota.Window{}, false
	}
	window := quota.Window{ID: name}
	if body.LimitWindowSeconds != nil {
		window.Duration = time.Duration(*body.LimitWindowSeconds) * time.Second
		window.ID = windowID(window.Duration)
	}
	if body.UsedPercent != nil {
		window.Used = quota.Used{Fraction: *body.UsedPercent / quota.PercentFull, Reported: true}
	}
	switch {
	case body.ResetAt != nil:
		window.ResetsAt = quota.EpochTime(*body.ResetAt)
	case body.ResetAfterSeconds != nil:
		window.ResetsAt = now.Add(time.Duration(*body.ResetAfterSeconds) * time.Second).UTC()
	}
	if !window.Used.Reported && window.ResetsAt.IsZero() && window.Duration == 0 {
		return quota.Window{}, false
	}
	return window, true
}

func fromHeaders(header http.Header, now time.Time) quota.Report {
	report := quota.Report{Provider: quota.CodexSub, FetchedAt: now, Source: quota.SourceHeaders}
	for _, name := range []string{"primary", "secondary"} {
		prefix := headerPrefix + name + "-"
		body := windowBody{UsedPercent: quota.HeaderNumber(header, prefix+"used-percent"), ResetAt: quota.HeaderNumber(header, prefix+"reset-at")}
		if body.UsedPercent == nil {
			continue
		}
		if minutes := quota.HeaderNumber(header, prefix+"window-minutes"); minutes != nil {
			seconds := *minutes * time.Minute.Seconds()
			body.LimitWindowSeconds = &seconds
		}
		if window, ok := windowOf(name, &body, now); ok {
			report.Windows = append(report.Windows, window)
		}
	}
	return report
}
