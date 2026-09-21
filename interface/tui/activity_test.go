package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/crew"
	"tofu/interface/tui/theme"
)

const (
	spinnerFrames = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	activityWidth = 80
)

func spinningRows(rows []string) []int {
	var found []int
	for index, row := range rows {
		if strings.ContainsAny(row, spinnerFrames) {
			found = append(found, index)
		}
	}
	return found
}

const composerBlockRows = 5

func composerTint() string {
	escape, _, _ := strings.Cut(lipgloss.NewStyle().Background(theme.ComposerColor()).Render("X"), "X")
	return escape
}

func composerTopRow(t *testing.T, content string) int {
	t.Helper()
	for index, row := range strings.Split(content, "\n") {
		if strings.Contains(row, composerTint()) {
			return index
		}
	}
	t.Fatalf("no composer row in the frame:\n%s", content)
	return 0
}

func fixedStart() time.Time { return time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC) }

func TestTheRunningRowSitsBetweenTheTranscriptAndTheComposer(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	view := app.View()
	rows := plainRows(view)
	top := composerTopRow(t, view.Content)
	running := spinningRows(rows)
	if len(running) != 2 {
		t.Fatalf("the frame carries %d spinning rows, want two, the progress line and the footer\n%s", len(running), strings.Join(rows, "\n"))
	}
	if running[1] != top-1 {
		t.Errorf("the footer's running row is row %d and the composer begins at row %d, want the row just above it\n%s",
			running[1], top, strings.Join(rows, "\n"))
	}
	if fold := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, " tools") }); fold >= 0 {
		t.Errorf("row %d summarises a turn that has not ended\n%s", fold, strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[running[0]], "#") {
		t.Errorf("the transcript's running line carries no id\n%s", rows[running[0]])
	}
	assertGolden(t, "session-activity-80x24.golden", view.Content)
}

func TestWithNothingRunningThereIsNoActivityRow(t *testing.T) {
	app := sessionApp(t, 80, 24)
	rows := plainRows(app.View())
	if running := spinningRows(rows); len(running) != 0 {
		t.Errorf("an idle session draws %d spinning rows on %v\n%s", len(running), running, strings.Join(rows, "\n"))
	}
	assertGolden(t, "session-80x24.golden", app.View().Content)
}

func TestTheWholeRunningRowCarriesThePhasesOwnColour(t *testing.T) {
	at := fixedStart()
	line := styledTurnRow(t, liveApp(t, &at))
	toolOpen, _, _ := strings.Cut(theme.Tool().Render(""), "\x1b[m")
	if !strings.HasPrefix(line, toolOpen) {
		t.Errorf("a working row does not open with the tool colour %q\n%q", toolOpen, line)
	}
	if painted := colours(line); len(painted) != 2 {
		t.Errorf("the row is painted with %v, want the phase colour opened once and closed once\n%q", painted, line)
	}
	elapsed := strings.Fields(ansi.Strip(line))
	if len(elapsed) < 2 || !strings.HasSuffix(elapsed[1], "s") {
		t.Fatalf("the running row carries no elapsed time: %q", ansi.Strip(line))
	}
}

func belowTheComposer(t *testing.T, view tea.View) []string {
	t.Helper()
	return plainRows(view)[composerTopRow(t, view.Content)+composerBlockRows:]
}

func TestNothingUnderTheComposerSaysWhatIsRunning(t *testing.T) {
	at := fixedStart()
	for _, row := range belowTheComposer(t, liveApp(t, &at).View()) {
		if strings.ContainsAny(row, spinnerFrames) {
			t.Errorf("a row under the composer spins: %q", row)
		}
		if strings.Contains(row, longIntent) {
			t.Errorf("a row under the composer says what is running: %q", row)
		}
	}
}

func TestCtrlCAppearsOnceWhileATurnRuns(t *testing.T) {
	at := fixedStart()
	content := ansi.Strip(liveApp(t, &at).View().Content)
	if said := strings.Count(content, "ctrl+c"); said != 1 {
		t.Errorf("ctrl+c appears %d times, want once\n%s", said, content)
	}
}

func TestScrollingTheTranscriptDoesNotMoveTheRunningRow(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	for step := range 40 {
		app.Update(Event{Kind: EventNote, Text: "note " + strings.Repeat("x", step%7)})
	}
	before := spinningRows(plainRows(app.View()))
	app.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	view := app.View()
	rows := plainRows(view)
	if !strings.Contains(ansi.Strip(view.Content), scrolledWords) {
		t.Fatalf("pgup did not scroll the transcript back\n%s", strings.Join(rows, "\n"))
	}
	after := spinningRows(rows)
	if len(before) != 1 || len(after) != 1 || before[0] != after[0] {
		t.Errorf("the running row moved from %v to %v when the transcript scrolled\n%s",
			before, after, strings.Join(rows, "\n"))
	}
	assertGolden(t, "session-activity-scrolled-80x24.golden", view.Content)
}

