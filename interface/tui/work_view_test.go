package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/frametime"
	"tofu/interface/tui/work"
)

func twelveCallTurn(t *testing.T, width, height int) *App {
	t.Helper()
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := wholeRun(t, &at, width, height, twelveCalls())
	app.Update(Event{Kind: EventText, Text: runAnswer})
	finishTurn(app)
	return app
}

func TestARunningCallCarriesAnIDInChatThatReachesWork(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := startTurn(t, &at, 80, 24)
	startCall(app, 7, runCall{tool: "read", text: "internal/turn/loop.go"})
	at = at.Add(callStep)
	chat := ansi.Strip(app.View().Content)
	if strings.Contains(chat, " tools") {
		t.Fatalf("a running turn summarises itself\n%s", chat)
	}
	if !strings.Contains(chat, "#c7") {
		t.Fatalf("a running turn carries no id in chat\n%s", chat)
	}
	typeText(app, "#c7")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.current != viewWork {
		t.Fatal("the id a running turn shows does not move to work")
	}
	if picked, ok := app.work.Picked(); !ok || picked.ID != "c7" {
		t.Fatalf("work did not land on the running call: picked %+v", picked)
	}
}

func TestChatFoldsTwelveCallsToOneLineAndWorkDrawsEachWhole(t *testing.T) {
	for _, width := range []int{80, 120} {
		height := 24
		if width == 120 {
			height = 36
		}
		app := twelveCallTurn(t, width, height)
		chat := ansi.Strip(app.View().Content)
		if strings.Count(chat, "⟩ ") != 0 {
			t.Fatalf("chat at width %d drew a row per tool call\n%s", width, chat)
		}
		for _, gone := range []string{"read target 0", "bash target 0", "done", "allow"} {
			if strings.Contains(chat, gone) {
				t.Errorf("chat at width %d carries %q, which belongs in work\n%s", width, gone, chat)
			}
		}
		if !strings.Contains(chat, "(12) tools") {
			t.Fatalf("chat at width %d does not fold the turn to one line\n%s", width, chat)
		}

		app.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
		if got := len(app.work.Entries); got != 12 {
			t.Fatalf("work at width %d holds %d entries, want 12", width, got)
		}
		for _, want := range []string{"read target 0", "bash target 0", "glob target 0"} {
			if !slices.ContainsFunc(app.work.Entries, func(e work.Entry) bool { return strings.Contains(e.Head, want) }) {
				t.Errorf("work at width %d lost %q", width, want)
			}
		}
		work := ansi.Strip(app.View().Content)
		if !strings.Contains(work, "out   done") {
			t.Errorf("work at width %d does not draw a result whole\n%s", width, work)
		}
		assertGolden(t, chatWorkGoldenName("chat", width, height), chat)
		assertGolden(t, chatWorkGoldenName("work", width, height), work)
	}
}

func chatWorkGoldenName(view string, width, height int) string {
	return view + "-twelve-" + strconv.Itoa(width) + "x" + strconv.Itoa(height) + ".golden"
}

func TestEveryEventDrawsItsIDWithAHashInChatAndInWork(t *testing.T) {
	app := twelveCallTurn(t, 80, 24)
	chat := app.View().Content
	if strings.Count(chat, "#") == 0 {
		t.Fatal("chat draws no id at all")
	}
	app.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	work := app.View().Content
	if strings.Count(work, "#") == 0 {
		t.Fatal("work draws no id at all")
	}
}

func TestAnIDInChatIsTheSameIDAsTheEventInWork(t *testing.T) {
	app := twelveCallTurn(t, 80, 24)
	chatID, found := chatFoldID(app)
	if !found {
		t.Fatal("the fold line in chat carries no id")
	}
	if !app.work.JumpTo(strings.TrimPrefix(chatID, "#")) {
		t.Fatalf("work holds no event under the id chat shows: %s", chatID)
	}
}

func chatFoldID(app *App) (string, bool) {
	for _, line := range strings.Split(ansi.Strip(app.View().Content), "\n") {
		if !strings.Contains(line, "(12) tools") {
			continue
		}
		open := strings.LastIndex(line, "[#")
		shut := strings.LastIndex(line, "]")
		if open < 0 || shut <= open {
			return "", false
		}
		return line[open+1 : shut], true
	}
	return "", false
}

func TestAnIDCanBeActivatedAndMovesToItsEventInWork(t *testing.T) {
	app := twelveCallTurn(t, 80, 24)
	chatID, found := chatFoldID(app)
	if !found {
		t.Fatal("the fold line in chat carries no id")
	}
	typeText(app, chatID)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.current != viewWork {
		t.Fatal("activating an id did not move to work")
	}
	picked, ok := app.work.Picked()
	if !ok || !strings.HasPrefix(picked.ID, strings.TrimPrefix(chatID, "#")) {
		t.Fatalf("work did not land on the activated event: picked %+v", picked)
	}
}

func TestASettingsSwitchMovesToolCallsBetweenWorkAndChat(t *testing.T) {
	app := twelveCallTurn(t, 80, 24)
	folded := ansi.Strip(app.View().Content)
	if strings.Count(folded, "⟩ ") != 0 {
		t.Fatalf("the switch off already shows a row per tool call\n%s", folded)
	}
	if !strings.Contains(folded, "(12) tools") {
		t.Fatal("the switch off does not fold tool calls")
	}
	app.settings.ChatShowsTools = true
	unfolded := ansi.Strip(app.View().Content)
	if strings.Count(unfolded, "⟩ ") == 0 {
		t.Fatalf("the switch on did not move any tool call into chat\n%s", unfolded)
	}
}

func TestABlockInWorkCanBeBroughtIntoChatAsATruncatedReference(t *testing.T) {
	app := twelveCallTurn(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	app.Update(tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})
	chat := ansi.Strip(app.View().Content)
	if !strings.Contains(chat, "glob") && !strings.Contains(chat, "read") && !strings.Contains(chat, "bash") {
		t.Fatalf("chat did not receive the brought-in reference\n%s", chat)
	}
	assertGolden(t, "chat-with-reference-80x24.golden", app.View().Content)
}

func TestTheFrameBudgetHoldsWithWorkOpenOnAFullTurn(t *testing.T) {
	app := twelveCallTurn(t, 120, 36)
	app.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	frametime.Frames(t, "work open over a twelve call turn at 120x36", func() { app.View() })
}

func TestWorkTakesNoTextInput(t *testing.T) {
	app := twelveCallTurn(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	before := app.View().Content
	typeText(app, "this should not go anywhere")
	after := app.View().Content
	if before != after {
		t.Fatalf("work changed on a typed character\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}
