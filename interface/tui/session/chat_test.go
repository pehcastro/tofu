package session

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/subagent"
	"tofu/internal/host"
	roster "tofu/internal/subagent"
)

func backgroundOf(c look.Color) string {
	escape, _, _ := strings.Cut(lipgloss.NewStyle().Background(lipgloss.Color(string(c))).Render("X"), "X")
	return escape
}

func TestRequestStatusSitsImmediatelyAboveComposer(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(120, 36)
	model.Focus()
	model.Append(Entry{Kind: User, Body: "hey"})
	model.Start()
	model.Append(Entry{Kind: Assistant, ID: "a1b2c3", Body: "hello"})
	model.Close("cooked for", "e6ec8c")
	model.Stop()
	lines := strings.Split(ansi.Strip(model.View()), "\n")
	inputRow, requestRow := -1, -1
	for y, line := range lines {
		if strings.Contains(line, "Ask tofu to build") {
			inputRow = y
		}
		if strings.Contains(line, "cooked for") && strings.Contains(line, "[request#e6ec8c]") {
			requestRow = y
		}
	}
	if inputRow-requestRow != 3 {
		t.Fatalf("request needs one breathing row before input: request=%d input=%d\n%s", requestRow, inputRow, strings.Join(lines, "\n"))
	}
}

func TestOnlyLatestUserMessageIsTintedAndColumnsAlign(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(100, 40)
	model.Append(Entry{Kind: User, Body: "the older question"})
	model.Append(Entry{Kind: Assistant, ID: "a1", Body: "the first answer"})
	model.Append(Entry{Kind: User, Body: "the latest question"})
	tint := backgroundOf(look.Panel)
	rows := strings.Split(model.View(), "\n")
	old := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, "the older question") })
	latest := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, "the latest question") })
	if old < 1 || latest < 1 {
		t.Fatalf("both questions are not drawn\n%s", ansi.Strip(model.View()))
	}
	if strings.Contains(rows[old], tint) || strings.Contains(rows[old-1], tint) {
		t.Fatal("old user message still tinted")
	}
	if !strings.Contains(rows[latest], tint) || !strings.Contains(rows[latest-1], tint) {
		t.Fatal("latest user message lacks subdued tint")
	}
	plain := strings.Split(ansi.Strip(model.View()), "\n")
	you := slices.IndexFunc(plain, func(row string) bool { return strings.Contains(row, "You") })
	agent := slices.IndexFunc(plain, func(row string) bool { return strings.Contains(row, "[&orchestrator]") })
	if you < 0 || agent < 0 || strings.Index(plain[you], "You") != strings.Index(plain[agent], "[&orchestrator]") {
		t.Fatalf("message columns differ\n%s", strings.Join(plain, "\n"))
	}
}

func composerRowCount(frame string) int {
	tint := backgroundOf(look.PanelLight)
	count := 0
	for _, row := range strings.Split(frame, "\n") {
		if strings.Contains(row, tint) {
			count++
		}
	}
	return count
}

func TestChatComposerStartsCompactAndGrowsForMultiline(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.Focus()
	initial := composerRowCount(model.View())
	if initial < 3 || initial > 4 {
		t.Fatalf("initial composer should be two input rows plus metadata, got %d\n%s", initial, ansi.Strip(model.View()))
	}
	for range 7 {
		model.Insert("A line of longer input that wraps in the composer.")
		model.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	}
	grown := composerRowCount(model.View())
	if grown <= initial || grown > 9 {
		t.Fatalf("multiline composer height %d did not grow from %d within the 8-row cap", grown, initial)
	}
}

