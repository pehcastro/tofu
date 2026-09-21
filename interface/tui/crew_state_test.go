package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/crew"
)

func crewStateApp(t *testing.T, state crew.State) *App {
	t.Helper()
	app := sessionApp(t, 80, 24)
	app.Update(Event{Kind: EventCrew, Children: []crew.Child{
		{Name: "go-dev", Owns: []string{"internal/judge/**"}, Doing: "at work", State: state},
	}})
	app.Update(tea.KeyPressMsg{Code: '4', Mod: tea.ModAlt})
	return app
}

func TestASubAgentCarriesAStateFromTheNamedSetAndTheCrewViewDrawsIt(t *testing.T) {
	for _, state := range crew.AllStates() {
		t.Run(state.Label(), func(t *testing.T) {
			content := crewStateApp(t, state).View().Content
			assertGolden(t, "crew-state-"+strings.ReplaceAll(state.Label(), " ", "-")+"-80x24.golden", content)
		})
	}
}

func TestSubAgentsCarriesItsExactCountInBracketsOrPlainWithNone(t *testing.T) {
	app := sessionApp(t, 80, 24)
	if strings.Contains(app.strip.Render(app.width), "sub-agents (") {
		t.Fatalf("sub-agents carries a count with no children\n%s", app.strip.Render(app.width))
	}
	app.Update(Event{Kind: EventCrew, Children: []crew.Child{
		{Name: "a", State: crew.Running},
		{Name: "b", State: crew.Running},
		{Name: "c", State: crew.Done},
	}})
	strip := app.strip.Render(app.width)
	if !strings.Contains(strip, "sub-agents (2)") {
		t.Fatalf("sub-agents does not carry its exact running count\n%s", strip)
	}
}
