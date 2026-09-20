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

func TestLoadDirLoadsEveryShippedRule(t *testing.T) {
	rules, err := LoadDir(filepath.Join("..", "..", "catalog", "rules"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(rules) != 4 {
		t.Fatalf("rules = %d, want 4: %+v", len(rules), rules)
	}
	for _, r := range rules {
		if r.Mode != ModeShadow {
			t.Fatalf("rule %q ships in mode %s, want %s, this ticket promotes nothing", r.ID, r.Mode, ModeShadow)
		}
	}
}
