package session

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
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
