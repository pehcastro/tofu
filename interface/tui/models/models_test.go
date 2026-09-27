package models

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/golden"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
)

const (
	shippedRoot = "../../../library"
	claudeSub   = library.Subscription("claude-sub")
	codexSub    = library.Subscription("codex-sub")
)

func shippedLibrary(t *testing.T) library.Library {
	t.Helper()
	layer := library.Layer{Name: "library", Origin: "library", FS: os.DirFS(shippedRoot)}
	loaded, err := library.Load([]library.Layer{layer})
	if err != nil {
		t.Fatalf("loading the shipped library: %v", err)
	}
	return loaded
}

func sourceEfforts(id library.Subscription) []llm.Effort {
	if id == claudeSub {
		return []llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh, llm.EffortXHigh, llm.EffortMax}
	}
	return llm.Efforts()
}

func everySource(loaded library.Library) []Source {
	sources := make([]Source, 0, len(loaded.Subscriptions))
	for _, spec := range loaded.Subscriptions {
		sources = append(sources, Source{ID: spec.ID, Efforts: sourceEfforts(spec.ID)})
	}
	return sources
}

func testLibrary() library.Library {
	windows := []string{"5h", "weekly"}
	loaded := library.Library{Subscriptions: []library.SubscriptionSpec{
		{ID: claudeSub, Provider: library.Anthropic, Wire: "claude", Windows: windows},
		{ID: codexSub, Provider: library.OpenAI, Wire: "codex", Windows: windows},
	}}
	add := func(provider library.Provider, source library.Subscription, ids []string) {
		for i, id := range ids {
			use, reason := library.UseAllowed, ""
			switch {
			case i == 0:
				use = library.UseDefault
			case i == len(ids)-1:
				use, reason = library.UseExcluded, "retired by the vendor, and the subscription no longer serves it"
			}
			loaded.Models = append(loaded.Models, library.Model{Provider: provider, ID: id, Subscription: source,
				Windows: windows, Use: use, Kind: library.KindLLM, Reason: reason})
		}
	}
	add(library.Anthropic, claudeSub, []string{"claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5", "claude-opus-4-8",
		"claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-4-6", "claude-sonnet-4-5", "claude-opus-4-5", "claude-haiku-3"})
	add(library.OpenAI, codexSub, []string{"gpt-5.6-sol", "gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.5", "gpt-5.4",
		"gpt-5.3", "gpt-5.2", "gpt-5.1", "gpt-5", "gpt-reserve"})
	loaded.Roles = []library.Role{{ID: library.RoleOrchestrator, Model: loaded.Models[0]}}
	return loaded
}

func testPicker(width, height int) Model {
	loaded := testLibrary()
	targets := []Target{
		{Name: "orchestrator", Job: library.RoleOrchestrator.What(), Assigned: loaded.Models[0].Slug(), Role: library.RoleOrchestrator},
		{Name: "go-dev", Job: "every Go ticket", Assigned: "inherit"},
	}
	built := Build(loaded, everySource(loaded), targets)
	built.SetSize(width, height)
	return built
}

func TestEveryRowSpellsSourceSlashModel(t *testing.T) {
	loaded := shippedLibrary(t)
	if len(loaded.Models) == 0 {
		t.Fatal("the shipped library carries no model")
	}
	type paidFor struct {
		source library.Subscription
		id     string
	}
	known := map[paidFor]bool{}
	for _, one := range loaded.Models {
		known[paidFor{one.Subscription, one.ID}] = true
	}
	for _, group := range Build(loaded, everySource(loaded), nil).Groups {
		for _, row := range group.Rows {
			source, name, found := strings.Cut(row.Slug, "/")
			if !found {
				t.Fatalf("%s has no slash", row.Slug)
			}
			if source != group.Source {
				t.Errorf("%s: the source is %q, want the subscription %q that pays for it", row.Slug, source, group.Source)
			}
			if !known[paidFor{library.Subscription(source), name}] {
				t.Errorf("%s: the library has no %q under the %q subscription, and a slug names the money", row.Slug, name, source)
			}
		}
	}
}

