package quota

import (
	"encoding/json"
	"net/http"
	"time"

	"boji/internal/transport"
)

func FromRejection(provider Provider, status int, header http.Header, body []byte, now time.Time) Report {
	report := FromHeaders(provider, header, now)
	report.Source = SourceRejection
	code, plan, resets := parseRejection(body)
	report.Plan = plan
	if !isQuotaRejection(status, code) {
		return report
	}
	report.LimitReached = true
	if resets.IsZero() {
		if wait, ok := retryAfter(header); ok {
			resets = now.Add(wait).UTC()
		}
	}
	if resets.IsZero() {
		return report
	}
	for _, window := range report.Windows {
		if window.State() == StateExhausted && !window.ResetsAt.IsZero() {
			return report
		}
	}
	report.Windows = append(report.Windows,
		Window{ID: "rejected", Used: Used{Fraction: 1, Reported: true}, ResetsAt: resets})
	return report
}

func parseRejection(body []byte) (code, plan string, resets time.Time) {
	var payload struct {
		Error struct {
			Code     string   `json:"code"`
			Type     string   `json:"type"`
			PlanType string   `json:"plan_type"`
			ResetsAt *float64 `json:"resets_at"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", time.Time{}
	}
	code = payload.Error.Code
	if code == "" {
		code = payload.Error.Type
	}
	if payload.Error.ResetsAt != nil {
		resets = epochTime(*payload.Error.ResetsAt)
	}
	return code, payload.Error.PlanType, resets
}

func isQuotaRejection(status int, code string) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	switch code {
	case "usage_limit_reached", "usage_not_included", "rate_limit_error", "rate_limit_exceeded":
		return true
	}
	return false
}

func retryAfter(header http.Header) (time.Duration, bool) {
	if millis, ok := headerNumber(header, transport.RetryAfterMillis); ok && millis >= 0 {
		return time.Duration(millis) * time.Millisecond, true
	}
	if seconds, ok := headerNumber(header, transport.RetryAfter); ok && seconds >= 0 {
		return time.Duration(seconds * float64(time.Second)), true
	}
	return 0, false
}
