package subagent

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"tofu/internal/konst"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
	shipped "tofu/library"
)

func scanOf(t testing.TB, home string, sources ...string) Scan {
	catalog, err := models.Load([]sys.Layer{{Name: "library", Origin: "library", FS: shipped.Files()}})
	if err != nil {
		t.Fatal(err)
	}
	return Scan{
		Project: filepath.Join("testdata", "definition", "project"),
		Home:    home,
		Sources: sources,
		Library: shipped.Files(),
		Tools:   []string{"read", "edit", "bash", "glob", "search", "symbols"},
		Catalog: catalog,
	}
}

func definitionNamed(t *testing.T, found Found, name string) Definition {
	t.Helper()
	for _, definition := range found.Definitions {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("no definition named %s in %+v", name, found)
	return Definition{}
}

func TestDiscoveryFailures(t *testing.T) {
	home := filepath.Join("testdata", "definition", "home")
	all := Definitions(scanOf(t, home, "tofu", "agents", "claude"))

	t.Run("two folders with the same name", func(t *testing.T) {
		fixtureDev := definitionNamed(t, all, "fixture-dev")
		if fixtureDev.Origin != ".tofu" || fixtureDev.Model != "claude-sub/claude-opus-5" || fixtureDev.Effort != "high" {
			t.Fatalf("the .tofu fixture-dev should win with its own model and effort, got %+v", fixtureDev)
		}
		want := filepath.Join("testdata", "definition", "project", ".claude", "agents", "fixture-dev.md")
		if len(fixtureDev.Shadowed) != 1 || fixtureDev.Shadowed[0].Path != want || fixtureDev.Shadowed[0].Written != "sonnet" {
			t.Fatalf("the .claude fixture-dev should be shadowed, got %+v", fixtureDev.Shadowed)
		}
	})

	t.Run("a .claude file with model opus", func(t *testing.T) {
		reviewer := definitionNamed(t, all, "reviewer")
		if reviewer.Runs != RunsModel || reviewer.Model != "claude-sub/claude-opus-5" {
			t.Fatalf("opus should resolve through the catalog, got %+v", reviewer)
		}
		if !slices.Equal(reviewer.Ignored, []string{"color", "permissionMode"}) {
			t.Fatalf("color and permissionMode should be named as ignored, got %q", reviewer.Ignored)
		}
	})

	t.Run("a .claude file naming Claude tools", func(t *testing.T) {
		reviewer := definitionNamed(t, all, "reviewer")
		if reviewer.Runs != RunsModel || !slices.Equal(reviewer.Tools, []string{"read", "search", "glob", "bash", "edit"}) {
			t.Fatalf("Claude tools should translate to tofu tools once each, got %+v", reviewer)
		}
		if !slices.Equal(reviewer.IgnoredTools, []string{"NotebookEdit"}) {
			t.Fatalf("NotebookEdit has no tofu counterpart and should be ignored, got %q", reviewer.IgnoredTools)
		}
	})

	t.Run("a file with no front matter", func(t *testing.T) {
		if len(all.Broken) != 1 || !strings.HasSuffix(all.Broken[0].Path, "notes.md") || !strings.Contains(all.Broken[0].Reason, "front matter") {
			t.Fatalf("notes.md should be reported broken for its missing front matter, got %+v", all.Broken)
		}
		definitionNamed(t, all, "scout")
	})

	t.Run("an unknown tool", func(t *testing.T) {
		scout := definitionNamed(t, all, "scout")
		if scout.Runs != RunsRefused || len(scout.Refused) != 1 || !strings.Contains(scout.Refused[0], `"teleport"`) {
			t.Fatalf("scout should be refused for naming teleport, got %+v", scout)
		}
		if fixtureDev := definitionNamed(t, all, "fixture-dev"); fixtureDev.Runs != RunsModel || len(fixtureDev.Refused) != 0 {
			t.Fatalf("one refused definition should not touch another, got %+v", fixtureDev)
		}
	})

	t.Run("a missing home", func(t *testing.T) {
		for _, home := range []string{"", filepath.Join(t.TempDir(), "absent")} {
			found := Definitions(scanOf(t, home, "tofu", "agents", "claude"))
			definitionNamed(t, found, "fixture-dev")
			definitionNamed(t, found, "qa")
			if slices.ContainsFunc(found.Definitions, func(d Definition) bool { return d.Name == "helper" }) {
				t.Fatalf("home %q should not yield the home helper", home)
			}
			if (home == "") != (len(found.Notices) == 1) {
				t.Fatalf("home %q: an unknown home is one notice and an absent folder is none, got %q", home, found.Notices)
			}
		}
		if slices.ContainsFunc(all.Definitions, func(d Definition) bool { return d.Name == "helper" }) {
			t.Fatal("the helper in ~/.agents belongs to another harness and should not be read")
		}
	})

	t.Run("the home gives only ~/.tofu/agents", func(t *testing.T) {
		others := map[string]string{".claude/agents/y.md": agentFile("y", "inherit"), ".agents/agents/w.md": agentFile("w", "inherit")}
		withTofu := map[string]string{".tofu/agents/a.md": agentFile("a", "inherit")}
		maps.Copy(withTofu, others)
		for _, c := range []struct {
			home map[string]string
			want []string
		}{{others, nil}, {withTofu, []string{"~/.tofu/a"}}} {
			scan := scanOf(t, projectWith(t, c.home), "tofu", "agents", "claude")
			scan.Project = t.TempDir()
			var read []string
			for _, found := range Definitions(scan).Definitions {
				if found.Origin != libraryOrigin {
					read = append(read, found.Origin+"/"+found.Name)
				}
			}
			if !slices.Equal(read, c.want) {
				t.Fatalf("read %q, want %q", read, c.want)
			}
		}
	})

	t.Run("agentSources without claude", func(t *testing.T) {
		found := Definitions(scanOf(t, home, "tofu", "agents"))
		if slices.ContainsFunc(found.Definitions, func(d Definition) bool { return d.Name == "reviewer" }) {
			t.Fatal("reviewer lives only in .claude and should not be read")
		}
		if fixtureDev := definitionNamed(t, found, "fixture-dev"); len(fixtureDev.Shadowed) != 0 {
			t.Fatalf("with claude left out nothing shadows fixture-dev, got %q", fixtureDev.Shadowed)
		}
		if qa := definitionNamed(t, found, "qa"); qa.Origin != "library" {
			t.Fatalf("the library is always read, got %+v", qa)
		}
	})
}

func projectWith(t *testing.T, files map[string]string) string {
	project := t.TempDir()
	for name, text := range files {
		path := filepath.Join(project, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return project
}

func agentFile(name, model string) string {
	return "---\nname: " + name + "\ndescription: " + name + " for a test\nmodel: " + model + "\n---\nYou are " + name + ".\n"
}

func TestModelTiers(t *testing.T) {
	project := projectWith(t, map[string]string{
		".claude/agents/writer.md":      agentFile("writer", "sonnet"),
		".claude/agents/shared-tier.md": agentFile("shared-tier", `"@genius"`),
		".tofu/agents/thinker.md":       agentFile("thinker", "@genius"),
		".tofu/agents/typo.md":          agentFile("typo", "@brilliant"),
		".tofu/agents/planner.md":       agentFile("planner", "inherit"),
		".tofu/agents/assigned.md":      agentFile("assigned", "inherit"),
		".tofu/agent-models.yaml":       "planner: \"@genius\"\nassigned: sonnet\n",
	})
	scan := func(tiers map[Tier]string) Found {
		s := scanOf(t, "", "tofu", "agents", "claude")
		s.Project, s.Tiers = project, tiers
		return Definitions(s)
	}

	t.Run("no tier set", func(t *testing.T) {
		found := scan(nil)
		if writer := definitionNamed(t, found, "writer"); writer.Model != "claude-sub/claude-sonnet-5" || writer.From != "file" {
			t.Fatalf("with no tier, sonnet resolves through the catalog from the file, got %+v", writer)
		}
		for _, name := range []string{"thinker", "planner"} {
			one := definitionNamed(t, found, name)
			if one.Runs != RunsInherit || one.From != "tier genius" || len(one.Notices) != 1 || !strings.Contains(one.Notices[0], "modelTier.genius") {
				t.Fatalf("%s: an unset tier inherits with a notice naming the setting, got %+v", name, one)
			}
		}
		if planner := definitionNamed(t, found, "planner"); planner.AssignedIn == "" {
			t.Fatalf("the tier came from agent-models.yaml, got %+v", planner)
		}
		if shared := definitionNamed(t, found, "shared-tier"); shared.Runs != RunsRefused {
			t.Fatalf("a tier in a shared .claude file is refused, got %+v", shared)
		}
		if typo := definitionNamed(t, found, "typo"); typo.Runs != RunsRefused || !strings.Contains(strings.Join(typo.Refused, ""), "@brilliant") {
			t.Fatalf("an unknown tier is refused by name, got %+v", typo)
		}
	})

	t.Run("tiers set", func(t *testing.T) {
		found := scan(map[Tier]string{TierSmart: "codex-sub/gpt-5.6-sol", TierGenius: "claude-sub/claude-opus-5"})
		if writer := definitionNamed(t, found, "writer"); writer.Model != "codex-sub/gpt-5.6-sol" || writer.From != "tier smart" {
			t.Fatalf("sonnet in a shared file resolves to the smart tier, got %+v", writer)
		}
		if thinker := definitionNamed(t, found, "thinker"); thinker.Runs != RunsModel || thinker.Model != "claude-sub/claude-opus-5" || len(thinker.Notices) != 0 {
			t.Fatalf("@genius resolves to the genius tier, got %+v", thinker)
		}
		if assigned := definitionNamed(t, found, "assigned"); assigned.Model != "claude-sub/claude-sonnet-5" || assigned.From != "agent-models.yaml" {
			t.Fatalf("sonnet in tofu's own agent-models.yaml is the catalog alias, not a tier, got %+v", assigned)
		}
	})

	t.Run("a tier set to a model tofu refuses", func(t *testing.T) {
		thinker := definitionNamed(t, scan(map[Tier]string{TierGenius: "claude-sub/claude-opus-4-8"}), "thinker")
		if thinker.Runs != RunsRefused || !strings.Contains(strings.Join(thinker.Refused, ""), "modelTier.genius") {
			t.Fatalf("a tier naming an excluded model is refused naming the setting, got %+v", thinker)
		}
	})

	t.Run("an alias picks the newest allowed model of its family", func(t *testing.T) {
		sonnet := func(id string, use models.Use) models.Model {
			return models.Model{Provider: models.Anthropic, ID: id, Subscription: models.ClaudeSub, Use: use, Kind: models.KindLLM}
		}
		for _, c := range []struct {
			name  string
			first []models.Model
			last  []models.Model
			want  string
		}{
			{"a newer point release", nil, []models.Model{sonnet("claude-sonnet-5-5", models.UseAllowed)}, "claude-sub/claude-sonnet-5-5"},
			{"the newer one listed first", []models.Model{sonnet("claude-sonnet-5-5", models.UseAllowed)}, nil, "claude-sub/claude-sonnet-5-5"},
			{"ten after nine", nil, []models.Model{sonnet("claude-sonnet-5-10", models.UseAllowed), sonnet("claude-sonnet-5-9", models.UseAllowed)}, "claude-sub/claude-sonnet-5-10"},
			{"a date is not a version", nil, []models.Model{sonnet("claude-sonnet-5-20990101", models.UseAllowed), sonnet("claude-sonnet-5-5", models.UseAllowed)}, "claude-sub/claude-sonnet-5-5"},
			{"an excluded newer one", nil, []models.Model{sonnet("claude-sonnet-6", models.UseExcluded), sonnet("claude-sonnet-5-5", models.UseAllowed)}, "claude-sub/claude-sonnet-5-5"},
		} {
			s := scanOf(t, "", "claude")
			s.Project = project
			s.Catalog.Models = slices.Concat(c.first, s.Catalog.Models, c.last)
			if writer := definitionNamed(t, Definitions(s), "writer"); writer.Runs != RunsModel || writer.Model != c.want || len(writer.Notices) != 0 {
				t.Fatalf("%s: sonnet should run on %s, got %+v", c.name, c.want, writer)
			}
		}
	})

	t.Run("an alias that finds no model", func(t *testing.T) {
		s := scanOf(t, "", "claude")
		s.Project, s.Catalog = project, models.Library{}
		writer := definitionNamed(t, Definitions(s), "writer")
		if writer.Runs != RunsInherit || len(writer.Notices) != 1 {
			t.Fatalf("an alias with no model inherits with a notice, got %+v", writer)
		}
	})
}

func TestReferences(t *testing.T) {
	var every []string
	for _, name := range []string{"ts-strict-config", "ts-type-design", "ts-boundaries", "flakiness", "failure-triage", "metrics", "test-planning"} {
		every = append(every, "  - "+name)
	}
	project := projectWith(t, map[string]string{
		".claude/agents/foreign.md": "---\nname: foreign\ndescription: d\nreferences:\n  - ts-boundaries\n---\nx\n",
		".tofu/agents/missing.md":   "---\nname: missing\ndescription: d\nreferences:\n  - no-such-reference\n---\nx\n",
		".tofu/agents/reader.md":    "---\nname: reader\ndescription: d\nreferences:\n" + strings.Join(every, "\n") + "\n---\nx\n",
	})
	s := scanOf(t, "", "tofu", "claude")
	s.Project = project
	found := Definitions(s)

	tsDev := definitionNamed(t, found, "ts-dev")
	var names []string
	for _, reference := range tsDev.References {
		names = append(names, reference.Name)
		if reference.Text == "" || strings.Contains(reference.Text, "\nfound:") || strings.HasPrefix(reference.Text, "---") {
			t.Fatalf("%s should carry its body without front matter, got %q", reference.Name, reference.Text)
		}
	}
	if !slices.Equal(names, []string{"ts-strict-config", "ts-type-design", "ts-boundaries", "verify-a-running-service"}) || len(tsDev.Cut) != 0 {
		t.Fatalf("ts-dev carries its four references whole, got %q cut %q", names, tsDev.Cut)
	}
	if qa := definitionNamed(t, found, "qa"); len(qa.References) != 5 || len(qa.Cut) != 0 {
		t.Fatalf("qa carries its five references whole, four from qa and verify-a-running-service from dev, got %+v cut %q", qa.References, qa.Cut)
	}
	if foreign := definitionNamed(t, found, "foreign"); len(foreign.References) != 0 || !slices.Contains(foreign.Ignored, "references") {
		t.Fatalf("references in a .claude file are not Claude's field and are ignored, got %+v", foreign)
	}
	if missing := definitionNamed(t, found, "missing"); missing.Runs != RunsRefused || !strings.Contains(strings.Join(missing.Refused, ""), "no-such-reference") {
		t.Fatalf("a tofu file naming a reference the library lacks is refused, got %+v", missing)
	}
	reader := definitionNamed(t, found, "reader")
	total := 0
	for _, reference := range reader.References {
		total += len(reference.Text)
	}
	if len(reader.Cut) == 0 || total > konst.SubAgentReferenceBytes || len(reader.References)+len(reader.Cut) != 7 {
		t.Fatalf("seven references pass the budget, so the tail is cut and named: kept %d bytes, cut %q", total, reader.Cut)
	}
}

func BenchmarkDefinitionsTwenty(b *testing.B) {
	project := b.TempDir()
	folder := filepath.Join(project, ".claude", "agents")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		b.Fatal(err)
	}
	for i := range 20 {
		body := fmt.Sprintf("---\nname: agent-%d\ndescription: number %d\nmodel: sonnet\ntools: read, bash\n---\nYou are number %d.\n", i, i, i)
		if err := os.WriteFile(filepath.Join(folder, fmt.Sprintf("agent-%d.md", i)), []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	scan := scanOf(b, b.TempDir(), "tofu", "agents", "claude")
	scan.Project = project
	library, err := fs.Glob(shipped.Files(), "*/agents/*.md")
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if found := Definitions(scan); len(found.Definitions) != 20+len(library) {
			b.Fatalf("want 20 definitions and the library's %d, got %d", len(library), len(found.Definitions))
		}
	}
}

func TestToolsTheModeDoesNotBuild(t *testing.T) {
	every := []string{"browser_tabs", "browser_observe", "browser_act", "browser_motion"}
	for _, row := range []struct {
		mode, tools, why string
		built, known     []string
	}{
		{mode: "read", built: every[:2], known: every, tools: "browser_tabs browser_observe"},
		{mode: "a tool nowhere", built: every[:2], known: every[:3], why: `names the tool "browser_motion", which tofu does not have`},
		{mode: "off", known: every, why: `names the tool "browser_tabs", which tofu does not have`},
	} {
		scan := scanOf(t, t.TempDir())
		scan.Tools, scan.KnownTools = append(slices.Clone(scan.Tools), row.built...), append(slices.Clone(scan.Tools), row.known...)
		browser := definitionNamed(t, Definitions(scan), "browser")
		refused := strings.Join(browser.Refused, "; ")
		if row.why == "" && (browser.Runs == RunsRefused || strings.Join(browser.Tools, " ") != row.tools) {
			t.Fatalf("%s: want the browser sub-agent to run with %s, got %v offering %v refused %q", row.mode, row.tools, browser.Runs, browser.Tools, refused)
		}
		if row.why != "" && (browser.Runs != RunsRefused || !strings.Contains(refused, row.why)) {
			t.Fatalf("%s: want the browser sub-agent refused with %q, got %v offering %v refused %q", row.mode, row.why, browser.Runs, browser.Tools, refused)
		}
	}
}

func TestBothSkillKeysAreRead(t *testing.T) {
	definition, err := read(fstest.MapFS{"a.md": {Data: []byte("---\r\nname: a\r\ndescription: d\r\nskills: [commit, header]\r\nautoloadSkills:\r\n  - tests\r\n---\r\nbody")}}, "a.md")
	if err != nil || strings.Join(definition.Skills, ",") != "commit,header,tests" || slices.Contains(definition.Ignored, "skills") {
		t.Fatalf("skills %v, ignored %v, err %v; want both keys read", definition.Skills, definition.Ignored, err)
	}
}

func TestTheShippedLanguageAgentsNameTheirGate(t *testing.T) {
	found := Definitions(scanOf(t, ""))
	for name, want := range map[string][]string{"rust-dev": {"cargo clippy", "cargo test"}, "go-dev": {"go vet", "go test"}, "py-dev": {"ruff check", "pytest"}, "ts-dev": {"typecheck", "test"}, "qa": nil} {
		definition := definitionNamed(t, found, name)
		if !slices.Equal(definition.Gate, want) || slices.Contains(definition.Ignored, "gate") {
			t.Errorf("%s: gate %q, ignored %q; want gate %q and gate not ignored", name, definition.Gate, definition.Ignored, want)
		}
	}
}
