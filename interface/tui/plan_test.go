package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/frametime"
	"tofu/interface/tui/golden"
	"tofu/interface/tui/session"
)

const (
	planTranscript = 400
	planMarkers    = "◇◆✓"
	spinFrames     = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
)

var planLabels = []string{"read", "write", "check"}

func fourItems(states ...session.PlanState) []session.PlanItem {
	texts := []struct{ phase, text string }{
		{planLabels[0], "read the gate and the record shape"},
		{planLabels[1], "write the plan tool"},
		{planLabels[1], "draw the plan in the session view"},
		{planLabels[2], "measure the frame against the budget"},
	}
	items := make([]session.PlanItem, 0, len(texts))
	for index, one := range texts {
		items = append(items, session.PlanItem{Phase: one.phase, Text: one.text, State: states[index]})
	}
	return items
}

func fillTranscript(app *App, calls int) {
	for step := range calls {
		call := "p" + strconv.Itoa(step)
		app.Update(Event{Kind: EventToolCall, ID: call, Tool: "read", Text: "internal/turn/loop.go line " + strconv.Itoa(step)})
		app.Update(Event{Kind: EventToolResult, ID: call, Text: "9 lines, 210 bytes"})
	}
}

func planRowsOf(rows []string) []int {
	found := make([]int, 0, len(rows))
	for index, row := range rows {
		if strings.ContainsAny(row, planMarkers) {
			found = append(found, index)
		}
	}
	return found
}

func TestATurnWithNoPlanDrawsNothing(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	quiet := app.View().Content

	app.Update(Event{Kind: EventPlan, Plan: nil})
	if after := app.View().Content; after != quiet {
		t.Errorf("an empty plan changed the frame\n--- after ---\n%s\n--- before ---\n%s", after, quiet)
	}
	if found := planRowsOf(plainRows(app.View())); len(found) != 0 {
		t.Errorf("a turn with no plan drew plan rows %v\n%s", found, ansi.Strip(quiet))
	}
}

func TestAPlanSitsDirectlyAboveTheRunningRowAndMovesNothingBelowIt(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	fillTranscript(app, planTranscript)
	before := plainRows(app.View())

	app.Update(Event{Kind: EventPlan, Plan: fourItems(session.PlanDone, session.PlanRunning, session.PlanPending, session.PlanPending)})
	after := plainRows(app.View())

	if len(before) != len(after) {
		t.Fatalf("the plan changed the frame from %d rows to %d", len(before), len(after))
	}
	drawn := planRowsOf(after)
	if len(drawn) != 4 {
		t.Fatalf("the plan drew %d item rows, want 4\n%s", len(drawn), strings.Join(after, "\n"))
	}
	block := len(planLabels) + len(drawn)
	last := drawn[len(drawn)-1]
	top := last - block + 1
	for _, label := range planLabels {
		if !slices.ContainsFunc(after[top:last+1], func(row string) bool { return strings.TrimSpace(row) == label }) {
			t.Fatalf("the %d rows above the last item do not hold the phase %q\n%s", block, label, strings.Join(after[top:last+1], "\n"))
		}
	}
	if strings.TrimSpace(after[last+1]) != "" {
		t.Fatalf("the row under the plan is %q, want the blank line above the activity block\n%s", after[last+1], strings.Join(after, "\n"))
	}
	if !strings.ContainsAny(after[last+2], spinFrames) {
		t.Fatalf("the row under the plan's gap is %q, want the running row\n%s", after[last+2], strings.Join(after, "\n"))
	}

	hintRow := -1
	for row, line := range after {
		if strings.Contains(line, "⏎ send") {
			hintRow = row
		}
	}
	changed := make([]int, 0, len(after))
	for row := range before {
		if row != hintRow && before[row] != after[row] {
			changed = append(changed, row)
		}
	}
	if len(changed) == 0 || changed[len(changed)-1] != last {
		t.Fatalf("the rows the plan changed are %v, want them to end at its last item on row %d", changed, last)
	}
	t.Logf("the plan holds rows %d to %d, %d rows changed and every row from the running row down stayed where it was",
		top, last, len(changed))
}

func longPlan(finished int, items int) []session.PlanItem {
	plan := make([]session.PlanItem, 0, items)
	for index := range items {
		state := session.PlanPending
		if index < finished {
			state = session.PlanDone
		}
		plan = append(plan, session.PlanItem{Text: "step " + strconv.Itoa(index), State: state})
	}
	return plan
}

func TestAPlanTooLongForItsRoomCollapsesWhatIsFinished(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	app.Update(Event{Kind: EventPlan, Plan: longPlan(6, 12)})
	rows := plainRows(app.View())
	if drawn := planRowsOf(rows); len(drawn) != 6 {
		t.Fatalf("a twelve item plan drew %d item rows, want the six unfinished ones\n%s", len(drawn), strings.Join(rows, "\n"))
	}
	if !strings.Contains(strings.Join(rows, "\n"), "6 finished") {
		t.Fatalf("the collapsed plan does not count what is finished\n%s", strings.Join(rows, "\n"))
	}

	app.Update(Event{Kind: EventPlan, Plan: longPlan(0, 12)})
	rows = plainRows(app.View())
	drawn := planRowsOf(rows)
	if len(drawn) != 7 {
		t.Fatalf("a twelve item plan with nothing finished drew %d item rows, want 7\n%s", len(drawn), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[drawn[len(drawn)-1]+1], "5 more") {
		t.Fatalf("the trimmed plan does not say how many it is holding back\n%s", strings.Join(rows, "\n"))
	}
}

func TestThePlanDrawsInThreeStatesAtEightyColumns(t *testing.T) {
	for _, state := range []struct {
		name   string
		states []session.PlanState
	}{
		{"session-plan-fresh-80x24.golden", []session.PlanState{
			session.PlanPending, session.PlanPending, session.PlanPending, session.PlanPending}},
		{"session-plan-running-80x24.golden", []session.PlanState{
			session.PlanDone, session.PlanRunning, session.PlanPending, session.PlanPending}},
		{"session-plan-done-80x24.golden", []session.PlanState{
			session.PlanDone, session.PlanDone, session.PlanDone, session.PlanDone}},
	} {
		t.Run(state.name, func(t *testing.T) {
			at := fixedStart()
			app := liveApp(t, &at)
			app.Update(Event{Kind: EventPlan, Plan: fourItems(state.states...)})
			content := app.View().Content
			golden.Assert(t, state.name, content)
			for _, row := range strings.Split(ansi.Strip(content), "\n") {
				if cells := ansi.StringWidth(row); cells > 80 {
					t.Errorf("a row is %d cells wide\n%s", cells, row)
				}
			}
			t.Logf("%s\n%s", state.name, ansi.Strip(content))
		})
	}
}

func TestTheSessionViewWithAPlanRendersInsideTheFrameBudget(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	fillTranscript(app, planTranscript)
	app.Update(Event{Kind: EventPlan, Plan: fourItems(session.PlanDone, session.PlanRunning, session.PlanPending, session.PlanPending)})
	if len(planRowsOf(plainRows(app.View()))) != 4 {
		t.Fatal("the frame being measured does not carry the plan")
	}

	frametime.Frames(t, "a plan and "+strconv.Itoa(planTranscript)+" calls at 120x36", func() { app.View() })
}
