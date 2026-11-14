package app

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	roster "tofu/internal/agent/subagents"
	"tofu/internal/testkit/golden"
	"tofu/internal/ui/tui/look/look"
	"tofu/internal/ui/tui/panels/progress"
	"tofu/internal/ui/tui/panels/subagent"
)

const spinnerFrames = progress.Frames + progress.WorkFrames

func spinningRows(rows []string) []int {
	var found []int
	for index, row := range rows {
		if strings.ContainsAny(row, spinnerFrames) {
			found = append(found, index)
		}
	}
	return found
}

const (
	composerBlockRows = 3
	breathingRows     = 1
)

func composerTint() string {
	return escapeOf(lipgloss.NewStyle().Background(lipgloss.Color(string(look.PanelLight))))
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

func TestTheStatusLineSitsBetweenTheTranscriptAndTheComposerUnderTheLeadsRow(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	view := app.View()
	rows := plainRows(view)
	top := composerTopRow(t, view.Content)
	running := spinningRows(rows)
	if len(running) != 2 || !strings.Contains(rows[running[0]], "running bash · (3) steps") {
		t.Fatalf("the frame carries spinning rows %v, want the lead's row in the chat and the status line\n%s", running, strings.Join(rows, "\n"))
	}
	if running[1] != top-1-breathingRows {
		t.Errorf("the request row is row %d and the composer begins at row %d, want one breathing row between them\n%s",
			running[1], top, strings.Join(rows, "\n"))
	}
	if call := slices.IndexFunc(rows[:running[1]], func(row string) bool { return strings.Contains(row, "CLAUDE.md") }); call >= 0 {
		t.Errorf("row %d draws a lead call above the status line\n%s", call, strings.Join(rows, "\n"))
	}
	if strings.Contains(rows[running[1]], "[tool#") {
		t.Errorf("the status line names a call, want working and its elapsed time alone\n%s", rows[running[1]])
	}
	golden.Assert(t, "session-activity-80x24.golden", view.Content)
}

func TestWithNothingRunningThereIsNoActivityRow(t *testing.T) {
	app := sessionApp(t, 80, 24)
	rows := plainRows(app.View())
	if running := spinningRows(rows); len(running) != 0 {
		t.Errorf("an idle session draws %d spinning rows on %v\n%s", len(running), running, strings.Join(rows, "\n"))
	}
	golden.Assert(t, "session-80x24.golden", app.View().Content)
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

func TestScrollingTheTranscriptDoesNotMoveTheRunningRow(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	for step := range 40 {
		app.Update(Event{Kind: EventNote, Text: "note " + strings.Repeat("x", step%7)})
	}
	tail := app.View()
	before := spinningRows(plainRows(tail))
	app.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	view := app.View()
	rows := plainRows(view)
	if transcriptOf(view.Content) == transcriptOf(tail.Content) {
		t.Fatalf("pgup did not scroll the transcript back\n%s", strings.Join(rows, "\n"))
	}
	after := spinningRows(rows)
	if len(before) == 0 || len(after) == 0 || before[len(before)-1] != after[len(after)-1] {
		t.Errorf("the running row moved from %v to %v when the transcript scrolled\n%s",
			before, after, strings.Join(rows, "\n"))
	}
	golden.Assert(t, "session-activity-scrolled-80x24.golden", view.Content)
}

func runningSubAgents() []subagent.Row {
	return []subagent.Row{
		{
			Name:  "go-dev",
			Owns:  []string{"internal/judge/**"},
			Doing: "write the tool gate",
			Since: 2*time.Minute + 14*time.Second,
			State: roster.Working,
			Calls: []subagent.Call{{ID: "d1", Tool: "edit", Text: "policy/toolgate.go"}},
		},
		{
			Name:  "bench",
			Owns:  []string{"bench/kit/harness/**"},
			Doing: "run the bench",
			Since: time.Minute + 2*time.Second,
			State: roster.Working,
			Calls: []subagent.Call{{ID: "b1", Tool: "bash", Text: "go test ./bench/..."}},
		},
		{
			Name:  "go-docs",
			Owns:  []string{"docs/**"},
			Doing: "check the docs",
			Since: 9 * time.Second,
			State: roster.Working,
		},
		{Name: "go-rules", Owns: []string{"library/**"}, Doing: "tidy the rules", State: roster.InReview},
	}
}

func TestSubAgentWorkSpinsOnOneLinePerStatusAndOneStatusLineSitsAboveTheComposer(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	subAgents := runningSubAgents()
	for index, subAgent := range subAgents {
		app.Update(Event{Kind: EventToolCall, ID: "5a0" + strconv.Itoa(index), Tool: "spawn", Text: subAgent.Doing, Promote: true})
		app.Update(Event{Kind: EventSubAgent, SubAgents: subAgents[:index+1]})
	}
	view := app.View()
	rows := plainRows(view)
	top := composerTopRow(t, view.Content)
	status := top - 1 - breathingRows
	if !strings.Contains(rows[status], "working") || strings.TrimSpace(rows[status-1]) != "" {
		t.Errorf("the rows above the composer are %q and %q, want one status line under a blank row\n%s", rows[status-1], rows[status], strings.Join(rows, "\n"))
	}
	working := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, "working · [&go-dev], [&bench], [&go-docs]") })
	if working < 0 || working >= status || !strings.ContainsRune(progress.WorkFrames, []rune(strings.TrimSpace(rows[working]))[0]) || strings.Contains(rows[working], "toolgate.go") {
		t.Errorf("the three working sub-agents, one with no call yet, do not spin in Dots8 on one line under their status without their files\n%s", strings.Join(rows, "\n"))
	}
	if slices.ContainsFunc(rows, func(row string) bool { return strings.Contains(row, "[&go-rules]") }) {
		t.Errorf("the sub-agent in review still draws a row in the chat\n%s", strings.Join(rows, "\n"))
	}
	golden.Assert(t, "session-sub-agents-80x24.golden", view.Content)
}

