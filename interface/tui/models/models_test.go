package models

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/golden"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
	"tofu/internal/sys"
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

func noKeys() Keys {
	return Keys{Set: func(string) bool { return false }}
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
				Windows: windows, Use: use, Kind: library.KindLLM, Reason: reason, Layer: "library"})
		}
	}
	add(library.Anthropic, claudeSub, []string{"claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5", "claude-opus-4-8",
		"claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-4-6", "claude-sonnet-4-5", "claude-opus-4-5", "claude-haiku-3"})
	add(library.OpenAI, codexSub, []string{"gpt-5.6-sol", "gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.5", "gpt-5.4",
		"gpt-5.3", "gpt-5.2", "gpt-5.1", "gpt-5", "gpt-reserve"})
	loaded.Models[1].Layer, loaded.Models[1].From = "catalog", "listed by the claude-sub account under claude-cli 2.1.257"
	loaded.Roles = []library.Role{{ID: library.RoleOrchestrator, Model: loaded.Models[0]}}
	return loaded
}

func testPicker(width, height int) Model {
	loaded := testLibrary()
	targets := []Target{
		{Name: "orchestrator", Job: library.RoleOrchestrator.What(), Assigned: loaded.Models[0].Slug(), Role: library.RoleOrchestrator},
		{Name: "go-dev", Job: "every Go ticket", Assigned: "inherit"},
	}
	built := Build(loaded, everySource(loaded), noKeys(), targets)
	built.SetSize(width, height)
	return built
}

