package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/session"
	"tofu/internal/golden"
)

func fourItems(states ...session.PlanState) []session.PlanItem {
	texts := []struct{ phase, text string }{
		{"read", "read the gate and the record shape"},
		{"write", "write the plan tool"},
		{"write", "draw the plan in the session view"},
		{"check", "measure the frame against the budget"},
	}
	items := make([]session.PlanItem, 0, len(texts))
	for index, one := range texts {
		items = append(items, session.PlanItem{Phase: one.phase, Text: one.text, State: states[index]})
	}
	return items
}

func TestAPlanNeverDrawsInTheChat(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	quiet := app.View().Content
	app.Update(Event{Kind: EventPlan, Plan: fourItems(session.PlanDone, session.PlanRunning, session.PlanPending, session.PlanPending)})
	if after := app.View().Content; after != quiet {
		t.Errorf("a plan changed the chat frame\n--- after ---\n%s\n--- before ---\n%s", ansi.Strip(after), ansi.Strip(quiet))
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
		})
	}
}
