package frame

import (
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/widget"
)

func head(release string) Head {
	return Head{
		Release: release,
		Repo:    "silo",
		Branch:  "develop",
		Wire:    "codex",
		Model:   "gpt-5.6-sol-2026-09-01",
		At:      time.Date(2026, 9, 19, 14, 34, 0, 0, time.Local),
		Elapsed: 138 * time.Second,
	}
}

func TestTheHeaderCarriesTheVersionNextToTheName(t *testing.T) {
	for _, width := range []int{80, 120} {
		text := headerText(head(konst.Version), width)
		if !strings.HasPrefix(text, "tofu "+konst.Version+separator) {
			t.Errorf("at width %d the header does not lead with the name and the version\n%s", width, text)
		}
		for _, want := range []string{"silo", "develop", "codex → gpt-5.6-sol-2026-09-01"} {
			if !strings.Contains(text, want) {
				t.Errorf("at width %d the header lost %q\n%s", width, want, text)
			}
		}
	}
}

func TestTheHeaderGivesUpTheElapsedTimeBeforeAnythingThatSaysWhatIsRunning(t *testing.T) {
	develRelease := konst.Version + develSuffix
	for _, step := range []struct {
		width int
		want  []string
		gone  []string
	}{
		{120, []string{develRelease, "silo", "develop", "codex → gpt-5.6-sol-2026-09-01", "14:34 02:18"}, nil},
		{80, []string{develRelease, "silo", "develop", "codex → gpt-5.6-sol-2026-09-01", "14:34"}, []string{"02:18"}},
		{70, []string{develRelease, "silo", "develop", "codex → gpt-5.6-sol-2026-09-01"}, []string{"14:34"}},
		{50, []string{develRelease, "silo", "develop", "codex"}, []string{"gpt-5.6-sol"}},
		{40, []string{develRelease, "develop", "codex"}, []string{"silo"}},
		{30, []string{develRelease, "codex"}, []string{"develop"}},
		{20, []string{develRelease}, []string{"codex"}},
	} {
		text := headerText(head(develRelease), step.width)
		if widget.Cells(text) > step.width {
			t.Errorf("the header is %d cells wide at width %d\n%s", widget.Cells(text), step.width, text)
		}
		for _, want := range step.want {
			if !strings.Contains(text, want) {
				t.Errorf("at width %d the header lost %q\n%s", step.width, want, text)
			}
		}
		for _, gone := range step.gone {
			if strings.Contains(text, gone) {
				t.Errorf("at width %d the header still carries %q\n%s", step.width, gone, text)
			}
		}
	}
}

func TestAnUnstampedOrDirtyBuildDoesNotClaimTheReleasedVersion(t *testing.T) {
	for _, build := range []struct {
		version  string
		revision string
		want     string
	}{
		{"v" + konst.Version, "9f2c1ab", konst.Version},
		{"dev", "9f2c1ab", konst.Version + develSuffix},
		{"v" + konst.Version, "9f2c1ab-dirty", konst.Version + develSuffix},
		{"dev", "unknown", konst.Version + develSuffix},
	} {
		if got := Release(build.version, build.revision); got != build.want {
			t.Errorf("Release(%q, %q) = %q, want %q", build.version, build.revision, got, build.want)
		}
	}
	stamped := headerText(head(Release("v"+konst.Version, "9f2c1ab")), 80)
	unstamped := headerText(head(Release("dev", "unknown")), 80)
	if stamped == unstamped {
		t.Fatalf("a stamped build and an unstamped one draw the same header\n%s", stamped)
	}
}

func carried() Status {
	return Status{
		Context:   Context{Used: 118000, Budget: 250000},
		TokensIn:  284000,
		TokensOut: 61000,
		Decisions: 1204,
		At:        time.Date(2026, 9, 19, 14, 32, 0, 0, time.Local),
		Quota: Quota{
			Label:    "codex 7d",
			Fraction: 0.62,
			Reported: true,
			ResetsAt: time.Date(2026, 9, 19, 18, 0, 0, 0, time.Local),
		},
	}
}

const (
	meterFill   = "▓"
	meterEmpty  = "░"
	oldForkMark = "│"
)

