package rule

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadFSReadsEveryRuleAndNamesTheDirectoryItActuallySitsIn(t *testing.T) {
	shipped := fstest.MapFS{
		"dev/go/rules/comments@1.yaml": {Data: []byte("id: comments\ndomain: dev\nkind: structural\nchecker: comments\n")},
		"general/rules/em_dash@1.yaml": {Data: []byte("id: em_dash\ndomain: general\nkind: structural\nchecker: em_dash\nmode: enforced\n")},
		"general/rules/notes.md":       {Data: []byte("not a rule\n")},
	}
	rules, err := LoadFS(shipped, "catalog")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want 2: %+v", len(rules), rules)
	}
	if rules[0].ID != "comments" || rules[1].Mode != ModeEnforced {
		t.Fatalf("LoadFS did not read the rules it was given: %+v", rules)
	}
	if rules[0].File != "catalog/dev/go/rules/comments@1.yaml" {
		t.Fatalf("comments says it came from %q, want catalog/dev/go/rules/comments@1.yaml", rules[0].File)
	}
	if rules[1].File != "catalog/general/rules/em_dash@1.yaml" {
		t.Fatalf("em_dash says it came from %q, want catalog/general/rules/em_dash@1.yaml", rules[1].File)
	}
}

func TestLoadFSSkipsARuleADecisionPointReads(t *testing.T) {
	rules, err := LoadFS(fstest.MapFS{
		"general/rules/em_dash@1.yaml":   {Data: []byte("id: em_dash\ndomain: general\nkind: structural\nchecker: em_dash\n")},
		"general/rules/tool_gate@1.yaml": {Data: []byte("name: tool_gate\ndomain: general\nkind: threshold\nrule_version: 1\n")},
	}, "catalog")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if len(rules) != 1 || rules[0].ID != "em_dash" {
		t.Fatalf("rules = %+v, want the structural rule alone", rules)
	}
}

func TestLoadFSNamesTheFileInAnError(t *testing.T) {
	_, err := LoadFS(fstest.MapFS{"general/rules/broken@1.yaml": {Data: []byte("id: broken\ndomain: general\nkind: rumour\nchecker: none\n")}}, "catalog")
	if err == nil {
		t.Fatal("LoadFS accepted a rule whose kind is not a kind")
	}
	if !strings.HasPrefix(err.Error(), "catalog/general/rules/broken@1.yaml") {
		t.Fatalf("the error does not start with the file it came from: %v", err)
	}
}

func TestParseRuleRefusesARuleWithNoDomainByName(t *testing.T) {
	_, err := parseRule([]byte("id: em_dash\nkind: structural\nchecker: em_dash\n"), "catalog/general/rules/em_dash@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a rule that declares no domain")
	}
	if !strings.HasPrefix(err.Error(), "catalog/general/rules/em_dash@1.yaml") || !strings.Contains(err.Error(), "no domain") {
		t.Fatalf("the refusal does not name the file and the field: %v", err)
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
	off, err := parseRule([]byte("id: test_boundary_cases\ndomain: qa\nkind: structural\nchecker: test_boundary_cases\nmode: off\n"), "someone-elses-project/rules/test_boundary_cases@1.yaml")
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

func TestLoadReadsTheMeasuredQARules(t *testing.T) {
	for _, name := range []string{"flake_disagreement@1.yaml", "skipped_test_budget@1.yaml"} {
		r, err := Load(filepath.Join("..", "..", "catalog", "qa", "general", "rules", name))
		if err != nil {
			t.Fatalf("Load %s: %v", name, err)
		}
		if r.Kind != KindMeasured || r.Domain != "qa" {
			t.Fatalf("%s is kind %q in domain %q, want %q in qa", name, r.Kind, r.Domain, KindMeasured)
		}
		if r.Measurement != "bench/testquality/flakerun" {
			t.Fatalf("%s measurement = %q, want bench/testquality/flakerun", name, r.Measurement)
		}
		if !strings.HasPrefix(r.Source, ".local/sources/qa-skills/") {
			t.Fatalf("%s source = %q, want a path under .local/sources/qa-skills/", name, r.Source)
		}
		if !strings.Contains(r.Evidence, "2202 test names over 70 packages") {
			t.Fatalf("%s evidence = %q, want the 2026-09-21 sweep it was measured on", name, r.Evidence)
		}
	}
}

func TestParseRuleRefusesAMeasuredRuleThatDeclaresAChecker(t *testing.T) {
	_, err := parseRule([]byte("id: flake_disagreement\ndomain: qa\nkind: measured\nmeasurement: bench/testquality/flakerun\nchecker: comments\nsource: s\nevidence: e\n"), "catalog/qa/general/rules/flake_disagreement@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a measured rule carrying a checker")
	}
	if !strings.Contains(err.Error(), "flake_disagreement") || !strings.Contains(err.Error(), "checker") {
		t.Fatalf("the refusal names neither the rule nor the field: %v", err)
	}
}

func TestParseRuleRefusesAStructuralRuleThatDeclaresAMeasurement(t *testing.T) {
	_, err := parseRule([]byte("id: comments\ndomain: dev\nkind: structural\nchecker: comments\nmeasurement: bench/testquality/flakerun\n"), "catalog/dev/go/rules/comments@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a structural rule carrying a measurement")
	}
	if !strings.Contains(err.Error(), "comments") || !strings.Contains(err.Error(), "measurement") {
		t.Fatalf("the refusal names neither the rule nor the field: %v", err)
	}
}

func TestLoadDirWalksEveryDomainAndSkipsTheRulesADecisionPointReads(t *testing.T) {
	rules, err := LoadDir(filepath.Join("..", "..", "catalog"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(rules) != 9 {
		t.Fatalf("rules = %d, want 9: %+v", len(rules), rules)
	}
	domains := map[string]int{}
	for _, r := range rules {
		if r.Mode != ModeShadow {
			t.Fatalf("rule %q ships in mode %s, want %s, this ticket promotes nothing", r.ID, r.Mode, ModeShadow)
		}
		if r.Domain == "" {
			t.Fatalf("rule %q ships with no domain: %+v", r.ID, r)
		}
		domains[r.Domain]++
	}
	if domains[DomainDev] == 0 || domains[DomainQA] == 0 || domains[DomainGeneral] == 0 {
		t.Fatalf("the shipped rules cover %v, want at least one in dev, qa and general", domains)
	}
}
