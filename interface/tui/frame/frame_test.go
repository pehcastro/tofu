package frame

import (
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/widget"
)

func head() Head {
	return Head{
		Path:        "silo",
		Branch:      "develop",
		Provider:    "codex",
		Model:       "gpt-5.6-sol-2026-09-01",
		SessionName: "amber-cedar-otter",
		SessionID:   "turn-1a2b3c4d5e6f",
		At:          time.Date(2026, 9, 19, 14, 34, 0, 0, time.Local),
		Started:     time.Date(2026, 9, 19, 14, 31, 42, 0, time.Local),
	}
}

func TestTheHeaderReadsPathBranchModelSessionThenClockInOrder(t *testing.T) {
	text := headerText(head(), 200)
	order := []string{"./silo", "develop", "codex/gpt-5.6-sol-2026-09-01", "amber-cedar-otter #4d5e6f", "2m 18s"}
	last := -1
	for _, want := range order {
		at := strings.Index(text, want)
		if at < 0 {
			t.Fatalf("the header lost %q\n%s", want, text)
		}
		if at < last {
			t.Fatalf("%q reads out of order\n%s", want, text)
		}
		last = at
	}
}

func TestTheHeaderCarriesNoVersionAndNoArrow(t *testing.T) {
	text := headerText(head(), 200)
	if strings.Contains(text, "tofu "+konst.Version) {
		t.Errorf("the header still carries a version string\n%s", text)
	}
	if strings.Contains(text, " → ") {
		t.Errorf("the header still draws an arrow between provider and model\n%s", text)
	}
	if !strings.Contains(text, "codex/gpt-5.6-sol-2026-09-01") {
		t.Errorf("the header does not draw provider slash model\n%s", text)
	}
}

func TestADirectoryWithNoGitRepositoryDrawsNoBranchAndDoesNotBreak(t *testing.T) {
	bare := head()
	bare.Branch = ""
	text := headerText(bare, 80)
	if strings.Contains(text, "  ·  ·") {
		t.Errorf("an empty branch left a doubled separator\n%s", text)
	}
	if !strings.Contains(text, "./silo") {
		t.Errorf("the path is missing when there is no branch\n%s", text)
	}
}

