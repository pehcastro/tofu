package subagent

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
		goDev := definitionNamed(t, all, "go-dev")
		if goDev.Origin != ".tofu" || goDev.Model != "claude-sub/claude-opus-5" || goDev.Effort != "high" {
			t.Fatalf("the .tofu go-dev should win with its own model and effort, got %+v", goDev)
		}
		want := filepath.Join("testdata", "definition", "project", ".claude", "agents", "go-dev.md")
		if len(goDev.Shadowed) != 1 || goDev.Shadowed[0].Path != want || goDev.Shadowed[0].Written != "sonnet" {
			t.Fatalf("the .claude go-dev should be shadowed, got %+v", goDev.Shadowed)
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
		if goDev := definitionNamed(t, all, "go-dev"); goDev.Runs != RunsModel || len(goDev.Refused) != 0 {
			t.Fatalf("one refused definition should not touch another, got %+v", goDev)
		}
	})

	t.Run("a missing home", func(t *testing.T) {
		for _, home := range []string{"", filepath.Join(t.TempDir(), "absent")} {
			found := Definitions(scanOf(t, home, "tofu", "agents", "claude"))
			definitionNamed(t, found, "go-dev")
			definitionNamed(t, found, "qa")
			if slices.ContainsFunc(found.Definitions, func(d Definition) bool { return d.Name == "helper" }) {
				t.Fatalf("home %q should not yield the home helper", home)
			}
			if (home == "") != (len(found.Notices) == 1) {
				t.Fatalf("home %q: an unknown home is one notice and an absent folder is none, got %q", home, found.Notices)
			}
		}
		if helper := definitionNamed(t, all, "helper"); helper.Origin != "~/.agents" || helper.Runs != RunsInherit {
			t.Fatalf("the home helper should come from ~/.agents and inherit, got %+v", helper)
		}
	})

	t.Run("agentSources without claude", func(t *testing.T) {
		found := Definitions(scanOf(t, home, "tofu", "agents"))
		if slices.ContainsFunc(found.Definitions, func(d Definition) bool { return d.Name == "reviewer" }) {
			t.Fatal("reviewer lives only in .claude and should not be read")
		}
		if goDev := definitionNamed(t, found, "go-dev"); len(goDev.Shadowed) != 0 {
			t.Fatalf("with claude left out nothing shadows go-dev, got %q", goDev.Shadowed)
		}
		if qa := definitionNamed(t, found, "qa"); qa.Origin != "library" {
			t.Fatalf("the library is always read, got %+v", qa)
		}
	})
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
