package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTheShellsTabCountsRunningShellsTheWayTheSubAgentsTabCountsSubAgents(t *testing.T) {
	app := sessionApp(t, 120, 36)
	top := strings.Split(ansi.Strip(app.View().Content), "\n")[0]
	if strings.Contains(top, "shells (") {
		t.Fatalf("with no shell the menu reads %q, want plain shells", top)
	}
	app.Update(shellsMsg(shellEntries()))
	top = strings.Split(ansi.Strip(app.View().Content), "\n")[0]
	if !strings.Contains(top, "shells (1)") {
		t.Fatalf("with one running shell and two exited the menu reads %q, want shells (1)", top)
	}
}
