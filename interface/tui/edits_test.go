package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/edits"
)

const (
	gatePath  = "internal/judge/policy/toolgate.go"
	rulesPath = "internal/rule/check.go"
	docsPath  = "docs/verification.md"
	gateDiff  = "--- internal/judge/policy/toolgate.go\n" +
		"+++ internal/judge/policy/toolgate.go\n" +
		"@@ -40,6 +40,7 @@\n" +
		" func Decide(answers Answers) Verdict {\n" +
		"\tif answers.Risk > thresholds.RiskAskAt {\n" +
		"-\t\treturn Ask\n" +
		"+\t\treturn AskWithReason(answers)\n" +
		"\t}\n" +
		"\treturn Allow\n" +
		"}\n"
	rulesDiff = "--- internal/rule/check.go\n" +
		"+++ internal/rule/check.go\n" +
		"@@ -12,3 +12,4 @@\n" +
		" func Check(findings []Finding) error {\n" +
		"+\treturn nil\n" +
		"}\n"
	docsDiff = "--- docs/verification.md\n" +
		"+++ docs/verification.md\n" +
		"@@ -8,2 +8,2 @@\n" +
		"-a skip is silent\n" +
		"+a skip is a result and it is named\n"
)

func edited(app *App, id, agent, path, diff string) {
	app.Update(Event{Kind: EventToolCall, ID: id, Tool: "edit", Text: path})
	app.Update(Event{Kind: EventToolResult, ID: id, Agent: agent, Text: "9 lines, 210 bytes", Diff: diff})
}

func createdFile(app *App, id, agent, path, content string) {
	app.Update(Event{Kind: EventToolCall, ID: id, Tool: "write", Text: path})
	app.Update(Event{Kind: EventToolResult, ID: id, Agent: agent, Text: "created " + path, Created: content})
}

func numberedLines(count int) string {
	var content strings.Builder
	for line := 1; line <= count; line++ {
		content.WriteString("line " + strconv.Itoa(line) + "\n")
	}
	return content.String()
}

func TestACreatedFileDrawsEveryLineItHoldsAndAgreesWithItsTally(t *testing.T) {
	app := sessionApp(t, 80, 40)
	createdFile(app, "w1", "", "catalog/README.md", numberedLines(16))
	app.Update(tea.KeyPressMsg{Code: '3', Mod: tea.ModAlt})
	feed := ansi.Strip(app.View().Content)
	drawn := 0
	for _, row := range strings.Split(feed, "\n") {
		if _, body, sided := strings.Cut(row, " │ "); sided && strings.HasPrefix(strings.TrimSpace(body), "+line ") {
			drawn++
		}
	}
	if drawn != 16 {
		t.Errorf("a sixteen line file drew %d added lines\n%s", drawn, feed)
	}
	for _, want := range []string{"+16 -0", "+line 1", "+line 16"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the feed does not show %q\n%s", want, feed)
		}
	}
}

func TestAFileTooLargeToDrawWholeSaysHowManyLinesItHas(t *testing.T) {
	const written = 900
	app := sessionApp(t, 80, 40)
	createdFile(app, "w1", "", "internal/konst/konst.go", numberedLines(written))
	app.Update(tea.KeyPressMsg{Code: '3', Mod: tea.ModAlt})
	feed := ansi.Strip(app.View().Content)
	for _, want := range []string{"+" + strconv.Itoa(written) + " -0", "900 lines in all, the first 400 drawn"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the feed does not show %q for a file too large to draw whole\n%s", want, feed)
		}
	}
}

func TestEveryRowNamesTheAgentTheVerbAndThePath(t *testing.T) {
	app := sessionApp(t, 100, 40)
	edited(app, "e1", "go-dev", gatePath, gateDiff)
	createdFile(app, "w1", "", "catalog/README.md", "one\ntwo\n")
	app.Update(tea.KeyPressMsg{Code: '3', Mod: tea.ModAlt})
	feed := ansi.Strip(app.View().Content)
	for _, want := range []string{"go-dev edited " + gatePath, "tofu created catalog/README.md"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the feed does not name who did what with %q\n%s", want, feed)
		}
	}
}

