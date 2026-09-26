package tui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/frametime"
	"tofu/interface/tui/look"
	"tofu/internal/golden"
)

func rawRowHolding(t *testing.T, app *App, text string) string {
	t.Helper()
	for _, line := range strings.Split(app.View().Content, "\n") {
		if strings.Contains(ansi.Strip(line), text) {
			return line
		}
	}
	t.Fatalf("no row holds %q\n%s", text, app.View().Content)
	return ""
}

func TestNoFoldLineDrawsWhileTheTurnRunsAndItArrivesFaintAtTheEnd(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := startTurn(t, &at, runWidth, runHeight)

	startCall(app, 0, runCall{tool: "read", text: "internal/turn/loop.go"})
	at = at.Add(callStep)
	endCall(app, 0, runCall{tool: "read", text: "internal/turn/loop.go", result: "84 lines, 2.1 KB", bytes: 2148})
	startCall(app, 1, runCall{tool: "bash", text: "go test ./internal/turn/..."})
	at = at.Add(callStep)
	endCall(app, 1, runCall{tool: "bash", text: "go test ./internal/turn/...", result: "ok 0.4s", bytes: 96})

	if running := ansi.Strip(app.View().Content); strings.Contains(running, "(2) tools") {
		t.Fatalf("a turn that has not ended summarises itself\n%s", running)
	}

	app.Update(Event{Kind: EventText, Text: runAnswer})
	if spoken := ansi.Strip(app.View().Content); strings.Contains(spoken, "(2) tools") {
		t.Fatalf("the answer arriving is not the end of the turn, and the summary drew anyway\n%s", spoken)
	}

	finishTurn(app)
	final := rawRowHolding(t, app, "(2) tools")
	if !strings.Contains(final, escapeOf(look.Style(look.FaintColor))) {
		t.Fatalf("the fold line is not faint once the turn has ended\n%q", final)
	}
}

func TestTheFoldLineElapsedCountsFromTheSendAndNeverRestarts(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := startTurn(t, &at, runWidth, runHeight)

	startCall(app, 0, runCall{tool: "read", text: "a"})
	at = at.Add(callStep)
	endCall(app, 0, runCall{tool: "read", text: "a", result: "ok", bytes: 10})
	startCall(app, 1, runCall{tool: "read", text: "b"})
	at = at.Add(callStep)
	endCall(app, 1, runCall{tool: "read", text: "b", result: "ok", bytes: 10})
	app.Update(Event{Kind: EventText, Text: "first note"})

	at = at.Add(callStep)
	startCall(app, 2, runCall{tool: "bash", text: "c"})
	at = at.Add(callStep)
	endCall(app, 2, runCall{tool: "bash", text: "c", result: "ok", bytes: 10})
	startCall(app, 3, runCall{tool: "bash", text: "d"})
	at = at.Add(callStep)
	endCall(app, 3, runCall{tool: "bash", text: "d", result: "ok", bytes: 10})
	finishTurn(app)

	firstFold := rowHolding(t, app, "(2) tools")
	if !strings.Contains(firstFold, "6s") {
		t.Fatalf("the first fold does not read 6s since the send\n%s", firstFold)
	}

	secondFold := rowHolding(t, app, "(2) tools · shell (2)")
	if !strings.Contains(secondFold, "15s") {
		t.Fatalf("the second fold does not count from the send: %q, want 15s", secondFold)
	}
	if strings.Contains(secondFold, "6s") {
		t.Fatalf("the second fold restarted from its own block instead of the send\n%s", secondFold)
	}
}

func TestTheFoldLineNamesToolsJevShellAndTimeWithNoByteFigureOrToolName(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := startTurn(t, &at, runWidth, runHeight)
	startCall(app, 0, runCall{tool: "read", text: "internal/judge/policy/toolgate.go", decision: allowed()})
	at = at.Add(callStep)
	endCall(app, 0, runCall{tool: "read", text: "internal/judge/policy/toolgate.go", result: "412 lines, 11.8 KB", bytes: 11800})
	startCall(app, 1, runCall{tool: "bash", text: "go test ./internal/judge/..."})
	at = at.Add(callStep)
	endCall(app, 1, runCall{tool: "bash", text: "go test ./internal/judge/...", result: "ok tofu/internal/judge 0.42s", bytes: 412})
	app.Update(Event{Kind: EventText, Text: runAnswer})
	finishTurn(app)

	content := app.View().Content
	row := rowHolding(t, app, "(2) tools")
	for _, want := range []string{"(2) tools", "jev 1", "shell (1)", "6s"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the fold line does not say %q\n%s", want, row)
		}
	}
	for _, unwanted := range []string{"KB", "bytes", "read ", "bash "} {
		if strings.Contains(row, unwanted) {
			t.Fatalf("the fold line names a tool or a byte figure, got %q in\n%s", unwanted, row)
		}
	}
	golden.Assert(t, "session-fold-line-80x24.golden", content)
}

func TestATranscriptLongerThanThePaneAnchorsToTheBottomAndScrolls(t *testing.T) {
	app := longApp(t)
	view := app.View()
	rows := plainRows(view)
	top := composerTopRow(t, view.Content)
	for index := bodyTop; index < top-breathingRows; index++ {
		if strings.TrimSpace(rows[index]) == "" {
			t.Fatalf("row %d above the composer is blank in a transcript longer than the pane\n%s", index, strings.Join(rows, "\n"))
		}
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if app.View().Content == view.Content {
		t.Fatal("a long transcript did not scroll on pgup")
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if app.View().Content != view.Content {
		t.Fatal("end did not return a long transcript to the anchored tail")
	}
}

func TestTheSessionViewOfARunningTurnRendersInsideTheFrameBudget(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := runningApp(t, &at)
	frametime.Frames(t, "a running turn at "+strconv.Itoa(runWidth)+"x"+strconv.Itoa(runHeight), func() { app.View() })
}