func TestTheContextMeterDrawsNoForkMarkAtAnyFill(t *testing.T) {
	status := carried()
	for tenth := range 10 {
		status.Context.Used = status.Context.Budget * tenth / 10
		text := contextText(status.Context, nothing)
		if strings.Contains(text, oldForkMark) {
			t.Errorf("the meter at %d0%% draws a fork mark: %q", tenth, text)
		}
		if bar := strings.Count(text, meterFill) + strings.Count(text, meterEmpty); bar != konst.MeterBarWidthChars {
			t.Errorf("the meter at %d0%% is %d cells of bar, want %d: %q",
				tenth, bar, konst.MeterBarWidthChars, text)
		}
	}
}

func TestTheBarLeadsWithTheContextMeter(t *testing.T) {
	text := barText(carried(), 120)
	if !strings.HasPrefix(text, "118k/250k ") {
		t.Fatalf("the bar does not lead with the context meter\n%s", text)
	}
	for _, want := range []string{"codex 7d", "⇅ 284k/61k", "jev 1204", "resets in 3h 28m"} {
		if !strings.Contains(text, want) {
			t.Errorf("the wide bar lost %q\n%s", want, text)
		}
	}
}

func TestAZeroContextBudgetNamesTheAbsence(t *testing.T) {
	status := carried()
	status.Context = Context{}
	text := barText(status, 120)
	field, _, _ := strings.Cut(text, separator)
	if field != "context unread" {
		t.Fatalf("a zero budget does not name the absence, it reads %q\n%s", field, text)
	}
	for _, narrow := range []int{80, 40, 16} {
		field, _, _ := strings.Cut(barText(status, narrow), separator)
		if field != "context unread" {
			t.Errorf("at width %d a zero budget reads %q", narrow, field)
		}
	}
}

func TestTheNarrowBarDropsDetailBeforeItDropsTheContextMeter(t *testing.T) {
	for _, step := range []struct {
		width int
		want  []string
		gone  []string
	}{
		{104, []string{"118k/250k", meterFill, "codex 7d", "62%", "resets in 3h 28m", "⇅ 284k/61k", "jev 1204"}, nil},
		{96, []string{"118k/250k", meterFill, "codex 7d", "62%", "resets in 3h 28m", "⇅ 284k/61k"}, []string{"jev "}},
		{70, []string{"118k/250k", meterFill, "codex 7d", "62%", "⇅ "}, []string{"resets"}},
		{60, []string{"118k/250k", meterFill, "codex 7d", "62%"}, []string{"⇅ "}},
		{40, []string{"118k/250k", meterFill, "codex 7d"}, []string{"62%"}},
		{16, []string{"118k/250k"}, []string{"codex 7d", meterFill}},
	} {
		text := barText(carried(), step.width)
		if widget.Cells(text) > step.width {
			t.Errorf("the bar is %d cells wide at width %d\n%s", widget.Cells(text), step.width, text)
		}
		for _, want := range step.want {
			if !strings.Contains(text, want) {
				t.Errorf("at width %d the bar lost %q\n%s", step.width, want, text)
			}
		}
		for _, gone := range step.gone {
			if strings.Contains(text, gone) {
				t.Errorf("at width %d the bar still carries %q\n%s", step.width, gone, text)
			}
		}
	}
}

func TestTheNoteIsTheFirstThingTheBarGivesUp(t *testing.T) {
	status := carried()
	status.Note = "gate off: no tool call is judged"
	if text := barText(status, 200); !strings.Contains(text, status.Note) {
		t.Errorf("a wide bar drops the note\n%s", text)
	}
	text := barText(status, 80)
	if strings.Contains(text, status.Note) {
		t.Errorf("an 80 column bar keeps the note ahead of the numbers\n%s", text)
	}
	if !strings.Contains(text, "62%") {
		t.Errorf("the note pushed the quota off an 80 column bar\n%s", text)
	}
}

func TestTheStripCarriesTheNoticeWithoutMovingItsViews(t *testing.T) {
	strip := Strip{Views: []View{{Digit: '1', Name: "session"}, {Digit: '6', Name: "settings"}}}
	quiet := strip.Render(80)
	strip.Notice = "forking the session in the background"
	busy := strip.Render(80)
	if !strings.Contains(busy, strip.Notice) {
		t.Fatalf("the strip does not carry the notice\n%s", busy)
	}
	if strings.Contains(quiet, strip.Notice) {
		t.Fatalf("the quiet strip already carries the notice\n%s", quiet)
	}
	head, _, _ := strings.Cut(quiet, "settings")
	if busyHead, _, _ := strings.Cut(busy, "settings"); busyHead != head {
		t.Errorf("the notice moved the view names\n%q\n%q", busyHead, head)
	}
}
