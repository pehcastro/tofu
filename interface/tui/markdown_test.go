package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/session"
)

func TestAHeadingRendersWhileTheEntryStreams(t *testing.T) {
	app := proseApp(t, 24)
	app.view.Stream("# Streaming heading\n\n")
	app.view.Stream("more of the answer is still arriving, one token at a time.")
	content := app.View().Content
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "Streaming heading") {
		t.Fatalf("the heading text is missing while streaming\n%s", plain)
	}
	if strings.Contains(plain, "# Streaming heading") {
		t.Errorf("the heading still carries its raw hash while streaming\n%s", plain)
	}
	if !strings.Contains(plain, "more of the answer is still arriving") {
		t.Errorf("the growing tail is missing\n%s", plain)
	}
	assertGolden(t, "session-markdown-streaming-80x24.golden", content)
}

func blankRunBefore(lines []string, index int) int {
	count := 0
	for i := index - 1; i >= 0 && strings.TrimSpace(lines[i]) == ""; i-- {
		count++
	}
	return count
}

func lineIndex(lines []string, want string) int {
	for i, line := range lines {
		if strings.Contains(line, want) {
			return i
		}
	}
	return -1
}

func twoTurnsApp(t *testing.T, height int) *App {
	t.Helper()
	app := proseApp(t, height)
	app.view.Append(session.Entry{Kind: session.User, Body: "why does the gate read the policy first?"})
	app.view.Append(session.Entry{Kind: session.Assistant, Body: "It reads the policy first.\n\nThat way a threshold never comes from a literal."})
	app.view.Append(session.Entry{Kind: session.User, Body: "and where do the thresholds live?"})
	app.view.Append(session.Entry{Kind: session.Assistant, Body: "In a lock file produced by calibration."})
	return app
}

func TestABlankLineSeparatesAPersonsMessageFromTheAnswerAbove(t *testing.T) {
	app := twoTurnsApp(t, 24)
	content := app.View().Content
	plain := ansi.Strip(content)
	lines := strings.Split(plain, "\n")
	at := lineIndex(lines, "and where do the thresholds live?")
	if at < 0 {
		t.Fatalf("the second person message is missing\n%s", plain)
	}
	if gap := blankRunBefore(lines, at); gap < 1 {
		t.Errorf("no blank line separates the person's message from the answer above it, got %d\n%s", gap, plain)
	}
	assertGolden(t, "session-turn-spacing-80x24.golden", content)
}

func TestTheBreakBetweenTwoTurnsIsLargerThanAnyBreakInsideOne(t *testing.T) {
	app := twoTurnsApp(t, 24)
	plain := ansi.Strip(app.View().Content)
	lines := strings.Split(plain, "\n")
	insideAnswer := lineIndex(lines, "That way a threshold never comes from a literal.")
	turnBreak := lineIndex(lines, "and where do the thresholds live?")
	if insideAnswer < 0 || turnBreak < 0 {
		t.Fatalf("could not find both markers\n%s", plain)
	}
	paragraphGap := blankRunBefore(lines, insideAnswer)
	turnGap := blankRunBefore(lines, turnBreak)
	if turnGap <= paragraphGap {
		t.Errorf("the turn break (%d blank lines) is not larger than the break inside one answer (%d)\n%s", turnGap, paragraphGap, plain)
	}
}

func TestARealRecordedAnswerRendersHeadingsBulletsAndInlineCode(t *testing.T) {
	source, err := os.ReadFile("testdata/markdown-real-turn-18d7430fd0c0c304.md")
	if err != nil {
		t.Fatal(err)
	}
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  anthropicAlone,
		Turn:   func(context.Context, Pick, string, CalledFromInsideTheTurnAndNeverAfterItReturns) {},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 90})
	app.view.Append(session.Entry{Kind: session.Assistant, Body: string(source)})
	content := app.View().Content
	plain := ansi.Strip(content)
	if strings.Contains(plain, "## What it is") {
		t.Errorf("a heading still carries its raw hashes\n%s", plain)
	}
	if !strings.Contains(plain, "What it is") {
		t.Errorf("the first heading's text is missing\n%s", plain)
	}
	if strings.Contains(plain, "library/  :") || strings.Contains(plain, "library/ :") {
		t.Errorf("a bullet reads as a mangled fragment\n%s", plain)
	}
	if !strings.Contains(plain, "library/") {
		t.Errorf("a nested bullet's code span is missing\n%s", plain)
	}
	if strings.Contains(plain, "`develop`") {
		t.Errorf("inline code still carries its backticks\n%s", plain)
	}
	assertGolden(t, "markdown-real-answer-120x90.golden", content)
}

func TestABlankLineSitsAboveTheActivityBlock(t *testing.T) {
	at := fixedClock()()
	app := startTurn(t, &at, 80, 24)
	startCall(app, 0, runCall{tool: "read", text: "internal/turn/loop.go"})
	content := app.View().Content
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "working") && !strings.Contains(plain, "requesting") {
		t.Fatalf("the activity line is missing\n%s", plain)
	}
	assertGolden(t, "session-activity-gap-80x24.golden", content)
}