func runningChildren() []crew.Child {
	return []crew.Child{
		{
			Name:   "go-dev",
			Owns:   []string{"internal/judge/**"},
			Doing:  "writing policy/toolgate.go",
			Since:  2*time.Minute + 14*time.Second,
			State:  crew.Running,
			Tokens: 181000,
		},
		{
			Name:   "bench",
			Owns:   []string{"bench/harness/**"},
			Doing:  "go test ./bench/...",
			Since:  time.Minute + 2*time.Second,
			State:  crew.Running,
			Tokens: 129100,
		},
		{
			Name:   "go-docs",
			Owns:   []string{"docs/**"},
			Doing:  "reading docs/verification.md",
			Since:  9 * time.Second,
			State:  crew.Running,
			Tokens: 4200,
		},
		{Name: "go-rules", Owns: []string{"catalog/**"}, Doing: "handed back", State: crew.HandedBack, Tokens: 900},
	}
}

func TestEachRunningChildIsARowCarryingWhatItSpent(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	app.Update(Event{Kind: EventCrew, Children: runningChildren()})
	view := app.View()
	rows := plainRows(view)
	all := spinningRows(rows)
	if len(all) != 5 {
		t.Fatalf("a call, three children and a turn draw %d spinning rows, want 5\n%s", len(all), strings.Join(rows, "\n"))
	}
	running := all[1:]
	top := composerTopRow(t, view.Content)
	if running[3] != top-1 || running[0] != top-4 {
		t.Errorf("the footer block is on rows %v and the composer begins at row %d\n%s", running, top, strings.Join(rows, "\n"))
	}
	for index, want := range []string{"go-dev", "bench", "go-docs", "working"} {
		if !strings.Contains(rows[running[index]], want) {
			t.Errorf("row %d does not name %q: %q", index, want, rows[running[index]])
		}
	}
	for index, want := range []string{"181k", "129k", "4k"} {
		if !strings.Contains(rows[running[index]], want) {
			t.Errorf("row %d does not carry the tokens %q: %q", index, want, rows[running[index]])
		}
	}
	if strings.Contains(strings.Join(rows[:top], "\n"), "go-rules") {
		t.Errorf("a child that is not running took a row\n%s", strings.Join(rows, "\n"))
	}
	assertGolden(t, "session-children-80x24.golden", view.Content)
}

func turnRow(t *testing.T, app *App) string {
	t.Helper()
	rows := plainRows(app.View())
	spinning := spinningRows(rows)
	if len(spinning) == 0 {
		t.Fatalf("no running row in the frame:\n%s", strings.Join(rows, "\n"))
	}
	return rows[spinning[len(spinning)-1]]
}

func turnElapsed(t *testing.T, app *App) string {
	t.Helper()
	return strings.Fields(turnRow(t, app))[1]
}

func TestTheClockTimesTheWholeTurnAndNotTheNewestCall(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	at = at.Add(40 * time.Second)
	app.Update(Event{Kind: EventToolResult, ID: "c2", Text: "14 lines, 64 bytes"})
	app.Update(Event{Kind: EventToolCall, ID: "c3", Tool: "bash", Text: "go test ./internal/..."})
	if elapsed := turnElapsed(t, app); elapsed != "41s" {
		t.Errorf("a call starting after 41 seconds of work read the clock as %q", elapsed)
	}
}

func TestTheRunningRowNamesTheStateAndNotTheWordTurn(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	if row := turnRow(t, app); !strings.Contains(row, "working") || strings.Contains(row, "turn") {
		t.Errorf("a running call does not read as working: %q", row)
	}
	app.Update(Event{Kind: EventToolResult, ID: "c2", Text: "14 lines, 64 bytes"})
	at = at.Add(time.Second)
	if row := turnRow(t, app); !strings.Contains(row, "thinking") {
		t.Errorf("a turn waiting on the model does not read as thinking: %q", row)
	}
	app.Update(Event{Kind: EventToolCall, ID: "c3", Tool: "bash", Text: "go test ./internal/..."})
	app.Update(Event{Kind: EventAwaitPerson})
	at = at.Add(time.Second)
	if row := turnRow(t, app); !strings.Contains(row, "waiting") {
		t.Errorf("a turn waiting on the person does not say so: %q", row)
	}
}

func TestTheClockDoesNotAdvanceWhileTheTurnWaitsOnThePerson(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	at = at.Add(4 * time.Second)
	before := turnElapsed(t, app)
	if before != "5s" {
		t.Fatalf("the turn clock reads %q", before)
	}
	app.Update(Event{Kind: EventAwaitPerson})
	at = at.Add(30 * time.Second)
	if waited := turnElapsed(t, app); waited != before {
		t.Errorf("thirty seconds of waiting on the person moved the clock from %q to %q", before, waited)
	}
	app.Update(Event{Kind: EventResumed})
	at = at.Add(5 * time.Second)
	if after := turnElapsed(t, app); after != "10s" {
		t.Errorf("after the wait the turn reads %q, want 10s", after)
	}
}
