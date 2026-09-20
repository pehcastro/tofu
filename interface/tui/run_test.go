package tui

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"boji/interface/tui/session"
	"boji/interface/tui/theme"
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
		{tool: "bash", text: "go test ./internal/judge/...", result: "ok boji/internal/judge 0.42s", bytes: 412},
		{tool: "glob", text: "internal/**/*.go", result: "184 paths", bytes: 3140},
		{tool: "read", text: "internal/judge/jev/wire.go", result: "266 lines, 8.2 KB", bytes: 8210},
		{tool: "bash", text: "go vet ./internal/...", result: "no output", bytes: 128},
		{tool: "read", text: "internal/point/toolgate.go", result: "141 lines, 4.1 KB", bytes: 4096},
		{tool: "bash", text: "rg toolgate internal", result: "31 lines, 2.2 KB", bytes: 2210},
		{tool: "read", text: "catalog/questions/tool_gate.yaml", result: "58 lines, 1.7 KB", bytes: 1740},
		{tool: "bash", text: "go test ./internal/point/...", result: "ok boji/internal/point 0.31s", bytes: 380},
		{tool: "read", text: "internal/turn/budget.go", result: "172 lines, 5.3 KB", bytes: 5310},
		{tool: "bash", text: longIntent, detail: longCommand, result: "14 lines, 64 bytes", bytes: 64},
		{tool: "bash", text: "go tool deadcode -test ./...", result: "no output", bytes: 96},
	}
}

func startTurn(t *testing.T, at *time.Time, width, height int) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:   "silo",
		Branch: "develop",
		Now:    func() time.Time { return *at },
		Wires:  anthropicAlone,
		Turn:   func(context.Context, string, string, func(Event)) {},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	typeText(app, runTask)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return app
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

func TestARunOfFourteenCallsIsOneLineWhileItRuns(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	content := runningApp(t, &at).View().Content
	assertGolden(t, "session-run-80x24.golden", content)
	plain := ansi.Strip(content)
	for _, want := range []string{"14 tools", "read 6", "bash 7", "glob 1", "38.8 KB", "42s"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the running run does not say %q\n%s", want, plain)
		}
	}
	if rows := callRows(plain); rows != 0 {
		t.Errorf("the running run drew %d call rows, want none\n%s", rows, plain)
	}
}

func TestTheRunCollapsesToOneDimLineWhenTheModelSpeaks(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	content := spokenApp(t, &at).View().Content
	assertGolden(t, "session-run-collapsed-80x24.golden", content)
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "· 14 tools, 38.9 KB, 42s") {
		t.Errorf("the collapsed run is not one counted line\n%s", plain)
	}
	if !strings.Contains(plain, runAnswer) {
		t.Errorf("the answer is not on the screen\n%s", plain)
	}
	if rows := callRows(plain); rows != 0 {
		t.Errorf("the collapsed run drew %d call rows, want none\n%s", rows, plain)
	}
}

func TestCtrlOOpensACollapsedRunAndShowsEveryCall(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := spokenApp(t, &at)
	app.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	content := app.View().Content
	assertGolden(t, "session-run-open-80x24.golden", content)
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "wc -l") {
		t.Errorf("the opened run does not show the whole command\n%s", plain)
	}
	if rows := callRows(plain); rows == 0 {
		t.Errorf("the opened run drew no call rows\n%s", plain)
	}
}

func TestTheSessionRecordStillHoldsEveryCallAfterTheTurn(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	calls := fourteenCalls()
	app := wholeRun(t, &at, 120, 40, calls)
	app.Update(Event{Kind: EventText, Text: runAnswer})
	app.Update(closedMsg{})
	app.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	plain := ansi.Strip(app.View().Content)
	for _, one := range calls {
		if !strings.Contains(plain, one.text) {
			t.Errorf("the record lost the call %q\n%s", one.text, plain)
		}
		if !strings.Contains(plain, one.result) {
			t.Errorf("the record lost the result %q\n%s", one.result, plain)
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
	content := wholeRun(t, &at, runWidth, runHeight, askedRun()).View().Content
	assertGolden(t, "session-run-ask-80x24.golden", content)
	plain := ansi.Strip(content)
	for _, want := range []string{"git push --force origin main", "ask", "risk", "2.00", "risk 2.00 is over risk_ask_at 1.50"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the asked call does not show %q without being opened\n%s", want, plain)
		}
	}
	if rows := callRows(plain); rows != 1 {
		t.Errorf("the run drew %d call rows, want the asked one alone\n%s", rows, plain)
	}
	if folds := strings.Count(plain, " tools, "); folds != 2 {
		t.Errorf("the run folded into %d lines, want one on each side of the ask\n%s", folds, plain)
	}
}

func failedRun() []runCall {
	return readsAround(runCall{
		tool:   "bash",
		text:   "go test ./internal/recall/...",
		result: "FAIL boji/internal/recall 0.18s",
		failed: true,
	})
}

func TestAFailedCallIsNeverFoldedAndTheRunBreaksAroundIt(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	content := wholeRun(t, &at, runWidth, runHeight, failedRun()).View().Content
	assertGolden(t, "session-run-failure-80x24.golden", content)
	plain := ansi.Strip(content)
	for _, want := range []string{"go test ./internal/recall/...", "FAIL boji/internal/recall 0.18s"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the failed call does not show %q without being opened\n%s", want, plain)
		}
	}
	if rows := callRows(plain); rows != 1 {
		t.Errorf("the run drew %d call rows, want the failed one alone\n%s", rows, plain)
	}
	if !strings.Contains(content, theme.Fail().Render("FAIL boji/internal/recall 0.18s")) {
		t.Errorf("the failure is drawn like every other result\n%q", content)
	}
	if folds := strings.Count(plain, " tools, "); folds != 2 {
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
	startCall(app, len(calls), runCall{tool: "edit", text: "internal/turn/loop.go"})
	plain := ansi.Strip(app.View().Content)
	for _, want := range []string{"13 tools", "read 5", "bash 4", "glob 3", "edit 1", "11.7 KB"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the folded line does not count %q\n%s", want, plain)
		}
	}
}
