package tui

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/session"
	"tofu/interface/tui/theme"
	"tofu/internal/golden"
)

const (
	runWidth  = 80
	runHeight = 24
	callStep  = 3 * time.Second
	runAnswer = "the gate reads the policy before the wire, and here is why."
	runTask   = "why does the gate read the policy first?"
)

type runCall struct {
	tool     string
	text     string
	detail   string
	result   string
	bytes    int
	failed   bool
	decision *session.Decision
}

func fourteenCalls() []runCall {
	return []runCall{
		{tool: "read", text: "internal/turn/loop.go", result: "84 lines, 2.1 KB", bytes: 2148},
		{tool: "bash", text: "go build ./...", result: "no output", bytes: 96},
		{tool: "read", text: "internal/judge/policy/toolgate.go", result: "412 lines, 11.8 KB", bytes: 11800},
		{tool: "bash", text: "go test ./internal/judge/...", result: "ok tofu/internal/judge 0.42s", bytes: 412},
		{tool: "glob", text: "internal/**/*.go", result: "184 paths", bytes: 3140},
		{tool: "read", text: "internal/judge/jev/wire.go", result: "266 lines, 8.2 KB", bytes: 8210},
		{tool: "bash", text: "go vet ./internal/...", result: "no output", bytes: 128},
		{tool: "read", text: "internal/point/toolgate.go", result: "141 lines, 4.1 KB", bytes: 4096},
		{tool: "bash", text: "rg toolgate internal", result: "31 lines, 2.2 KB", bytes: 2210},
		{tool: "read", text: "library/questions/tool_gate.yaml", result: "58 lines, 1.7 KB", bytes: 1740},
		{tool: "bash", text: "go test ./internal/point/...", result: "ok tofu/internal/point 0.31s", bytes: 380},
		{tool: "read", text: "internal/turn/budget.go", result: "172 lines, 5.3 KB", bytes: 5310},
		{tool: "bash", text: longIntent, detail: longCommand, result: "14 lines, 64 bytes", bytes: 64},
		{tool: "bash", text: "go tool deadcode -test ./...", result: "no output", bytes: 96},
	}
}

func startTurn(t *testing.T, at *time.Time, width, height int) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    func() time.Time { return *at },
		Wires:  anthropicAlone,
		Turn:   func(context.Context, Pick, string, CalledFromInsideTheTurnAndNeverAfterItReturns) {},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	typeText(app, runTask)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return app
}

func finishTurn(app *App) {
	app.Update(Event{Kind: EventDone, Text: "cooked for"})
	app.Update(Closed{})
}

func startCall(app *App, index int, one runCall) {
	app.Update(Event{Kind: EventToolCall, ID: "c" + strconv.Itoa(index), Tool: one.tool, Text: one.text, Detail: one.detail})
	if one.decision != nil {
		app.Update(Event{Kind: EventDecision, Decision: one.decision})
	}
}

func endCall(app *App, index int, one runCall) {
	app.Update(Event{Kind: EventToolResult, ID: "c" + strconv.Itoa(index), Text: one.result, Bytes: one.bytes, Failed: one.failed})
}

func wholeRun(t *testing.T, at *time.Time, width, height int, calls []runCall) *App {
	t.Helper()
	app := startTurn(t, at, width, height)
	for index, one := range calls {
		startCall(app, index, one)
		*at = at.Add(callStep)
		endCall(app, index, one)
	}
	return app
}

func runningApp(t *testing.T, at *time.Time) *App {
	t.Helper()
	calls := fourteenCalls()
	last := len(calls) - 1
	app := startTurn(t, at, runWidth, runHeight)
	for index, one := range calls[:last] {
		startCall(app, index, one)
		*at = at.Add(callStep)
		endCall(app, index, one)
	}
	startCall(app, last, calls[last])
	*at = at.Add(callStep)
	return app
}

func spokenApp(t *testing.T, at *time.Time) *App {
	t.Helper()
	calls := fourteenCalls()
	app := runningApp(t, at)
	endCall(app, len(calls)-1, calls[len(calls)-1])
	app.Update(Event{Kind: EventText, Text: runAnswer})
	return app
}

func callRows(content string) int {
	return strings.Count(transcriptOf(content), "⟩ ")
}

func TestARunOfFourteenCallsDrawsNothingButItsProgressLineWhileItRuns(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	calls := fourteenCalls()
	app := runningApp(t, &at)
	content := app.View().Content
	golden.Assert(t, "session-run-80x24.golden", content)
	plain := ansi.Strip(content)
	if strings.Contains(plain, " tools") {
		t.Errorf("the running run summarises a turn that has not ended\n%s", plain)
	}
	for _, unwanted := range []string{"read 6", "bash 7", "glob 1"} {
		if strings.Contains(plain, unwanted) {
			t.Errorf("the running run still names a tool by kind, got %q\n%s", unwanted, plain)
		}
	}
	if rows := callRows(plain); rows != 0 {
		t.Errorf("the running run drew %d call rows, want none\n%s", rows, plain)
	}

	endCall(app, len(calls)-1, calls[len(calls)-1])
	finishTurn(app)
	ended := ansi.Strip(app.View().Content)
	for _, want := range []string{"(14) tools", "shell (7)", "42s"} {
		if !strings.Contains(ended, want) {
			t.Errorf("the ended run does not say %q\n%s", want, ended)
		}
	}
}

