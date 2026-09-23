package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/subagent"
)

func subAgentStateApp(t *testing.T, state subagent.State) *App {
	t.Helper()
	app := sessionApp(t, 80, 24)
	app.Update(Event{Kind: EventSubAgent, Children: []subagent.Child{
		{Name: "go-dev", Owns: []string{"internal/judge/**"}, Doing: "at work", State: state},
	}})
	app.Update(tea.KeyPressMsg{Code: '4', Mod: tea.ModAlt})
	return app
}

func TestASubAgentCarriesAStateFromTheNamedSetAndTheSubAgentViewDrawsIt(t *testing.T) {
	for _, state := range subagent.AllStates() {
		t.Run(state.Label(), func(t *testing.T) {
			content := subAgentStateApp(t, state).View().Content
			assertGolden(t, "subagent-state-"+strings.ReplaceAll(state.Label(), " ", "-")+"-80x24.golden", content)
		})
	}
}

func TestSubAgentsCarriesItsExactCountInBracketsOrPlainWithNone(t *testing.T) {
	app := sessionApp(t, 80, 24)
	if strings.Contains(app.strip.Render(app.width), "sub-agents (") {
		t.Fatalf("sub-agents carries a count with no children\n%s", app.strip.Render(app.width))
	}
	app.Update(Event{Kind: EventSubAgent, Children: []subagent.Child{
		{Name: "a", State: subagent.Running},
		{Name: "b", State: subagent.Running},
		{Name: "c", State: subagent.Done},
	}})
	strip := app.strip.Render(app.width)
	if !strings.Contains(strip, "sub-agents (2)") {
		t.Fatalf("sub-agents does not carry its exact running count\n%s", strip)
	}
}
