package codexsub

import (
	"net/http"
	"testing"
	"time"
)

func TestCodexReplyHeadersReadPercentAndMinutes(t *testing.T) {
	header := http.Header{}
	header.Set("x-codex-primary-used-percent", "12.5")
	header.Set("x-codex-primary-window-minutes", "300")
	header.Set("x-codex-primary-reset-at", "1780490000")
	header.Set("x-codex-secondary-used-percent", "60")
	header.Set("x-codex-secondary-window-minutes", "10080")
	used := map[string]float64{}
	for _, window := range fromHeaders(header, time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)).Windows {
		used[window.ID] = window.Used.Fraction
	}
	if used["5h"] != 0.125 || used["7d"] != 0.6 {
		t.Fatalf("codex headers read %v", used)
	}
}
