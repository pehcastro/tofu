package settings

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/golden"
	isettings "tofu/internal/settings"
)

const themeSource = "global C:/Users/tester/.tofu/settings.json"

func tableModel(width, height int) Model {
	var rows []Row
	for _, spec := range isettings.Default() {
		row := Row{Key: spec.Key, Category: spec.Category, Label: spec.Label, Description: spec.Description, Value: spec.DefaultText, Source: "default", Kind: Kind(spec.Kind), Choices: spec.Choices, RestartRequired: spec.Restart}
		switch spec.Kind {
		case isettings.Bool:
			row.Value = strconv.FormatBool(spec.Default == 1)
		case isettings.Int:
			row.Value = strconv.Itoa(spec.Default)
		}
		if spec.Key == isettings.Theme {
			row.Value, row.Changed, row.Source = "tofu dusk", true, themeSource
		}
		rows = append(rows, row)
	}
	m := Model{Scopes: []string{"global", "project"}}
	m.SetRows(rows)
	m.SetSize(width, height)
	m.SetSearchKey("ctrl+k")
	m.SetBranch("develop")
	return m
}

func TestAppearanceWithTheInspectorNamesTheSourceFile(t *testing.T) {
	m := tableModel(120, 36)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "C:/Users/tester/.tofu/") {
		t.Fatalf("the inspector must name the file the value came from\n%s", view)
	}
	golden.Assert(t, "appearance-120x36.golden", view)
}

func TestTheChoiceDialogOnTheme(t *testing.T) {
	m := tableModel(120, 36)
	opened := m.Key("enter")
	previewed := m.Key("down")
	t.Logf("enter %+v, down %+v", opened, previewed)
	golden.Assert(t, "theme-choice-120x36.golden", ansi.Strip(m.View()))
}

func TestTheSearchDialogFilteredToDiff(t *testing.T) {
	m := tableModel(120, 36)
	for _, key := range []string{"ctrl+k", "d", "i", "f", "f"} {
		m.Key(key)
	}
	golden.Assert(t, "search-diff-120x36.golden", ansi.Strip(m.View()))
}

func TestStartupAt80x24ReachedByAClickOnItsLabel(t *testing.T) {
	m := tableModel(80, 24)
	t.Logf("before: category %q cursor %d", m.categories()[m.category], m.cursor)
	intent := m.Click(6, 11)
	t.Logf("click (6,11) intent %+v; after: category %q cursor %d", intent, m.categories()[m.category], m.cursor)
	golden.Assert(t, "startup-80x24.golden", ansi.Strip(m.View()))
}

func TestSettingsBodyCacheInvalidatesOnVisibleChanges(t *testing.T) {
	m := tableModel(120, 36)
	check := func(stage string) {
		t.Helper()
		if got, want := m.View(), m.render(); got != want {
			t.Fatalf("%s: cached settings differ from uncached render", stage)
		}
	}
	check("initial")
	m.Key("down")
	check("selection")
	m.Key("right")
	check("category")
	rows := append([]Row(nil), m.Rows...)
	rows[m.inCategory()[0]].Value = "compact"
	m.SetRows(rows)
	check("value")
	m.SetDensity(densitySpacious)
	check("density")
	m.SetSearchKey("alt+k")
	check("search key")
	m.Scope = 1
	check("scope")
	m.Key("enter")
	check("dialog")
	m.SetSize(80, 36)
	check("width")
}

func BenchmarkProgressedScreenRender(b *testing.B) {
	b.Run("settings", func(b *testing.B) {
		m := tableModel(120, 36)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_ = m.View()
		}
	})
}