func TestAnEffortOneSubscriptionRefusesFallsBackOnTheOther(t *testing.T) {
	built := testPicker(120, 36)
	for range 20 {
		if row, _ := built.Picked(); strings.HasPrefix(row.Slug, "codex-sub/") {
			break
		}
		built.Key("down")
	}
	for range len(llm.Efforts()) {
		built.Key("shift+left")
	}
	if built.Effort() != llm.EffortNone {
		t.Fatalf("the codex-sub group offers %s and the walk stopped at %q", llm.EffortList(llm.Efforts()), built.Effort())
	}
	for range 20 {
		if row, _ := built.Picked(); strings.HasPrefix(row.Slug, "claude-sub/") {
			break
		}
		built.Key("up")
	}
	if built.Effort() != llm.EffortDefault {
		t.Fatalf("the pick moved to a group that refuses %s and the effort stayed %q", llm.EffortNone, built.Effort())
	}
}

func TestModelsSeparateProvidersAndRoleAssignments(t *testing.T) {
	m := testPicker(120, 36)
	m.Key("tab")
	if m.tab != tabRoles || !strings.Contains(ansi.Strip(m.View()), "ROLE PRESETS") {
		t.Fatalf("tab did not switch to roles: tab=%d", m.tab)
	}
	m.Key("down")
	if got := m.Key("enter"); got.Action != None || m.assign != &m.targets[1] || m.tab != tabModels {
		t.Fatalf("the go-dev row did not request a model: %+v, assign %+v, tab %d", got, m.assign, m.tab)
	}
	for _, key := range strings.Split("gpt-5.6-luna", "") {
		m.Key(key)
	}
	want := Intent{Action: Assign, Agent: "go-dev", Slug: "codex-sub/gpt-5.6-luna"}
	if got := m.Key("enter"); got != want {
		t.Fatalf("assigning returned %+v, want %+v", got, want)
	}
	if m.targets[1].Assigned != want.Slug {
		t.Fatalf("the roles tab still shows %q for go-dev", m.targets[1].Assigned)
	}
}

func TestAnExcludedModelStaysListedAndChoosingItStartsLogin(t *testing.T) {
	m := testPicker(120, 36)
	for _, key := range strings.Split("gpt-reserve", "") {
		m.Key(key)
	}
	if !strings.Contains(ansi.Strip(m.View()), "retired by the vendor") {
		t.Fatal("the excluded row is not listed with its reason")
	}
	want := Intent{Action: Login, Slug: "codex-sub/gpt-reserve"}
	if got := m.Key("enter"); got != want {
		t.Fatalf("choosing an excluded model returned %+v, want %+v", got, want)
	}
}

func TestNarrowModelPickerUsesTwoReadablePanes(t *testing.T) {
	m := testPicker(60, 20)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "codex-sub") || strings.Contains(view, "SELECTION") {
		t.Fatal("narrow picker wrapped its detail pane or provider names")
	}
	for y, line := range strings.Split(view, "\n") {
		if at := strings.Index(line, "codex-sub"); at >= 0 {
			m.Click(ansi.StringWidth(line[:at]), y)
			if m.providers()[m.provider] != "codex-sub" {
				t.Fatal("narrow provider row did not own its click")
			}
			return
		}
	}
	t.Fatal("provider row not visible")
}

func TestModelDialogScalesWithTerminal(t *testing.T) {
	smallWidth, smallHeight := modelDialogSize(100, 30)
	largeWidth, largeHeight := modelDialogSize(168, 42)
	if largeWidth <= smallWidth || largeHeight <= smallHeight {
		t.Fatalf("model dialog stayed fixed: %dx%d and %dx%d", smallWidth, smallHeight, largeWidth, largeHeight)
	}
	if largeWidth >= 168 || largeHeight >= 42 {
		t.Fatal("model dialog exceeds viewport")
	}
}

func TestPickerGoldens(t *testing.T) {
	roles := testPicker(120, 36)
	roles.Key("tab")
	for name, view := range map[string]string{
		"picker-120x36.golden":       testPicker(120, 36).View(),
		"picker-roles-120x36.golden": roles.View(),
		"picker-60x20.golden":        testPicker(60, 20).View(),
		"picker-empty-120x36.golden": func() string {
			empty := Build(library.Library{}, nil, nil)
			empty.SetSize(120, 36)
			return empty.View()
		}(),
	} {
		golden.Assert(t, name, ansi.Strip(view))
	}
}

func BenchmarkModelDialog(b *testing.B) {
	m := testPicker(120, 36)
	base := strings.TrimSuffix(strings.Repeat(strings.Repeat("the session behind the dialog ", 4)+"\n", 36), "\n")
	for b.Loop() {
		m.Dialog(base, 120, 36)
	}
}