func TestTheProgressLineReplacesItselfAndChangesColourWhenACallFinishes(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := startTurn(t, &at, runWidth, runHeight)
	first := runCall{tool: "read", text: "internal/turn/loop.go", result: "84 lines, 2.1 KB", bytes: 2148}
	second := runCall{tool: "bash", text: "go build ./...", result: "no output", bytes: 96}

	startCall(app, 0, first)
	running := app.View().Content
	golden.Assert(t, "session-progress-running-80x24.golden", running)
	if !strings.Contains(ansi.Strip(running), "read internal/turn/loop.go") {
		t.Fatalf("the running progress line does not name the call\n%s", ansi.Strip(running))
	}

	at = at.Add(callStep)
	endCall(app, 0, first)
	finished := app.View().Content
	golden.Assert(t, "session-progress-finished-80x24.golden", finished)
	runningOpen := escapeOf(theme.Accent())
	finishedOpen := escapeOf(theme.Added())
	if !strings.Contains(running, runningOpen) {
		t.Fatalf("the running frame does not carry the accent colour\n%s", running)
	}
	if !strings.Contains(finished, finishedOpen) {
		t.Fatalf("the finished frame does not carry the finished colour\n%s", finished)
	}

	startCall(app, 1, second)
	replaced := app.View().Content
	golden.Assert(t, "session-progress-replaced-80x24.golden", replaced)
	if strings.Contains(ansi.Strip(replaced), "internal/turn/loop.go") {
		t.Fatalf("the progress line still names the finished call once a new one starts\n%s", ansi.Strip(replaced))
	}
	if callRows(ansi.Strip(replaced)) != 0 {
		t.Fatalf("a second call grew the transcript with a call row\n%s", ansi.Strip(replaced))
	}
}

func sixKindsOfCall() []runCall {
	return []runCall{
		{tool: "read", text: "internal/turn/loop.go", result: "84 lines, 2.1 KB", bytes: 2148},
		{tool: "bash", text: "go build ./...", result: "no output", bytes: 96},
		{tool: "glob", text: "internal/**/*.go", result: "184 paths", bytes: 3140},
		{tool: "grep", text: "toolgate", result: "31 lines", bytes: 2210},
		{tool: "edit", text: "internal/turn/loop.go", result: "1 hunk applied", bytes: 240},
		{tool: "fetch", text: "https://go.dev", result: "200 ok", bytes: 512},
	}
}

func TestTheCountedLineNeverNamesAToolAcrossSixKinds(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := wholeRun(t, &at, runWidth, runHeight, sixKindsOfCall())
	finishTurn(app)
	content := app.View().Content
	row := ""
	for _, line := range strings.Split(ansi.Strip(content), "\n") {
		if strings.Contains(line, "(6) tools") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("the counted line does not count all six calls\n%s", ansi.Strip(content))
	}
	for _, unwanted := range []string{"read ", "glob ", "grep ", "edit ", "fetch "} {
		if strings.Contains(row, unwanted) {
			t.Errorf("the counted line names a tool by kind, got %q\n%s", unwanted, row)
		}
	}
	golden.Assert(t, "session-run-six-kinds-80x24.golden", content)
}

func TestTheRunCollapsesToOneDimLineWhenTheTurnEnds(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := spokenApp(t, &at)
	finishTurn(app)
	content := app.View().Content
	golden.Assert(t, "session-run-collapsed-80x24.golden", content)
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "· (14) tools · shell (7) · 42s") {
		t.Errorf("the collapsed run is not one counted line\n%s", plain)
	}
	if !strings.Contains(plain, runAnswer) {
		t.Errorf("the answer is not on the screen\n%s", plain)
	}
	if rows := callRows(plain); rows != 0 {
		t.Errorf("the collapsed run drew %d call rows, want none\n%s", rows, plain)
	}
}

func TestCtrlOLeavesChatForWorkAndShowsEveryCall(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := spokenApp(t, &at)
	app.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if app.current != viewWork {
		t.Fatal("ctrl+o did not leave chat for work")
	}
	content := app.View().Content
	golden.Assert(t, "work-open-80x24.golden", content)
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "wc -l") {
		t.Errorf("work does not show the whole command\n%s", plain)
	}
	if rows := callRows(plain); rows == 0 {
		t.Errorf("work drew no call rows\n%s", plain)
	}
}

