package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/sys"
)

func loadWithLayer(t *testing.T, dir string) Library {
	t.Helper()
	shipped := Layer{Name: "library", Origin: "library", FS: os.DirFS("../../../library")}
	loaded, err := Load([]Layer{shipped, sys.DirLayer("project", dir)})
	if err != nil {
		t.Fatalf("loading the library with the bound layer: %v", err)
	}
	return loaded
}

func roleNamed(loaded Library, id RoleID) (Role, bool) {
	for _, role := range loaded.Roles {
		if role.ID == id {
			return role, true
		}
	}
	return Role{}, false
}

func TestBindRoleIsReadBackByLoad(t *testing.T) {
	dir := t.TempDir()
	if err := BindRole(dir, RoleSubAgent, "claude-sub/claude-sonnet-5"); err != nil {
		t.Fatal(err)
	}
	loaded := loadWithLayer(t, dir)
	role, found := roleNamed(loaded, RoleSubAgent)
	if !found || role.Model.Slug() != "claude-sub/claude-sonnet-5" {
		t.Fatalf("the sub-agent role loads as %+v, found %v", role, found)
	}
	if role.File != filepath.Join(dir, rolesDir, "sub-agent.yaml") {
		t.Fatalf("the binding is attributed to %s", role.File)
	}
	written, err := os.ReadFile(role.File)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := loaded.Bind(ClaudeSub)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s holds %q, and the library says: %s", role.File, written, bindings[RoleSubAgent].Says())
}

func TestBindRoleReplacesAnEarlierBindingAndLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	for _, slug := range []string{"claude-sub/claude-sonnet-5", "codex-sub/gpt-5.6-sol"} {
		if err := BindRole(dir, RoleOrchestrator, slug); err != nil {
			t.Fatal(err)
		}
	}
	role, _ := roleNamed(loadWithLayer(t, dir), RoleOrchestrator)
	if role.Model.Slug() != "codex-sub/gpt-5.6-sol" {
		t.Fatalf("the second binding did not replace the first: %s", role.Model.Slug())
	}
	entries, err := os.ReadDir(filepath.Join(dir, rolesDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the roles directory holds %d entries, want only orchestrator.yaml", len(entries))
	}
}

func TestBindRoleRefusesARoleNothingReads(t *testing.T) {
	dir := t.TempDir()
	if err := BindRole(dir, RoleID("genius"), "claude-sub/claude-sonnet-5"); err == nil {
		t.Fatal("a role outside RoleIDs was written")
	}
	if _, err := os.Stat(filepath.Join(dir, rolesDir)); !os.IsNotExist(err) {
		t.Fatalf("a refused binding still touched the layer: %v", err)
	}
}

func TestTheClassifierRoleBindsOnlyAClassifierModel(t *testing.T) {
	bound := t.TempDir()
	if err := BindRole(bound, RoleClassifier, "typesafe/jev-latest"); err != nil {
		t.Fatal(err)
	}
	loaded := loadWithLayer(t, bound)
	if role, found := roleNamed(loaded, RoleClassifier); !found || role.Model.Slug() != "typesafe/jev-latest" {
		t.Fatalf("the classifier role loads as %+v, found %v", role, found)
	}
	bindings, err := loaded.Bind(ClaudeSub)
	if err != nil {
		t.Fatal(err)
	}
	if binding, handed := bindings[RoleClassifier]; handed {
		t.Fatalf("Bind handed the classifier the subscription binding %s", binding.Says())
	}
	for role, slug := range map[RoleID]string{RoleClassifier: "claude-sub/claude-sonnet-5", RoleOrchestrator: "openrouter/jev-latest"} {
		wrong := t.TempDir()
		if err := BindRole(wrong, role, slug); err != nil {
			t.Fatal(err)
		}
		shipped := Layer{Name: "library", Origin: "library", FS: os.DirFS("../../../library")}
		if _, err := Load([]Layer{shipped, sys.DirLayer("project", wrong)}); err == nil {
			t.Fatalf("the %s role loaded %s, a model of the wrong kind", role, slug)
		}
	}
}

func TestTheUnboundClassifierFollowsTheStoredKeys(t *testing.T) {
	unbound := loadWithLayer(t, t.TempDir())
	bound := t.TempDir()
	if err := BindRole(bound, RoleClassifier, "typesafe/jev-latest"); err != nil {
		t.Fatal(err)
	}
	both := map[string]string{sys.OpenRouterKeyName: "or-made-up", sys.TypeSafeKeyName: "ts-made-up"}
	for _, each := range []struct {
		name    string
		library Library
		stored  map[string]string
		want    string
	}{
		{"nothing stored", unbound, nil, "openrouter/jev-latest"},
		{"an openrouter key", unbound, map[string]string{sys.OpenRouterKeyName: "or-made-up"}, "openrouter/jev-latest"},
		{"a typesafe key", unbound, map[string]string{sys.TypeSafeKeyName: "ts-made-up"}, "typesafe/jev-latest"},
		{"both keys", unbound, both, "openrouter/jev-latest"},
		{"bound to typesafe with both keys", loadWithLayer(t, bound), both, "typesafe/jev-latest"},
	} {
		model, err := each.library.Classifier(each.stored)
		if err != nil || model.Slug() != each.want {
			t.Errorf("%s: the classifier is %q (%v), want %s", each.name, model.Slug(), err, each.want)
		}
	}
	if said := RoleClassifier.Unbound(); !strings.Contains(said, "openrouter/jev-latest") || !strings.Contains(said, "typesafe/jev-latest") {
		t.Errorf("the unbound classifier says %q, and names neither default", said)
	}
}
