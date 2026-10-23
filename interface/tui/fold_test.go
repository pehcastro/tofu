package tui

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/frametime"
	"tofu/internal/golden"
	roster "tofu/internal/subagent"
)

func TestTheLeadsToolCallsFoldIntoOneChatRowAndTheTurnLineNamesTheLatest(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := startTurn(t, &at, 120, runHeight)

	startCall(app, 0, runCall{tool: "read", text: "internal/turn/loop.go", decision: allowed()})
	at = at.Add(callStep)
	endCall(app, 0, runCall{tool: "read", text: "internal/turn/loop.go", result: "84 lines, 2.1 KB", bytes: 2148})
	startCall(app, 1, runCall{tool: "bash", text: "go test ./internal/turn/..."})
	at = at.Add(callStep)

	row := turnRow(t, app)
	for _, want := range []string{"jev 1", "shell (1)", "bash go test ./internal/turn/...", "[tool#"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the turn line does not say %q while the lead works\n%s", want, row)
		}
	}
	if strings.Contains(row, ") tools") {
		t.Fatalf("the turn line repeats the call count the chat's running row already shows\n%s", row)
	}
	running := ansi.Strip(app.View().Content)
	if strings.Contains(running, "loop.go") || strings.Count(running, "go test") != 1 || strings.Count(running, "2 calls") != 1 {
		t.Fatalf("the lead's two calls did not fold into one running row in the chat\n%s", running)
	}

	at = at.Add(callStep)
	endCall(app, 1, runCall{tool: "bash", text: "go test ./internal/turn/...", result: "ok 0.4s", bytes: 96})
	app.Update(Event{Kind: EventText, Text: runAnswer})
	finishTurn(app)
	ended := ansi.Strip(app.View().Content)
	for _, gone := range []string{"loop.go", "go test", ") tools", "[expand]"} {
		if strings.Contains(ended, gone) {
			t.Fatalf("the chat still carries %q once the turn has ended\n%s", gone, ended)
		}
	}
	if strings.Count(ended, "2 calls") != 1 {
		t.Fatalf("the ended turn lost its one row for the two calls\n%s", ended)
	}
	if !strings.Contains(ended, runTask) || !strings.Contains(ended, "the gate reads the policy") {
		t.Fatalf("the person's words or the lead's answer left the chat with the tool rows\n%s", ended)
	}
	golden.Assert(t, "session-lead-tools-out-of-chat-120x24.golden", app.View().Content)
}

func TestTheLeadsToolCallsBetweenTwoMessagesTakeOneRowWithAGapEachSide(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := startTurn(t, &at, runWidth, runHeight)
	app.Update(Event{Kind: EventText, Text: "first note"})
	for index := range 3 {
		startCall(app, index, runCall{tool: "read", text: "a.go"})
		endCall(app, index, runCall{tool: "read", text: "a.go", result: "ok", bytes: 10})
	}
	app.Update(Event{Kind: EventText, Text: "second note"})
	finishTurn(app)
	rows := plainRows(app.View())
	first := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, "first note") })
	second := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, "second note") })
	if first < 0 || second-first != 5 || !strings.Contains(rows[first+2], "3 calls") {
		t.Fatalf("two messages around three calls sit %d rows apart, want 5: the words, a gap, the one 3 calls row, a gap, the head\n%s", second-first, strings.Join(rows, "\n"))
	}
}

func TestWhileASpawnIsOutstandingTheTurnLineNamesTheSpawnAndNotTheCallBefore(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := startTurn(t, &at, 120, runHeight)
	startCall(app, 0, runCall{tool: "bash", text: "sleep 3"})
	at = at.Add(callStep)
	endCall(app, 0, runCall{tool: "bash", text: "sleep 3", result: "ok", bytes: 2})
	app.Update(Event{Kind: EventToolCall, ID: "s1", Tool: "spawn", Text: "read the note and say what it holds", Promote: true})
	at = at.Add(callStep)
	row := turnRow(t, app)
	if strings.Contains(row, "sleep 3") || !strings.Contains(row, "spawn read the note and say what it holds") || !strings.Contains(row, "[spawn#s1]") {
		t.Fatalf("the turn line does not name the outstanding spawn by its id: %q", row)
	}
	app.Update(Event{Kind: EventToolResult, ID: "s1", Text: "spawned"})
	if row := turnRow(t, app); strings.Contains(row, "sleep 3") || !strings.Contains(row, "[spawn#s1]") {
		t.Fatalf("a spawn that returned at once gave the line back to the call before it: %q", row)
	}
	startCall(app, 1, runCall{tool: "read", text: "note.txt"})
	at = at.Add(callStep)
	if row := turnRow(t, app); !strings.Contains(row, "read note.txt") || strings.Contains(row, "[spawn#s1]") {
		t.Fatalf("a call newer than the spawn does not take the line: %q", row)
	}
}

func TestChatShowsToolsOnStillDrawsEveryLeadCall(t *testing.T) {
	app := settingsStoreApp(t, filepath.Join(t.TempDir(), "settings.json"))
	switchOnToolDetail(t, app)
	app.runCommand("chat")
	app.Update(Event{Kind: EventRequesting})
	startCall(app, 0, runCall{tool: "read", text: "internal/turn/loop.go"})
	endCall(app, 0, runCall{tool: "read", text: "internal/turn/loop.go", result: "84 lines, 2.1 KB", bytes: 2148})
	finishTurn(app)
	if row := rowHolding(t, app, "read internal/turn/loop.go"); !strings.Contains(row, "84 lines") {
		t.Fatalf("chatShowsTools on lost the call's own row and its status\n%s", row)
	}
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

func TestALongSessionWhoseSubAgentsSettledHoursAgoDrawsInsideTheBudget(t *testing.T) {
	s := generatedLongSession(t, longSizeFromEnv(t))
	ended := *s.at
	for index := range s.rows {
		s.rows[index].Started, s.rows[index].Ended = fixedStart(), ended
	}
	*s.at = ended.Add(3 * time.Hour)
	s.rows[0].State = roster.Working
	s.app.Update(Event{Kind: EventSubAgent, SubAgents: slices.Clone(s.rows)})
	s.frames(t, "settled 3h ago, chat, a key behind the deltas", s.typing)
	s.app.show(screenAgents)
	s.frames(t, "settled 3h ago, sub-agents, thinking streams", func(at int) {
		for range longChunksAFrame {
			s.app.Update(Event{Kind: EventThinking, ID: "streaming-" + strconv.Itoa(at), Agent: s.rows[0].Name, Text: longThinking})
		}
	})
}

func TestTheSessionViewOfARunningTurnRendersInsideTheFrameBudget(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := runningApp(t, &at)
	frametime.Frames(t, "a running turn at "+strconv.Itoa(runWidth)+"x"+strconv.Itoa(runHeight), func() { app.View() })
}