func TestTheWorkRecordStillHoldsEveryCallAfterTheTurn(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	calls := fourteenCalls()
	app := wholeRun(t, &at, 120, 40, calls)
	app.Update(Event{Kind: EventText, Text: runAnswer})
	app.Update(Closed{})
	if got := len(app.work.Entries); got != len(calls) {
		t.Fatalf("work holds %d entries, want %d", got, len(calls))
	}
	for index, one := range calls {
		entry := app.work.Entries[index]
		if !strings.Contains(entry.Head, one.text) {
			t.Errorf("entry %d lost the call %q, has %q", index, one.text, entry.Head)
		}
		if !strings.Contains(entry.Output, one.result) {
			t.Errorf("entry %d lost the result %q, has %q", index, one.result, entry.Output)
		}
	}
}

func readsAround(middle runCall) []runCall {
	calls := make([]runCall, 0, 8)
	for step := range 8 {
		if step == 4 {
			calls = append(calls, middle)
			continue
		}
		calls = append(calls, runCall{
			tool:   "read",
			text:   "internal/judge/policy/toolgate.go line " + strconv.Itoa(step),
			result: "84 lines, 2.1 KB",
			bytes:  2148,
		})
	}
	return calls
}

func askedRun() []runCall {
	return readsAround(runCall{
		tool:     "bash",
		text:     "git push --force origin main",
		result:   "waiting for you",
		decision: asked(),
	})
}

func TestAGateAskIsNeverFoldedAndTheRunBreaksAroundIt(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := wholeRun(t, &at, runWidth, runHeight, askedRun())
	finishTurn(app)
	content := app.View().Content
	golden.Assert(t, "session-run-ask-80x24.golden", content)
	plain := ansi.Strip(content)
	for _, want := range []string{"git push --force origin main", "ask", "risk", "2.00",
		"risk is hard to undo or reaches outside the workspace"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the asked call does not show %q without being opened\n%s", want, plain)
		}
	}
	if rows := callRows(plain); rows != 1 {
		t.Errorf("the run drew %d call rows, want the asked one alone\n%s", rows, plain)
	}
	if folds := strings.Count(plain, " tools"); folds != 2 {
		t.Errorf("the run folded into %d lines, want one on each side of the ask\n%s", folds, plain)
	}
}

func failedRun() []runCall {
	return readsAround(runCall{
		tool:   "bash",
		text:   "go test ./internal/recall/...",
		result: "FAIL tofu/internal/recall 0.18s",
		failed: true,
	})
}

func TestAFailedCallIsNeverFoldedAndTheRunBreaksAroundIt(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := wholeRun(t, &at, runWidth, runHeight, failedRun())
	finishTurn(app)
	content := app.View().Content
	golden.Assert(t, "session-run-failure-80x24.golden", content)
	plain := ansi.Strip(content)
	for _, want := range []string{"go test ./internal/recall/...", "FAIL tofu/internal/recall 0.18s"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the failed call does not show %q without being opened\n%s", want, plain)
		}
	}
	if rows := callRows(plain); rows != 1 {
		t.Errorf("the run drew %d call rows, want the failed one alone\n%s", rows, plain)
	}
	if !strings.Contains(content, theme.Fail().Render("FAIL tofu/internal/recall 0.18s")) {
		t.Errorf("the failure is drawn like every other result\n%q", content)
	}
	if folds := strings.Count(plain, " tools"); folds != 2 {
		t.Errorf("the run folded into %d lines, want one on each side of the failure\n%s", folds, plain)
	}
}

func twelveCalls() []runCall {
	calls := make([]runCall, 0, 12)
	for _, kind := range []struct {
		tool  string
		times int
	}{{"read", 5}, {"bash", 4}, {"glob", 3}} {
		for step := range kind.times {
			calls = append(calls, runCall{
				tool:   kind.tool,
				text:   kind.tool + " target " + strconv.Itoa(step),
				result: "done",
				bytes:  1000,
			})
		}
	}
	return calls
}

func TestTheCountsOnTheFoldedLineAreRight(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	calls := twelveCalls()
	app := startTurn(t, &at, runWidth, runHeight)
	for index, one := range calls {
		startCall(app, index, one)
		at = at.Add(callStep)
		endCall(app, index, one)
	}
	last := runCall{tool: "edit", text: "internal/turn/loop.go", result: "1 hunk applied", bytes: 240}
	startCall(app, len(calls), last)
	endCall(app, len(calls), last)
	finishTurn(app)
	plain := ansi.Strip(app.View().Content)
	if !strings.Contains(plain, "(13) tools · shell (4)") {
		t.Errorf("the folded line does not count the tools and the shell calls\n%s", plain)
	}
	for _, unwanted := range []string{"read 5", "glob 3", "edit 1"} {
		if strings.Contains(plain, unwanted) {
			t.Errorf("the folded line still names a tool by kind, got %q\n%s", unwanted, plain)
		}
	}
}
