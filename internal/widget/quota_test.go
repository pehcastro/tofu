package widget

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var rawHours = regexp.MustCompile(`\b([2-9][4-9]|[3-9][0-9]|[0-9]{3,})h`)

func TestAResetReadsAsADurationAPersonPlansAround(t *testing.T) {
	for _, step := range []struct {
		window string
		left   time.Duration
		want   string
	}{
		{"spent", 0, "now"},
		{"5h", 40 * time.Minute, "in 40m"},
		{"5h", 2*time.Hour + 6*time.Minute, "in 2h 6m"},
		{"1d", 3 * time.Hour, "in 3h"},
		{"7d", 75*time.Hour + 26*time.Minute, "in 3d 3h"},
		{"7d", 160*time.Hour + 15*time.Minute, "in 6d 16h"},
		{"mo", 24 * time.Hour, "in 1d"},
	} {
		got := Until(step.left)
		if got != step.want {
			t.Errorf("the %s window resets %q, want %q", step.window, got, step.want)
		}
		if rawHours.MatchString(got) {
			t.Errorf("the %s window prints a raw hour count over 24: %q", step.window, got)
		}
	}
}

func TestTheQuotaMeterHoldsItsColumnsAtEveryFill(t *testing.T) {
	now := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	width := 0
	for tenth := range 11 {
		text := Quota(float64(tenth)/10, now.Add(time.Hour), now)
		if !strings.HasSuffix(text, "  resets in 1h") {
			t.Fatalf("the meter at %d0%% lost its reset clause: %q", tenth, text)
		}
		meter, _, _ := strings.Cut(text, "  resets")
		if width == 0 {
			width = Cells(meter)
		}
		if Cells(meter) != width {
			t.Errorf("the meter at %d0%% is %d cells, the meter at 0%% is %d: %q",
				tenth, Cells(meter), width, meter)
		}
	}
}

func TestAQuotaWithNoKnownResetPrintsNoResetClause(t *testing.T) {
	text := Quota(0.62, time.Time{}, time.Now())
	if strings.Contains(text, "resets") {
		t.Fatalf("an unknown reset still prints a clause: %q", text)
	}
}
