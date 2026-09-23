package settings

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "write golden files instead of comparing to them")

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s does not match the golden file\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func baseRows() []Row {
	return []Row{
		{Key: "chatShowsTools", Group: "chat", Label: "chat shows every tool call", Value: "off", Kind: Bool, Source: "default"},
		{Key: "decisionCap", Group: "turn", Label: "decision cap per turn, zero means no cap", Value: "0", Kind: Int, Source: "default"},
	}
}

func TestARowThatDiffersFromItsDefaultIsMarked(t *testing.T) {
	rows := baseRows()
	rows[1].Value, rows[1].Changed, rows[1].Source = "12", true, "~/.tofu/settings.json"
	m := Model{Scopes: []string{"global", "project"}, Rows: rows}
	m.SetSize(80, 24)
	content := m.View()
	if !strings.Contains(content, "*") {
		t.Fatalf("a changed row must draw a marker\n%s", content)
	}
	assertGolden(t, "changed-row-80x24.golden", content)
}

func TestAProjectValueOverridesAGlobalOneAndTheViewNamesTheFile(t *testing.T) {
	rows := baseRows()
	rows[1].Value, rows[1].Changed, rows[1].Source = "12", true, "silo/.tofu/settings.json"
	m := Model{
		Providers: []Provider{{Name: "anthropic", State: "signed in"}},
		Scopes:    []string{"global", "project"},
		Scope:     1,
		Rows:      rows,
	}
	m.SetSize(120, 36)
	content := m.View()
	if !strings.Contains(content, "silo/.tofu/settings.json") {
		t.Fatalf("the view must name the project file the override came from\n%s", content)
	}
	assertGolden(t, "project-override-120x36.golden", content)
}

func TestARestartRequiredSettingWarnsOnlyAfterItChanges(t *testing.T) {
	rows := baseRows()
	m := Model{Scopes: []string{"global", "project"}, Rows: rows}
	m.SetSize(120, 36)
	before := m.View()
	if strings.Contains(before, "needs restart") {
		t.Fatalf("nothing changed yet, so no warning is expected\n%s", before)
	}
	assertGolden(t, "restart-before-120x36.golden", before)

	rows[1].Value, rows[1].Changed, rows[1].RestartPending = "5", true, true
	m.SetRows(rows)
	after := m.View()
	if !strings.Contains(after, "needs restart") {
		t.Fatalf("decisionCap needs a restart once changed, and the view must say so\n%s", after)
	}
	assertGolden(t, "restart-after-120x36.golden", after)
}

func TestTypingFiltersTheListAndTheMatchCountDraws(t *testing.T) {
	m := Model{Scopes: []string{"global", "project"}, Rows: baseRows()}
	m.SetSize(120, 36)
	for _, r := range "decision" {
		m.Key(string(r))
	}
	if m.Query != "decision" {
		t.Fatalf("Query = %q, want decision", m.Query)
	}
	m.SetRows(baseRows()[1:])
	content := m.View()
	if !strings.Contains(content, "1 match") {
		t.Fatalf("a single match must say 1 match\n%s", content)
	}
	assertGolden(t, "search-120x36.golden", content)
}

func TestProjectOverrideWithASearchInProgress(t *testing.T) {
	rows := baseRows()
	rows[1].Value, rows[1].Changed, rows[1].Source = "12", true, "silo/.tofu/settings.json"
	m := Model{
		Providers: []Provider{{Name: "anthropic", State: "signed in"}},
		Scopes:    []string{"global", "project"},
		Scope:     1,
		Rows:      rows,
		Query:     "decision",
	}
	m.SetSize(120, 36)
	m.SetRows(rows[1:])
	content := m.View()
	assertGolden(t, "project-override-with-search-120x36.golden", content)
}

func TestTheTurnMaySpawnSettingDrawsItsLabel(t *testing.T) {
	rows := append(baseRows(), Row{Key: "turnMaySpawn", Group: "turn", Label: "a turn may spawn a sub-agent", Value: "true", Kind: Bool, Source: "default"})
	m := Model{Scopes: []string{"global", "project"}, Rows: rows}
	m.SetSize(120, 36)
	content := m.View()
	if !strings.Contains(content, "a turn may spawn a sub-agent") {
		t.Fatalf("the settings pane never draws the turnMaySpawn label\n%s", content)
	}
}

func TestBackspaceShortensTheQuery(t *testing.T) {
	m := Model{Rows: baseRows()}
	m.Key("a")
	m.Key("b")
	m.Key("backspace")
	if m.Query != "a" {
		t.Fatalf("Query after backspace = %q, want a", m.Query)
	}
	m.Key("backspace")
	m.Key("backspace")
	if m.Query != "" {
		t.Fatalf("backspace on an empty query must not panic or go negative, Query = %q", m.Query)
	}
}

func TestKeyReturnsTheIntentForTheActiveRow(t *testing.T) {
	m := Model{Rows: baseRows()}
	m.moveCursor(1)
	if got := m.Key("enter"); got.Action != ActionToggle || got.Key != "chatShowsTools" {
		t.Fatalf("enter on the bool row = %+v, want ActionToggle chatShowsTools", got)
	}
	m.moveCursor(1)
	if got := m.Key("right"); got.Action != ActionIncrement || got.Key != "decisionCap" {
		t.Fatalf("right on the int row = %+v, want ActionIncrement decisionCap", got)
	}
	m.moveCursor(-2)
	if got := m.Key("right"); got.Action != ActionCycleScope {
		t.Fatalf("right on the scope row = %+v, want ActionCycleScope", got)
	}
}