func sidebarCells(content string) (drawn, needed int) {
	for _, row := range strings.Split(ansi.Strip(content), "\n") {
		cut := strings.Index(row, " │ ")
		if cut < 0 {
			continue
		}
		drawn = ansi.StringWidth(row[:cut])
		needed = max(needed, ansi.StringWidth(strings.TrimRight(row[:cut], " ")))
	}
	return drawn, needed
}

func TestTheSidebarIsNoWiderThanItsLongestRow(t *testing.T) {
	widths := map[int]int{}
	for _, width := range []int{80, 120} {
		content := editsApp(t, width, 24).View().Content
		drawn, needed := sidebarCells(content)
		if drawn != needed {
			t.Errorf("at %d columns the sidebar is %d cells wide and its longest row with its count is %d\n%s",
				width, drawn, needed, ansi.Strip(content))
		}
		widths[width] = drawn
	}
	if widths[80] != widths[120] {
		t.Errorf("the sidebar took %d cells at 80 columns and %d at 120, so it follows the screen rather than its content",
			widths[80], widths[120])
	}
}

func TestTheFirstSidebarRowReadsAsAFeed(t *testing.T) {
	rows := strings.Split(ansi.Strip(editsApp(t, 80, 24).View().Content), "\n")
	feed, self := -1, -1
	for index, row := range rows {
		side, _, sided := strings.Cut(row, " │ ")
		switch {
		case !sided:
		case strings.Contains(side, "feed") && feed < 0:
			feed = index
		case strings.Contains(side, edits.Self) && self < 0:
			self = index
		}
	}
	if feed < 0 || self < 0 || feed > self {
		t.Errorf("the sidebar does not open on a feed row above the turn itself\n%s", strings.Join(rows, "\n"))
	}
}

func editsApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := sessionApp(t, width, height)
	app.Update(Event{Kind: EventCrew, Children: crewChildren()})
	edited(app, "e1", "", gatePath, gateDiff)
	edited(app, "e2", "go-dev", rulesPath, rulesDiff)
	edited(app, "e3", "go-docs", docsPath, docsDiff)
	app.Update(tea.KeyPressMsg{Code: '3', Mod: tea.ModAlt})
	if app.current != viewEdits {
		t.Fatalf("alt+3 left the app on view %d, want the file edits view", app.current)
	}
	return app
}

func TestAnEditLeavesItsDiffOutOfTheSessionAndPutsItInTheEditsView(t *testing.T) {
	app := sessionApp(t, 80, 24)
	edited(app, "e1", "", gatePath, gateDiff)
	transcript := ansi.Strip(app.View().Content)
	if strings.Contains(transcript, "⟩ edit "+gatePath) {
		t.Errorf("chat drew the call that made the edit, it belongs in work\n%s", transcript)
	}
	for _, body := range []string{"@@ -40,6", "return AskWithReason", "func Decide"} {
		if strings.Contains(transcript, body) {
			t.Errorf("the session drew the diff line %q\n%s", body, transcript)
		}
	}
	app.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	worked := ansi.Strip(app.View().Content)
	if !strings.Contains(worked, "edit "+gatePath) {
		t.Errorf("work does not show the call that made the edit\n%s", worked)
	}
	if !strings.Contains(worked, "+1 -1") {
		t.Errorf("work does not say how much the file changed\n%s", worked)
	}

	app.Update(tea.KeyPressMsg{Code: '3', Mod: tea.ModAlt})
	feed := ansi.Strip(app.View().Content)
	for _, want := range []string{gatePath, "+1 -1", "@@ -40,6 +40,7 @@", "-        return Ask", "+        return AskWithReason(answers)"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the file edits view does not show %q\n%s", want, feed)
		}
	}
	if strings.Contains(feed, "--- "+gatePath) {
		t.Errorf("the feed repeats the diff header under the path it already names\n%s", feed)
	}
}

