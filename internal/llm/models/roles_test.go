package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func twoModels() fstest.MapFS {
	return withSubscriptions(fstest.MapFS{
		"models/openai/gpt-5.6-sol.yaml":      &fstest.MapFile{Data: []byte("subscription: codex-sub\nuse: default\n")},
		"models/openai/gpt-5.6-luna.yaml":     &fstest.MapFile{Data: []byte("subscription: codex-sub\nuse: allowed\n")},
		"models/anthropic/claude-opus-5.yaml": &fstest.MapFile{Data: []byte("subscription: claude-sub\nuse: default\n")},
		"models/anthropic/claude-fable-5.yaml": &fstest.MapFile{
			Data: []byte("subscription: claude-sub\nuse: excluded\nreason: the owner will not pay for fable\n")},
	})
}

func boundIn(t *testing.T, files fstest.MapFS, fallback Subscription) Bindings {
	t.Helper()
	library, err := Load([]Layer{layerOf("library", files)})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	bound, err := library.Bind(fallback)
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	return bound
}

func withRole(id RoleID, slug string) fstest.MapFS {
	files := twoModels()
	files["roles/"+string(id)+".yaml"] = &fstest.MapFile{Data: []byte("model: " + slug + "\n")}
	return files
}

func TestEachRoleBindsToAModelSlugAndReadsBack(t *testing.T) {
	for id, slug := range map[RoleID]string{RoleTurn: "claude-sub/claude-opus-5", RoleChild: "codex-sub/gpt-5.6-luna"} {
		bound := boundIn(t, withRole(id, slug), ClaudeSub)[id]
		if bound.Model.Slug() != slug {
			t.Fatalf("%s reads back %q, want %q", id, bound.Model.Slug(), slug)
		}
		if bound.Wire != map[Subscription]string{ClaudeSub: "anthropic", CodexSub: "codex"}[bound.Model.Subscription] {
			t.Fatalf("%s binds %s and reports wire %q", id, slug, bound.Wire)
		}
		if bound.By != BoundByFile || !strings.Contains(bound.File, string(id)+".yaml") {
			t.Fatalf("%s does not report the file that bound it: %+v", id, bound)
		}
	}
}

func TestARoleNamingAModelTheLibraryDoesNotHaveIsRefusedByName(t *testing.T) {
	_, err := Load([]Layer{layerOf("library", withRole(RoleTurn, "claude-sub/claude-opus-9"))})
	if err == nil || !strings.Contains(err.Error(), "no claude-sub/claude-opus-9") {
		t.Fatalf("want the unknown model refused by name, got %v", err)
	}
	if !strings.Contains(err.Error(), "roles"+string(filepath.Separator)+"turn.yaml") {
		t.Fatalf("the refusal does not name the role file: %v", err)
	}
}

func TestARoleNamingAnExcludedModelIsRefusedWithTheReasonInTheLibrary(t *testing.T) {
	_, err := Load([]Layer{layerOf("library", withRole(RoleChild, "claude-sub/claude-fable-5"))})
	if err == nil || !strings.Contains(err.Error(), "the owner will not pay for fable") {
		t.Fatalf("want the library's own reason, got %v", err)
	}
}

func TestAFileUnderRolesThatIsNotARoleIsRefusedByName(t *testing.T) {
	files := twoModels()
	files["roles/vision.yaml"] = &fstest.MapFile{Data: []byte("model: openai/gpt-5.6-luna\n")}
	_, err := Load([]Layer{layerOf("library", files)})
	if err == nil || !strings.Contains(err.Error(), "a role is turn or child") {
		t.Fatalf("want an unknown role refused by name, got %v", err)
	}
}

func TestAProjectRoleFileLeavesTheGlobalOneTheProjectDoesNotName(t *testing.T) {
	global := twoModels()
	global["roles/turn.yaml"] = &fstest.MapFile{Data: []byte("model: claude-sub/claude-opus-5\n")}
	global["roles/child.yaml"] = &fstest.MapFile{Data: []byte("model: codex-sub/gpt-5.6-sol\n")}
	project := oneFile("roles/child.yaml", "model: codex-sub/gpt-5.6-luna\n")

	library, err := Load([]Layer{layerOf("global", global), layerOf("project", project)})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	bound, err := library.Bind(ClaudeSub)
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	if got := bound[RoleChild].Model.Slug(); got != "codex-sub/gpt-5.6-luna" {
		t.Fatalf("the project layer did not win the child role, it reads %q", got)
	}
	if got := bound[RoleTurn].Model.Slug(); got != "claude-sub/claude-opus-5" {
		t.Fatalf("the project layer took the turn role it never named, it reads %q", got)
	}
	if !strings.HasPrefix(bound[RoleTurn].File, "global") {
		t.Fatalf("the turn role reports %q rather than the global file", bound[RoleTurn].File)
	}
}

func TestARoleWithNothingBoundFallsBackToTheSubscriptionDefaultAndSaysSo(t *testing.T) {
	bound := boundIn(t, twoModels(), CodexSub)[RoleChild]
	if bound.By != BoundByDefault || bound.Model.Slug() != "codex-sub/gpt-5.6-sol" {
		t.Fatalf("the child role did not fall back to the codex default: %+v", bound)
	}
	want := "child has nothing bound, so it runs codex-sub/gpt-5.6-sol, the codex-sub default"
	if bound.Says() != want {
		t.Fatalf("it says %q, want %q", bound.Says(), want)
	}
}

func TestBindingsArePinnedAndAReReadPicksUpTheChange(t *testing.T) {
	dir := t.TempDir()
	write := func(slug string) {
		if err := os.MkdirAll(filepath.Join(dir, rolesDir), 0o755); err != nil {
			t.Fatalf("making the roles directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, rolesDir, "turn.yaml"), []byte("model: "+slug+"\n"), 0o644); err != nil {
			t.Fatalf("writing the role file: %v", err)
		}
	}
	reread := func() Bindings {
		library, err := Load([]Layer{layerOf("library", twoModels()), {Name: "project", Origin: dir, FS: os.DirFS(dir)}})
		if err != nil {
			t.Fatalf("loading: %v", err)
		}
		bound, err := library.Bind(ClaudeSub)
		if err != nil {
			t.Fatalf("binding: %v", err)
		}
		return bound
	}

	write("claude-sub/claude-opus-5")
	pinned := reread()
	write("codex-sub/gpt-5.6-luna")

	if got := pinned[RoleTurn].Model.Slug(); got != "claude-sub/claude-opus-5" {
		t.Fatalf("the pinned binding changed under the session, it reads %q", got)
	}
	if got := reread()[RoleTurn].Model.Slug(); got != "codex-sub/gpt-5.6-luna" {
		t.Fatalf("re-reading did not pick up the change, it reads %q", got)
	}
}

func BenchmarkReReadingEveryLibraryFile(b *testing.B) {
	layer := shippedLayer()
	for b.Loop() {
		library, err := Load([]Layer{layer})
		if err != nil {
			b.Fatalf("loading: %v", err)
		}
		if _, err := library.Bind(ClaudeSub); err != nil {
			b.Fatalf("binding: %v", err)
		}
	}
}