func chatRowsNaming(t *testing.T, app *App, name string) []string {
	t.Helper()
	view := app.View()
	rows := plainRows(view)
	var named []string
	for _, row := range rows[:composerTopRow(t, view.Content)-1-breathingRows] {
		if strings.Contains(row, name) {
			named = append(named, strings.TrimSpace(row))
		}
	}
	return named
}

func TestFiveSubAgentsShareOneChatLineAndTheFinishedLeaveIt(t *testing.T) {
	at := fixedStart()
	app := phaseApp(t, &at)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	var batch []subagent.Row
	for index := range 5 {
		name := "ts-dev-" + strconv.Itoa(index+1)
		batch = append(batch, subagent.Row{
			Name: name, Owns: []string{"src/" + name + "/**"}, Doing: "the routes of " + name, Since: time.Minute + 12*time.Second, State: roster.Working,
			Calls: []subagent.Call{{ID: "r" + name, Tool: "read", Text: "src/" + name + "/index.ts"}},
		})
		app.Update(Event{Kind: EventToolCall, ID: "5a" + strconv.Itoa(index), Tool: "spawn", Text: "the routes of " + name, Promote: true})
		app.Update(Event{Kind: EventSubAgent, SubAgents: slices.Clone(batch)})
	}
	running := chatRowsNaming(t, app, "[&ts-dev-")
	if len(running) != 1 || !strings.Contains(running[0], "working · [&ts-dev-1], [&ts-dev-2], [&ts-dev-3], [&ts-dev-4], [&ts-dev-5]") {
		t.Fatalf("five running sub-agents draw %q in the chat, want one line naming all five", running)
	}
	if strings.Contains(running[0], "the routes of") || strings.Contains(running[0], "index.ts") {
		t.Errorf("the running line carries a mission or a file: %q", running[0])
	}
	batch[0].State, batch[3].State = roster.Finished, roster.Finished
	app.Update(Event{Kind: EventSubAgent, SubAgents: slices.Clone(batch)})
	lines := chatRowsNaming(t, app, "[&ts-dev-")
	if len(lines) != 1 || !strings.Contains(lines[0], "working · [&ts-dev-2], [&ts-dev-3], [&ts-dev-5]") {
		t.Fatalf("after two finish the chat draws %q, want one working line with the three left and no row for the finished", lines)
	}
	golden.Assert(t, "session-sub-agents-batch-120x30.golden", app.View().Content)
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
	fields := strings.Fields(turnRow(t, app))
	for index, field := range fields {
		if field == "|" && index+1 < len(fields) {
			return fields[index+1]
		}
	}
	t.Fatalf("the running row carries no elapsed time: %q", turnRow(t, app))
	return ""
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
	if row := turnRow(t, app); !strings.Contains(row, "working") || strings.Contains(row, "thinking") {
		t.Errorf("a turn whose model sent no thinking does not read as working: %q", row)
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

func TestAReplayedSpawnKeepsTheTimeItWasAskedAndALiveOneTakesTheClock(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	asked := at.Add(-time.Hour)
	app.Update(Event{Kind: EventToolCall, ID: "5b01", Tool: "spawn", Text: "MOCKTASK-1", Promote: true, At: asked})
	app.Update(Event{Kind: EventToolResult, ID: "5b01", Text: "sub-1 is running in the background", At: asked.Add(time.Second), DurationMS: 1000})
	app.Update(Event{Kind: EventToolCall, ID: "5b02", Tool: "spawn", Text: "MOCKTASK-2", Promote: true})
	replayed, live := app.happened[app.happenedAt(app.shortOf("5b01"))], app.happened[app.happenedAt(app.shortOf("5b02"))]
	if !replayed.At.Equal(asked) || replayed.Elapsed != time.Second {
		t.Errorf("a spawn replayed from an hour ago is stamped %v and took %v, want %v and 1s", replayed.At, replayed.Elapsed, asked)
	}
	if !live.At.Equal(at) {
		t.Errorf("a live spawn with no time of its own is stamped %v, want the clock %v", live.At, at)
	}
}

func TestALiveAndAReplayedToolBothTakeTheLoopsDuration(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	app.Update(Event{Kind: EventToolCall, ID: "d1", Tool: "read", Text: "a.txt"})
	at = at.Add(3 * time.Second)
	app.Update(Event{Kind: EventToolResult, ID: "d1", Text: "1 line", DurationMS: 250})
	app.Update(Event{Kind: EventToolCall, ID: "d2", Tool: "read", Text: "b.txt"})
	at = at.Add(3 * time.Second)
	app.Update(Event{Kind: EventToolResult, ID: "d2", Text: "1 line"})
	asked := at.Add(-time.Hour)
	app.Update(Event{Kind: EventToolCall, ID: "d3", Tool: "read", Text: "c.txt", At: asked})
	app.Update(Event{Kind: EventToolResult, ID: "d3", Text: "1 line", At: asked.Add(2 * time.Second), DurationMS: 40})
	app.Update(Event{Kind: EventToolCall, ID: "d4", Tool: "read", Text: "d.txt", At: asked})
	app.Update(Event{Kind: EventToolResult, ID: "d4", Text: "1 line", At: asked.Add(2 * time.Second), Untimed: true})
	want := map[string]time.Duration{"d1": 250 * time.Millisecond, "d2": 0, "d3": 40 * time.Millisecond, "d4": 2 * time.Second}
	for id, took := range want {
		if got := app.happened[app.happenedAt(app.shortOf(id))].Elapsed; got != took {
			t.Errorf("tool %s took %v on its row, want %v", id, got, took)
		}
	}
}