func TestTheEditsSidebarSeparatesRunningAgentsFromFinishedOnes(t *testing.T) {
	app := editsApp(t, 80, 24)
	content := ansi.Strip(app.View().Content)
	running := strings.Index(content, "● go-dev")
	finished := strings.Index(content, "✓ go-docs")
	if running < 0 || finished < 0 {
		t.Fatalf("the sidebar does not list a running agent and a finished one\n%s", content)
	}
	if running > finished {
		t.Errorf("the finished agent sits above the running one\n%s", content)
	}
	if !strings.Contains(content, edits.Self) {
		t.Errorf("the sidebar does not list the turn itself as an agent\n%s", content)
	}
	assertGolden(t, "edits-80x24.golden", app.View().Content)
}

func TestPickingAnAgentFiltersTheFeedToItsEdits(t *testing.T) {
	app := editsApp(t, 120, 36)
	every := ansi.Strip(app.View().Content)
	for _, want := range []string{gatePath, rulesPath, docsPath} {
		if !strings.Contains(every, want) {
			t.Fatalf("the unfiltered feed does not hold %q\n%s", want, every)
		}
	}
	for range 2 {
		app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	picked := ansi.Strip(app.View().Content)
	if !strings.Contains(picked, rulesPath) {
		t.Errorf("picking go-dev hid the file it changed\n%s", picked)
	}
	for _, gone := range []string{gatePath, docsPath} {
		if strings.Contains(picked, gone) {
			t.Errorf("picking go-dev left %q in the feed\n%s", gone, picked)
		}
	}
	assertGolden(t, "edits-picked-120x36.golden", app.View().Content)
}

func TestATurnThatEditsNothingSaysSoRatherThanDrawingAnEmptyFrame(t *testing.T) {
	app := sessionApp(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: '3', Mod: tea.ModAlt})
	content := ansi.Strip(app.View().Content)
	if !strings.Contains(content, "no file has changed") {
		t.Fatalf("the empty file edits view does not say there is nothing to see\n%s", content)
	}
	if strings.Contains(content, " │ ") {
		t.Fatalf("the empty file edits view drew a frame instead of saying so\n%s", content)
	}
	assertGolden(t, "edits-empty-80x24.golden", app.View().Content)
}

const measuredEdits = 30

func editTurnRows(t *testing.T, inTheStream bool) int {
	t.Helper()
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: bothWires})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 600})
	app.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	for step := range measuredEdits {
		id := "e" + strconv.Itoa(step)
		app.Update(Event{Kind: EventText, Text: "editing " + gatePath})
		call := Event{Kind: EventToolCall, ID: id, Tool: "edit", Text: gatePath}
		result := Event{Kind: EventToolResult, ID: id, Text: "9 lines, 210 bytes"}
		if inTheStream {
			call.Detail = gateDiff
		} else {
			result.Diff = gateDiff
		}
		app.Update(call)
		app.Update(result)
	}
	rows := 0
	for _, line := range strings.Split(ansi.Strip(app.View().Content), "\n") {
		if strings.TrimSpace(line) != "" {
			rows++
		}
	}
	return rows
}

func TestAThirtyEditTurnNoLongerScrollsTheSessionView(t *testing.T) {
	inTheStream := editTurnRows(t, true)
	elsewhere := editTurnRows(t, false)
	t.Logf("a %d edit turn fills %d rows with the diff in the session view and %d with the diff in the file edits view",
		measuredEdits, inTheStream, elsewhere)
	if elsewhere >= inTheStream/2 {
		t.Errorf("moving the diff out of the session view saved %d rows of %d", inTheStream-elsewhere, inTheStream)
	}
}
