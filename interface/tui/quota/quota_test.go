package quota

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/frame"
	"tofu/internal/golden"
)

func TestQuotaGoldens(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	quotas := []frame.Quota{
		{Label: "claude-sub 5h", Fraction: 0.62, Reported: true, ResetsAt: now.Add(3*time.Hour + 28*time.Minute)},
		{Label: "codex-sub weekly", Fraction: 0.07, Reported: true, ResetsAt: now.Add(6*24*time.Hour + 23*time.Hour)},
		{Label: "claude-sub weekly", Fraction: 0.93, Reported: true, ResetsAt: now.Add(2*24*time.Hour + 14*time.Hour)},
		{Label: "codex-sub 5h"},
	}
	session := func(width, height int) string {
		line := strings.Repeat("the session behind the dialog ", 5)[:width]
		return strings.TrimSuffix(strings.Repeat(line+"\n", height), "\n")
	}
	full := ansi.Strip(Dialog(session(120, 36), 120, 36, quotas, now))
	for _, want := range []string{"claude-sub", "codex-sub", "62% · resets in 3h 28m", "93% · resets in 2d 14h", "not reported"} {
		if !strings.Contains(full, want) {
			t.Errorf("the dialog does not say %q", want)
		}
	}
	golden.Assert(t, "quota-120x36.golden", full)
	golden.Assert(t, "quota-70x20.golden", ansi.Strip(Dialog(session(70, 20), 70, 20, quotas, now)))
}