func TestThreeRunningSubAgentsShareOneBatchLineAndLeaveTheStatusLineToTheLead(t *testing.T) {
	at := time.Date(2026, 9, 27, 14, 32, 0, 0, time.UTC)
	model := New(func() time.Time { return at }, counted(new(int)))
	model.SetSize(100, 30)
	model.Append(Entry{Kind: User, Body: "split the routes"})
	model.Start()
	model.Returned()
	model.Spawned("ts-dev-1")
	model.Spawned("ts-dev-2")
	model.Spawned("ts-dev-3")
	model.SubAgents = []subagent.Row{
		{Name: "ts-dev-1", State: roster.Working, Since: 51 * time.Second, Calls: []subagent.Call{{ID: "r1", Tool: "read", Text: "src/routes/products.ts"}}},
		{Name: "ts-dev-2", State: roster.Working, Since: 3 * time.Minute, Calls: []subagent.Call{{ID: "r2", Tool: "edit", Text: "src/routes/orders.ts"}}},
		{Name: "ts-dev-3", State: roster.Working, Since: 2 * time.Minute, Calls: []subagent.Call{{ID: "r3", Tool: "read", Text: "src/routes/users.ts"}}},
	}
	at = at.Add(time.Minute)
	model.View()
	rows := strings.Split(ansi.Strip(model.View()), "\n")
	composer := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, Placeholder) })
	var above []string
	for _, row := range rows[model.transcriptRows():max(composer, 0)] {
		if strings.TrimSpace(row) != "" {
			above = append(above, strings.TrimSpace(row))
		}
	}
	if len(above) != 1 || !strings.Contains(above[0], "thinking") || strings.Contains(above[0], "sub-agent") || strings.Contains(above[0], "[&") {
		t.Fatalf("the rows above the composer are %q, want one status line saying what the lead does and nothing of its sub-agents\n%s", above, strings.Join(rows, "\n"))
	}
	spawn := slices.IndexFunc(rows[:model.transcriptRows()], func(row string) bool {
		return strings.Contains(row, "running [&ts-dev-1] [&ts-dev-2] [&ts-dev-3]")
	})
	if spawn < 0 || !strings.HasPrefix(strings.TrimSpace(rows[spawn]), "⠁ ") || !strings.HasSuffix(strings.TrimSpace(rows[spawn]), "3m") || strings.Contains(rows[spawn], "products.ts") {
		t.Errorf("the chat does not spin one Dots8 line naming all three sub-agents under the longest clock\n%s", strings.Join(rows, "\n"))
	}
}

func TestASubAgentSentBackByItsGateKeepsItsOneChatLineAndSaysWhy(t *testing.T) {
	at := time.Date(2026, 10, 2, 17, 5, 43, 0, time.UTC)
	model := New(func() time.Time { return at }, counted(new(int)))
	model.SetSize(100, 30)
	model.Append(Entry{Kind: User, Body: "move the lab link"})
	model.Start()
	model.Returned()
	model.Spawned("ts-dev-1")
	held := roster.SubAgent{ID: "ts-dev-1", Agent: "ts-dev", State: roster.Working, Round: 1, Started: at, Calling: []string{"edit"}}
	named := func() []string {
		model.SubAgents = host.SubAgentRows([]roster.SubAgent{held}, at, 0, nil, nil)
		var lines []string
		for _, row := range strings.Split(ansi.Strip(model.View()), "\n") {
			if strings.Contains(row, "ts-dev-1") {
				lines = append(lines, strings.TrimSpace(row))
			}
		}
		return lines
	}
	first := named()
	held.Round, held.Report = 2, "sent back: test did not run"
	at = at.Add(31 * time.Second)
	second := named()
	if len(first) != 1 || len(second) != 1 || !strings.Contains(second[0], "[&ts-dev-1] sent back: test did not run") || strings.Contains(second[0], "-r2") {
		t.Fatalf("round one draws %q and round two %q, want one line for ts-dev-1 each, the second saying it was sent back and why", first, second)
	}
	t.Logf("round one: %s\nround two: %s", first[0], second[0])
}

func TestSettledSubAgentNameIsDrawnInADifferentColourFromARunningOne(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(100, 30)
	model.Append(Entry{Kind: User, Body: "split the routes"})
	model.Start()
	model.Spawned("ts-dev-1")
	model.Spawned("ts-dev-2")
	model.SubAgents = []subagent.Row{
		{Name: "ts-dev-1", State: roster.Working, Calls: []subagent.Call{{ID: "r1", Tool: "read", Text: "a.ts"}}},
		{Name: "ts-dev-2", State: roster.Finished},
	}
	frame := model.View()
	colourBefore := func(name string) string {
		at := strings.Index(frame, name)
		if at < 0 {
			t.Fatalf("%s is not drawn\n%s", name, ansi.Strip(frame))
		}
		escape := frame[strings.LastIndex(frame[:at], "\x1b["):at]
		if !strings.Contains(escape, "38;") {
			t.Fatalf("%s has no foreground colour before it: %q", name, escape)
		}
		return escape
	}
	if running, settled := colourBefore("[&ts-dev-1]"), colourBefore("[&ts-dev-2]"); running == settled {
		t.Fatalf("the settled name is drawn as %q, the same as the running name", settled)
	}
}
