package session

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/subagent"
	roster "tofu/internal/subagent"
	"tofu/internal/widget"
)

func withRunningChildren(children ...subagent.Child) Model {
	model := New(fixed(), spoken)
	model.SetSize(100, 30)
	model.Start()
	model.Children = children
	return model
}

func TestATruncatedOwnsColumnKeepsItsGapBeforeTheMission(t *testing.T) {
	model := withRunningChildren(subagent.Child{
		Name:  "c1",
		State: roster.Working,
		Owns:  []string{"internal/judge/policy/**"},
		Doing: "read the policy loader",
		Since: 26 * time.Second,
	})
	line := ansi.Strip(model.activityLines()[0])
	before, _, found := strings.Cut(line, "read the policy loader")
	if !found {
		t.Fatalf("the mission is missing from the row: %q", line)
	}
	if !strings.HasSuffix(before, gap) {
		t.Errorf("a truncated owns column runs into the mission: %q", line)
	}
}

func TestAnOverWideElapsedTimeDoesNotMoveTheColumnsAfterIt(t *testing.T) {
	model := withRunningChildren(
		subagent.Child{Name: "under", State: roster.Working, Doing: "read the policy loader", Since: 52 * time.Second},
		subagent.Child{Name: "over", State: roster.Working, Doing: "read the policy loader", Since: 78 * time.Second},
	)
	lines := model.activityLines()
	startsAt := func(line, name string) int {
		before, _, found := strings.Cut(ansi.Strip(line), name)
		if !found {
			t.Fatalf("the row for %s is missing its name: %q", name, line)
		}
		return widget.Cells(before)
	}
	under, over := startsAt(lines[0], "under"), startsAt(lines[1], "over")
	if under != over {
		t.Errorf("a child at 52s names itself at cell %d and one at 1m 18s at cell %d:\n%q\n%q",
			under, over, ansi.Strip(lines[0]), ansi.Strip(lines[1]))
	}
}
