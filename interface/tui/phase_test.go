package tui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/session"
)

func phaseApp(t *testing.T, at *time.Time) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    func() time.Time { return *at },
		Wires:  anthropicAlone,
		Turn:   func(context.Context, Pick, string, CalledFromInsideTheTurnAndNeverAfterItReturns) {},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeText(app, "how many go files are under each root?")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return app
}

func styledTurnRow(t *testing.T, app *App) string {
	t.Helper()
	found := ""
	for _, row := range strings.Split(app.View().Content, "\n") {
		if strings.ContainsAny(ansi.Strip(row), spinnerFrames) {
			found = row
		}
	}
	if found == "" {
		t.Fatalf("no running row in the frame\n%s", app.View().Content)
	}
	return found
}

func colours(row string) []string {
	var found []string
	for rest := row; ; {
		open := strings.Index(rest, "\x1b[")
		if open < 0 {
			return found
		}
		shut := strings.IndexByte(rest[open:], 'm')
		if shut < 0 {
			return found
		}
		found = append(found, rest[open:open+shut+1])
		rest = rest[open+shut+1:]
	}
}

func TestAGapBetweenTwoCallsOfOneStepNeverDrawsThinkingAndNeverChangesColour(t *testing.T) {
	at := fixedStart()
	app := phaseApp(t, &at)
	app.Update(Event{Kind: EventStats, Model: "claude-opus-5"})
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "read", Text: "internal/turn/loop.go"})
	at = at.Add(2 * time.Second)

	frames := []string{styledTurnRow(t, app)}
	tick := func(milliseconds int) {
		for range milliseconds {
			at = at.Add(time.Millisecond)
			frames = append(frames, styledTurnRow(t, app))
		}
	}
	tick(4)
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "84 lines, 2.1 KB"})
	tick(8)
	app.Update(Event{Kind: EventToolCall, ID: "c2", Tool: "bash", Text: "go test ./internal/..."})
	tick(8)

	for index, frame := range frames {
		if strings.Contains(ansi.Strip(frame), "thinking") {
			t.Fatalf("frame %d of %d reads as thinking inside one step: %q", index, len(frames), ansi.Strip(frame))
		}
		if index > 0 && !slices.Equal(colours(frame), colours(frames[index-1])) {
			t.Fatalf("frame %d changes colour from %v to %v\n%q\n%q",
				index, colours(frames[index-1]), colours(frame), frames[index-1], frame)
		}
	}
	if painted := colours(frames[0]); len(painted) != 2 {
		t.Fatalf("the row is painted with %v, want one colour opened and closed", painted)
	}
}

func TestACallShorterThanTheDwellNeverTakesTheRowAndIsStillCounted(t *testing.T) {
	at := fixedStart()
	app := phaseApp(t, &at)
	app.Update(Event{Kind: EventStats, Model: "claude-opus-5"})
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "read", Text: "internal/turn/loop.go"})
	at = at.Add(2 * time.Second)
	if row := turnRow(t, app); !strings.Contains(row, "loop.go") {
		t.Fatalf("the first call never took the row: %q", row)
	}

	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "84 lines, 2.1 KB"})
	app.Update(Event{Kind: EventToolCall, ID: "c2", Tool: "read", Text: "CLAUDE.md"})
	at = at.Add(20 * time.Millisecond)
	row := turnRow(t, app)
	app.Update(Event{Kind: EventToolResult, ID: "c2", Text: "42 lines, 1.1 KB"})

	if strings.Contains(row, "CLAUDE.md") {
		t.Errorf("a 20 ms call took the row: %q", row)
	}
	finishTurn(app)
	content := ansi.Strip(app.View().Content)
	if !strings.Contains(content, "· (2) tools") {
		t.Errorf("the ended turn does not count the short call\n%s", content)
	}
}

func TestOnlyThePhaseWordChangesWhenTheFirstTokenArrives(t *testing.T) {
	at := fixedStart()
	app := phaseApp(t, &at)
	at = at.Add(40 * time.Second)
	before := turnRow(t, app)
	if !strings.Contains(before, "requesting") {
		t.Fatalf("the row does not read as requesting before the first token: %q", before)
	}
	app.Update(Event{Kind: EventStats, Model: "claude-opus-5"})
	if elapsed := turnElapsed(t, app); elapsed != "40s" {
		t.Errorf("the first token reset the clock to %q, want 40s", elapsed)
	}
	if after := turnRow(t, app); strings.Contains(after, "requesting") {
		t.Errorf("the phase word did not change at the first token: %q", after)
	}
}