func TestTheHeaderGivesUpTheClockBeforeTheSessionBeforeTheModelBeforeTheBranch(t *testing.T) {
	for _, step := range []struct {
		width int
		want  []string
		gone  []string
	}{
		{200, []string{"./silo", "develop", "codex/gpt-5.6-sol-2026-09-01", "amber-cedar-otter", "2m 18s"}, nil},
		{85, []string{"./silo", "develop", "codex/gpt-5.6-sol-2026-09-01", "amber-cedar-otter"}, []string{"2m 18s"}},
		{60, []string{"./silo", "develop", "codex/gpt-5.6-sol-2026-09-01"}, []string{"amber-cedar-otter"}},
		{30, []string{"./silo", "develop"}, []string{"codex/gpt-5.6-sol-2026-09-01"}},
		{10, []string{"./silo"}, []string{"develop"}},
	} {
		text := headerText(head(), step.width)
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

func carried() Status {
	return Status{
		Context:   Context{Used: 118000, Budget: konst.ContextCeilingTokens},
		TokensIn:  284000,
		TokensOut: 61000,
		Decisions: 1204,
		At:        time.Date(2026, 9, 19, 14, 32, 0, 0, time.Local),
		Quotas: []Quota{
			{
				Label:    "codex 7d",
				Fraction: 0.62,
				Reported: true,
				ResetsAt: time.Date(2026, 9, 19, 18, 0, 0, 0, time.Local),
			},
			{
				Label:    "claude weekly",
				Fraction: 0.31,
				Reported: true,
				ResetsAt: time.Date(2026, 9, 20, 20, 32, 0, 0, time.Local),
			},
		},
	}
}

const (
	meterFill   = "▓"
	meterEmpty  = "░"
	oldForkMark = "│"
)

func barText(status Status, width int) string {
	row1, row2 := barLines(status, width)
	return row1 + "\n" + row2
}

func TestTheContextMeterDrawsNoForkMarkAtAnyFill(t *testing.T) {
	status := carried()
	for tenth := range 10 {
		status.Context.Used = status.Context.Budget * tenth / 10
		text := contextText(status.Context, rowOneFull)
		if strings.Contains(text, oldForkMark) {
			t.Errorf("the meter at %d0%% draws a fork mark: %q", tenth, text)
		}
		if bar := strings.Count(text, meterFill) + strings.Count(text, meterEmpty); bar != konst.MeterBarWidthChars {
			t.Errorf("the meter at %d0%% is %d cells of bar, want %d: %q",
				tenth, bar, konst.MeterBarWidthChars, text)
		}
	}
}

func TestReadWrittenAndCachedAreThreeSeparateNumbers(t *testing.T) {
	fresh := carried()
	text := barText(fresh, 120)
	if !strings.Contains(text, "284k read") || !strings.Contains(text, "61k write") {
		t.Fatalf("a turn with no cache hit does not show the two plain numbers\n%s", text)
	}
	if strings.Contains(text, "cached") {
		t.Fatalf("a turn with no cache hit still names a cached number\n%s", text)
	}
	cached := carried()
	cached.CacheRead = 92000
	cachedText := barText(cached, 120)
	for _, want := range []string{"284k read", "61k write", "92k cached"} {
		if !strings.Contains(cachedText, want) {
			t.Errorf("a turn with a cache hit lost %q\n%s", want, cachedText)
		}
	}
}

func TestTheBarLeadsWithTheContextMeterAndCarriesBothQuotaWindows(t *testing.T) {
	text := barText(carried(), 200)
	if !strings.HasPrefix(text, "118k/250k ") {
		t.Fatalf("the bar does not lead with the context meter\n%s", text)
	}
	for _, want := range []string{
		"codex 7d", "resets in 3h 28m",
		"claude weekly", "resets in 1d 6h",
		"284k read", "61k write", "jev 1204",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the wide bar lost %q\n%s", want, text)
		}
	}
}

func TestASingleQuotaWindowDrawsAloneWithNoGap(t *testing.T) {
	status := carried()
	status.Quotas = status.Quotas[:1]
	row1, _ := barLines(status, 200)
	if !strings.Contains(row1, "codex 7d") {
		t.Fatalf("the single window is missing\n%s", row1)
	}
	if strings.Contains(row1, "claude weekly") {
		t.Fatalf("a trimmed slice still carries the second window\n%s", row1)
	}
	if strings.Contains(row1, separator+separator) || strings.HasSuffix(row1, separator) {
		t.Fatalf("one window leaves a gap where the second would join\n%q", row1)
	}
}

func TestAThirdQuotaSourceDrawsAsDataWithNoCodeChange(t *testing.T) {
	status := carried()
	status.Quotas = append(status.Quotas, Quota{
		Label:    "own-key daily",
		Fraction: 0.1,
		Reported: true,
		ResetsAt: status.At.Add(9 * time.Hour),
	})
	row1, _ := barLines(status, 400)
	for _, want := range []string{"codex 7d", "claude weekly", "own-key daily"} {
		if !strings.Contains(row1, want) {
			t.Errorf("a third source added to the slice did not draw: %q missing\n%s", want, row1)
		}
	}
}

func TestTheBarCarriesTheReleaseFarRight(t *testing.T) {
	status := carried()
	status.Release = konst.Version
	text := barText(status, 200)
	if !strings.HasSuffix(text, "tofu "+konst.Version) {
		t.Fatalf("the release is not the last field on the bar\n%s", text)
	}
}

func TestAZeroContextBudgetNamesTheAbsence(t *testing.T) {
	status := carried()
	status.Context = Context{}
	row1, _ := barLines(status, 120)
	field, _, _ := strings.Cut(row1, separator)
	if field != "context unread" {
		t.Fatalf("a zero budget does not name the absence, it reads %q\n%s", field, row1)
	}
	for _, narrow := range []int{80, 40, 16} {
		row1, _ := barLines(status, narrow)
		field, _, _ := strings.Cut(row1, separator)
		if field != "context unread" {
			t.Errorf("at width %d a zero budget reads %q", narrow, field)
		}
	}
}

func TestTheNarrowBarDropsTheQuotaMeterBeforeItDropsTheResetAndDropsTheResetBeforeTheContextMeter(t *testing.T) {
	for _, step := range []struct {
		width int
		want  []string
		gone  []string
	}{
		{150, []string{"118k/250k", meterFill, "codex 7d", meterFill, "62%", "resets in", "claude weekly", "31%"}, nil},
		{120, []string{"118k/250k", meterFill, "codex 7d", "62%", "resets in", "claude weekly", "31%"}, []string{"codex 7d " + meterFill}},
		{80, []string{"118k/250k", meterFill, "codex 7d", "62%", "claude weekly", "31%"}, []string{"resets in"}},
		{55, []string{"118k/250k", meterFill, "codex 7d", "claude weekly"}, []string{"62%", "31%", "resets in"}},
		{40, []string{"118k/250k", "codex 7d", "claude weekly"}, []string{meterFill}},
		{20, []string{"118k/250k"}, []string{"codex 7d", "claude weekly"}},
	} {
		row1, _ := barLines(carried(), step.width)
		if widget.Cells(row1) > step.width {
			t.Errorf("row1 is %d cells wide at width %d\n%s", widget.Cells(row1), step.width, row1)
		}
		for _, want := range step.want {
			if !strings.Contains(row1, want) {
				t.Errorf("at width %d row1 lost %q\n%s", step.width, want, row1)
			}
		}
		for _, gone := range step.gone {
			if strings.Contains(row1, gone) {
				t.Errorf("at width %d row1 still carries %q\n%s", step.width, gone, row1)
			}
		}
	}
}

func TestTheRowOneDropOrderNamesTheResetLastBeforeTheContextMeter(t *testing.T) {
	order := RowOneDropOrder()
	want := []string{"quota bar", "quota reset", "quota label", "context bar"}
	if len(order) != len(want) {
		t.Fatalf("the drop order has %d steps, want %d: %v", len(order), len(want), order)
	}
	for index, step := range want {
		if order[index] != step {
			t.Errorf("step %d is %q, want %q: %v", index, order[index], step, order)
		}
	}
}

func TestTheNoteDropsBeforeTheJevCountOnTheSecondRow(t *testing.T) {
	status := carried()
	status.Note = "gate off: no tool call is judged"
	if _, row2 := barLines(status, 200); !strings.Contains(row2, status.Note) {
		t.Errorf("a wide row2 drops the note\n%s", row2)
	}
	_, row2 := barLines(status, 60)
	if strings.Contains(row2, status.Note) {
		t.Errorf("a narrow row2 keeps the note ahead of the jev count\n%s", row2)
	}
	if !strings.Contains(row2, "jev 1204") {
		t.Errorf("the note pushed the jev count off row2\n%s", row2)
	}
}

func TestEveryDurationOnTheBarComesFromWidgetUntil(t *testing.T) {
	status := carried()
	status.At = time.Date(2026, 9, 19, 12, 0, 0, 0, time.Local)
	status.Quotas[0].ResetsAt = status.At.Add(26 * time.Hour)
	row1, _ := barLines(status, 400)
	want := "resets in " + widget.Until(26*time.Hour)
	if !strings.Contains(row1, want) {
		t.Fatalf("the reset clause does not read Until's own formatting\n%s\nwant %q", row1, want)
	}
	if strings.Contains(row1, "26h") {
		t.Fatalf("a duration over a day is printed as raw hours instead of Until's day form\n%s", row1)
	}
}

func TestTheBarAt80And100And120And150ColumnsMatchesTheGoldenFrame(t *testing.T) {
	tail := "\n284k read  61k write  ·  jev 1204  ·  tofu " + konst.Version
	golden := map[int]string{
		80:  "118k/250k ▓▓▓▓▓▓░░░░░░  ·  codex 7d  62%  ·  claude weekly  31%" + tail,
		100: "118k/250k ▓▓▓▓▓▓░░░░░░  ·  codex 7d  62%  resets in 3h 28m  ·  claude weekly  31%  resets in 1d 6h" + tail,
		120: "118k/250k ▓▓▓▓▓▓░░░░░░  ·  codex 7d  62%  resets in 3h 28m  ·  claude weekly  31%  resets in 1d 6h" + tail,
		150: "118k/250k ▓▓▓▓▓▓░░░░░░  ·  codex 7d ▓▓▓▓▓▓▓░░░░░  62%  resets in 3h 28m  " +
			"·  claude weekly ▓▓▓▓░░░░░░░░  31%  resets in 1d 6h" + tail,
	}
	status := carried()
	status.Release = konst.Version
	for _, width := range []int{80, 100, 120, 150} {
		if text := barText(status, width); text != golden[width] {
			t.Fatalf("the %d column bar does not match the golden frame\ngot:\n%s\nwant:\n%s", width, text, golden[width])
		}
	}
}

func TestTheStripCarriesTheNoticeWithoutMovingItsViews(t *testing.T) {
	strip := Strip{Views: []View{{Digit: '1', Name: "chat"}, {Digit: '5', Name: "shells"}}}
	quiet := strip.Render(80)
	strip.Notice = "forking the session in the background"
	busy := strip.Render(80)
	if !strings.Contains(busy, strip.Notice) {
		t.Fatalf("the strip does not carry the notice\n%s", busy)
	}
	if strings.Contains(quiet, strip.Notice) {
		t.Fatalf("the quiet strip already carries the notice\n%s", quiet)
	}
	before, _, _ := strings.Cut(quiet, "shells")
	if busyHead, _, _ := strings.Cut(busy, "shells"); busyHead != before {
		t.Errorf("the notice moved the view names\n%q\n%q", busyHead, before)
	}
}
