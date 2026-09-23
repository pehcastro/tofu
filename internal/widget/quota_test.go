package widget

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var rawHours = regexp.MustCompile(`\b([2-9][4-9]|[3-9][0-9]|[0-9]{3,})h`)

func TestADurationRendersAtEveryScale(t *testing.T) {
	for _, step := range []struct {
		scale string
		d     time.Duration
		want  string
	}{
		{"seconds", 40 * time.Second, "40s"},
		{"minutes", 40 * time.Minute, "40m"},
		{"hours", 2*time.Hour + 6*time.Minute, "2h 6m"},
		{"days", 75*time.Hour + 26*time.Minute, "3d 3h"},
	} {
		got := Until(step.d)
		if got != step.want {
			t.Errorf("the %s case renders %q, want %q", step.scale, got, step.want)
		}
		if rawHours.MatchString(got) {
			t.Errorf("the %s case prints a raw hour count over 24: %q", step.scale, got)
		}
	}
}

func TestAZeroOrPastDurationFloorsAtZeroSeconds(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Hour} {
		if got := Until(d); got != "0s" {
			t.Errorf("Until(%s) = %q, want %q", d, got, "0s")
		}
	}
}

func TestTheSameDurationBacksTheSessionSubAgentAndTofuSessionViews(t *testing.T) {
	for _, step := range []struct {
		place string
		d     time.Duration
		want  string
	}{
		{"interface/tui/session turn clock", 41 * time.Second, "41s"},
		{"interface/tui/subagent fold row", 2*time.Hour + 14*time.Minute, "2h 14m"},
		{"cmd/tofu session list", 10 * time.Minute, "10m"},
	} {
		if got := Until(step.d); got != step.want {
			t.Errorf("%s reads Until(%s) as %q, want %q", step.place, step.d, got, step.want)
		}
	}
}

func TestAResetPrintsNowRatherThanZeroSeconds(t *testing.T) {
	now := time.Now()
	if got := Quota(0.4, now, now); !strings.HasSuffix(got, "resets now") {
		t.Errorf("a reset that has already passed reads %q, want it to end in %q", got, "resets now")
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