func TestEveryRowSpellsSourceSlashModel(t *testing.T) {
	loaded := shippedLibrary(t)
	if len(loaded.Models) == 0 {
		t.Fatal("the shipped library carries no model")
	}
	type paidFor struct {
		source string
		id     string
	}
	known := map[paidFor]bool{}
	for _, one := range loaded.Models {
		source, _, _ := strings.Cut(one.Slug(), "/")
		known[paidFor{source, one.ID}] = true
	}
	for _, group := range Build(loaded, everySource(loaded), noKeys(), nil).Groups {
		for _, row := range group.Rows {
			source, name, found := strings.Cut(row.Slug, "/")
			if !found {
				t.Fatalf("%s has no slash", row.Slug)
			}
			if source != group.Source {
				t.Errorf("%s: the source is %q, want the subscription %q that pays for it", row.Slug, source, group.Source)
			}
			if !known[paidFor{source, name}] {
				t.Errorf("%s: the library has no %q paid by %q, and a slug names the money", row.Slug, name, source)
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

func TestAProviderSwitchAndAReloadKeepTheModelInUse(t *testing.T) {
	const luna = "codex-sub/gpt-5.6-luna"
	m := testPicker(120, 36)
	m.OpenOn(luna)
	for _, step := range []struct {
		does, wants string
		do          func()
	}{
		{"right, to claude-sub, which does not serve it", "claude-sub/claude-opus-5", func() { m.Key("right") }},
		{"right, to codex-sub", luna, func() { m.Key("right") }},
		{"left twice, back to all", luna, func() { m.Key("left"); m.Key("left") }},
		{"tab to roles and back", luna, func() { m.Key("tab"); m.Key("tab") }},
		{"f5", luna, func() { m = m.Rebuild(testLibrary()) }},
	} {
		step.do()
		if row, picked := m.Picked(); !picked || row.Slug != step.wants {
			t.Fatalf("after %s the picker is on %q, want %s", step.does, row.Slug, step.wants)
		}
	}
	m.Key("tab")
	if m.cursor != 0 {
		t.Fatalf("the roles tab opened on target %d, want the first", m.cursor)
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

func classifierPicker(t *testing.T, loaded library.Library) Model {
	t.Helper()
	keys := Keys{
		Set: func(name string) bool {
			stored, err := sys.StoredKeys()
			return err == nil && stored[name] != ""
		},
		Save: func(_ context.Context, name, value string) error { return sys.SaveKey(name, value) },
	}
	targets := []Target{
		{Name: "orchestrator", Job: library.RoleOrchestrator.What(), Role: library.RoleOrchestrator},
		{Name: "classifier", Job: library.RoleClassifier.What(), Role: library.RoleClassifier},
	}
	built := Build(loaded, everySource(loaded), keys, targets)
	built.SetSize(120, 36)
	built.AssignTo(1)
	return built
}

func entered(m *Model) Intent {
	if got := m.Key("enter"); got.Action != CheckKey {
		return got
	}
	return m.Checked(m.KeyCheck(context.Background()))
}

func typeInto(m *Model, text string) {
	for _, key := range strings.Split(text, "") {
		m.Key(key)
	}
}

func TestAKeyModelWithNoKeyAsksForTheKeyMaskedAndEnterStoresIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(sys.TypeSafeKeyName, "")
	const madeUp = "ts-made-up-4d1e9a7c0b52"
	m := classifierPicker(t, shippedLibrary(t))
	view := ansi.Strip(m.View())
	if strings.Contains(view, "claude-sub/") || !strings.Contains(view, "TypeSafe · no key") {
		t.Fatalf("the classifier picker does not list the key providers with their marks\n%s", view)
	}
	for range 4 {
		if row, _ := m.Picked(); row.Slug == "typesafe/jev-latest" {
			break
		}
		m.Key("down")
	}
	if got := m.Key("enter"); got.Action != None || !strings.Contains(ansi.Strip(m.View()), "TypeSafe key") {
		t.Fatalf("enter on a model with no key returned %+v and opened no input\n%s", got, ansi.Strip(m.View()))
	}
	typeInto(&m, madeUp)
	shown := ansi.Strip(m.View())
	for at := 0; at+5 <= len(madeUp); at++ {
		if strings.Contains(shown, madeUp[at:at+5]) {
			t.Fatalf("the input echoes %q of the key\n%s", madeUp[at:at+5], shown)
		}
	}
	if !strings.Contains(shown, "ts-m…0b52  23 chars") {
		t.Fatalf("the input does not show the key's shape\n%s", shown)
	}
	if got := m.Key("esc"); got.Action != None {
		t.Fatalf("esc on the input returned %+v, want the picker to stay", got)
	}
	if stored, _ := sys.StoredKeys(); len(stored) != 0 {
		t.Fatalf("esc stored %d keys", len(stored))
	}
	if strings.Contains(ansi.Strip(m.View()), keyHints) {
		t.Fatal("esc left the input open")
	}
	m.Key("enter")
	if got := entered(&m); got.Action != None {
		t.Fatalf("enter on an empty input returned %+v", got)
	}
	typeInto(&m, madeUp)
	want := Intent{Action: Bind, Role: library.RoleClassifier, Slug: "typesafe/jev-latest"}
	if got := entered(&m); got != want {
		t.Fatalf("enter with a key returned %+v, want %+v", got, want)
	}
	stored, err := sys.StoredKeys()
	if err != nil || stored[sys.TypeSafeKeyName] != madeUp {
		t.Fatalf("the temp home holds a %s of length %d (%v)", sys.TypeSafeKeyName, len(stored[sys.TypeSafeKeyName]), err)
	}
	m.AssignTo(1)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "TypeSafe · key set") {
		t.Fatalf("the mark does not say the key is set\n%s", view)
	}
}

func TestAKeyCheckThatNeverAnswersLeavesThePickerLiveAndEscCancelsIt(t *testing.T) {
	never := make(chan struct{})
	t.Cleanup(func() { close(never) })
	m := classifierPicker(t, shippedLibrary(t))
	m.keys.Save = func(context.Context, string, string) error { <-never; return nil }
	m.Key("enter")
	typeInto(&m, "sk-or-v1-made-up-silent-51c0")
	answered := make(chan Intent, 1)
	go func() { answered <- m.Key("enter") }()
	select {
	case <-answered:
	case <-time.After(2 * time.Second):
		t.Fatal("enter waited 2s on a key check that never answers, so the screen froze")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, keyChecking) {
		t.Fatalf("the input does not say the key is being checked\n%s", view)
	}
	if got := m.Key("esc"); got.Action != CancelCheck || strings.Contains(ansi.Strip(m.View()), keyChecking) {
		t.Fatalf("esc during the check returned %+v and left\n%s", got, ansi.Strip(m.View()))
	}
}

func TestAPastedKeyIsStoredWithoutItsNewline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(sys.OpenRouterKeyName, "")
	const madeUp = "sk-or-v1-made-up-7c2a90e1f3d4"
	m := classifierPicker(t, shippedLibrary(t))
	m.Key("enter")
	m.Paste(madeUp + "\r\n")
	if shown := ansi.Strip(m.View()); strings.Contains(shown, madeUp[len(madeUp)-6:]) {
		t.Fatalf("the pasted key is on the screen\n%s", shown)
	}
	if got := entered(&m); got.Action != Bind {
		t.Fatalf("enter after a paste returned %+v", got)
	}
	if stored, _ := sys.StoredKeys(); stored[sys.OpenRouterKeyName] != madeUp {
		t.Fatalf("the temp home holds an %s of length %d, want the pasted %d", sys.OpenRouterKeyName, len(stored[sys.OpenRouterKeyName]), len(madeUp))
	}
}

func TestPickerGoldens(t *testing.T) {
	roles := testPicker(120, 36)
	roles.Key("tab")
	catalog := testPicker(120, 36)
	catalog.Key("down")
	for name, view := range map[string]string{
		"picker-120x36.golden":         testPicker(120, 36).View(),
		"picker-roles-120x36.golden":   roles.View(),
		"picker-catalog-120x36.golden": catalog.View(),
		"picker-60x20.golden":          testPicker(60, 20).View(),
		"picker-empty-120x36.golden": func() string {
			empty := Build(library.Library{}, nil, noKeys(), nil)
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
