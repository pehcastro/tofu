package rule

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadFSReadsEveryRuleAndNamesWhereItCameFrom(t *testing.T) {
	shipped := fstest.MapFS{
		"comments@1.yaml": {Data: []byte("id: comments\nkind: structural\nchecker: comments\n")},
		"em_dash@1.yaml":  {Data: []byte("id: em_dash\nkind: structural\nchecker: em_dash\nmode: enforced\n")},
		"notes.md":        {Data: []byte("not a rule\n")},
	}
	rules, err := LoadFS(shipped)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want 2: %+v", len(rules), rules)
	}
	if rules[0].ID != "comments" || rules[1].Mode != ModeEnforced {
		t.Fatalf("LoadFS did not read the rules it was given: %+v", rules)
	}
	for _, r := range rules {
		if want := "catalog/rules/" + r.ID + "@1.yaml"; r.File != want {
			t.Fatalf("rule %q says it came from %q, want %q", r.ID, r.File, want)
		}
	}
}

func TestLoadFSNamesTheFileInAnError(t *testing.T) {
	_, err := LoadFS(fstest.MapFS{"broken@1.yaml": {Data: []byte("id: broken\nkind: rumour\nchecker: none\n")}})
	if err == nil {
		t.Fatal("LoadFS accepted a rule whose kind is not a kind")
	}
	if !strings.HasPrefix(err.Error(), "catalog/rules/broken@1.yaml") {
		t.Fatalf("the error does not start with the file it came from: %v", err)
	}
}

func TestLoadRejectsAnUnknownModeAsAnError(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "unknown_mode.yaml"))
	if err == nil {
		t.Fatal("Load returned no error for an unknown mode")
	}
}

func TestLoadDefaultsToShadowWhenModeIsMissing(t *testing.T) {
	r, err := Load(filepath.Join("testdata", "no_mode.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.ModeDeclared {
		t.Fatal("ModeDeclared = true, want false, no mode key was present")
	}
	if r.Mode != ModeShadow {
		t.Fatalf("Mode = %s, want %s", r.Mode, ModeShadow)
	}
}

func TestLayerLetsAProjectRetuneOneShippedRuleTurnAnotherOffAndAddItsOwn(t *testing.T) {
	off, err := parseRule([]byte("id: test_boundary_cases\nkind: structural\nchecker: test_boundary_cases\nmode: off\n"), "someone-elses-project/rules/test_boundary_cases@1.yaml")
	if err != nil {
		t.Fatalf("parseRule: %v", err)
	}
	shipped := []Rule{
		{ID: "test_assertion", Kind: KindStructural, Checker: "test_assertion", Mode: ModeShadow},
		{ID: "test_boundary_cases", Kind: KindStructural, Checker: "test_boundary_cases", Mode: ModeShadow},
	}
	project := []Rule{
		{ID: "test_assertion", Kind: KindStructural, Checker: "test_assertion", Mode: ModeEnforced},
		off,
		{ID: "no_worktree", Kind: KindStructural, Checker: "no_worktree", Mode: ModeShadow},
	}

	layered := Layer(shipped, project)

	if len(layered) != 2 {
		t.Fatalf("layered = %d rules, want 2: %+v", len(layered), layered)
	}
	if layered[0].ID != "test_assertion" || layered[0].Mode != ModeEnforced {
		t.Fatalf("the project did not retune the shipped rule: %+v", layered[0])
	}
	if layered[1].ID != "no_worktree" {
		t.Fatalf("the rule the project added on its own is %q, want no_worktree", layered[1].ID)
	}
	for _, r := range layered {
		if r.ID == "test_boundary_cases" {
			t.Fatal("a rule the project turned off is still in the layered set")
		}
	}
}

func TestLoadDirLoadsEveryShippedRule(t *testing.T) {
	rules, err := LoadDir(filepath.Join("..", "..", "catalog", "rules"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(rules) != 7 {
		t.Fatalf("rules = %d, want 7: %+v", len(rules), rules)
	}
	for _, r := range rules {
		if r.Mode != ModeShadow {
			t.Fatalf("rule %q ships in mode %s, want %s, this ticket promotes nothing", r.ID, r.Mode, ModeShadow)
		}
	}
}