func TestTheClockCountsTheWholeTurnAndOnlyTheWordChanges(t *testing.T) {
	at := fixedStart()
	app := phaseApp(t, &at)
	at = at.Add(37 * time.Second)
	waiting := turnRow(t, app)
	if !strings.Contains(waiting, "requesting") || !strings.Contains(waiting, "37s") {
		t.Fatalf("the row does not read as requesting for 37s: %q", waiting)
	}
	app.Update(Event{Kind: EventStats, Model: "claude-opus-5"})
	at = at.Add(13 * time.Second)
	working := turnRow(t, app)
	if !strings.Contains(working, "50s") {
		t.Errorf("the clock restarted on the phase change instead of reading 50s: %q", working)
	}
}

func workedAfterWaiting(t *testing.T, wait, work time.Duration) string {
	t.Helper()
	at := fixedStart()
	app := phaseApp(t, &at)
	at = at.Add(wait)
	app.Update(Event{Kind: EventStats, Model: "claude-opus-5"})
	at = at.Add(work)
	app.Update(Event{Kind: EventDone, Text: "finished in"})
	content := ansi.Strip(app.View().Content)
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "finished in") {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("no closing line in the frame\n%s", content)
	return ""
}

func TestTheClosingLineIsTheWorkAndCarriesNoQuota(t *testing.T) {
	line := workedAfterWaiting(t, 5*time.Second, 13*time.Second)
	if !strings.HasPrefix(line, "· finished in 13s") {
		t.Errorf("the closing line reads %q, want finished in 13s", line)
	}
	if strings.Contains(line, "quota") || strings.Contains(line, "ms") {
		t.Errorf("the closing line still carries the quota or the milliseconds: %q", line)
	}
}

func TestTheClosingLineCarriesTheWaitOnlyWhenItWasLongerThanTheWork(t *testing.T) {
	longer := workedAfterWaiting(t, 37*time.Second, 13*time.Second)
	if !strings.Contains(longer, "waited 37s") {
		t.Errorf("a 37s wait behind 13s of work is not reported: %q", longer)
	}
	shorter := workedAfterWaiting(t, 5*time.Second, 13*time.Second)
	if strings.Contains(shorter, "waited") {
		t.Errorf("a 5s wait behind 13s of work is reported anyway: %q", shorter)
	}
}

func TestAStoppedTurnAndACappedOneSayWhatEndedThem(t *testing.T) {
	for _, words := range []string{"cooked for", "stopped at the step cap after"} {
		at := fixedStart()
		app := phaseApp(t, &at)
		app.Update(Event{Kind: EventStats, Model: "claude-opus-5"})
		at = at.Add(13 * time.Second)
		app.Update(Event{Kind: EventDone, Text: words})
		content := ansi.Strip(app.View().Content)
		if !strings.Contains(content, words+" 13s") {
			t.Errorf("the closing line does not read %q\n%s", words+" 13s", content)
		}
	}
}

func rowOf(t *testing.T, rows []string, carrying string) int {
	t.Helper()
	at := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, carrying) })
	if at < 0 {
		t.Fatalf("no row carries %q\n%s", carrying, strings.Join(rows, "\n"))
	}
	return at
}

func TestTheRunningRowHoldsItsPlaceWhenAStepEndsAndNoStepIsSummarised(t *testing.T) {
	at := fixedStart()
	app := phaseApp(t, &at)
	app.Update(Event{Kind: EventStats, Model: "claude-opus-5"})
	app.Update(Event{Kind: EventText, Text: "counting the go files under each root."})
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "read", Text: "internal/turn/loop.go"})
	at = at.Add(2 * time.Second)
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "84 lines, 2.1 KB"})
	app.Update(Event{Kind: EventToolCall, ID: "c2", Tool: "bash", Text: "go test ./internal/..."})
	at = at.Add(2 * time.Second)

	before := plainRows(app.View())
	running := rowOf(t, before, "working")

	app.Update(Event{Kind: EventToolResult, ID: "c2", Text: "ok 0.4s"})
	app.Update(Event{Kind: EventRequesting})
	at = at.Add(2 * time.Second)
	after := plainRows(app.View())
	if joined := strings.Join(before, "\n"); strings.Contains(joined, " tools") {
		t.Errorf("the turn summarises itself while a step runs\n%s", joined)
	}
	if joined := strings.Join(after, "\n"); strings.Contains(joined, " tools") {
		t.Errorf("the turn summarises itself once the step ends\n%s", joined)
	}
	if moved := rowOf(t, after, "thinking"); moved != running {
		t.Errorf("the running row moved from %d to %d when the step ended\n%s", running, moved, strings.Join(after, "\n"))
	}
}

func TestTheDwellIsNeverShorterThanAFrame(t *testing.T) {
	if session.PhaseDwell < session.TickInterval {
		t.Fatalf("the dwell is %v and the tick is %v, so a phase can be replaced before it is drawn twice",
			session.PhaseDwell, session.TickInterval)
	}
}
