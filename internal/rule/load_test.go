package rule

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const shippedLibrary = "../../library"

func ruleFilesOnDisk(t *testing.T, library string) (loadable, decisionPoint []string) {
	t.Helper()
	err := filepath.WalkDir(library, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Base(filepath.Dir(name)) != "rules" || !strings.HasSuffix(name, ".yaml") {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "kind: "+ThresholdKind) {
			decisionPoint = append(decisionPoint, name)
			return nil
		}
		loadable = append(loadable, name)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", library, err)
	}
	return loadable, decisionPoint
}

func TestLoadFSReadsEveryRuleAndNamesTheDirectoryItActuallySitsIn(t *testing.T) {
	shipped := fstest.MapFS{
		"dev/go/rules/comments@1.yaml": {Data: []byte("id: comments\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\n")},
		"general/rules/em_dash@1.yaml": {Data: []byte("id: em_dash\ndomain: general\nkind: structural\nchecker: em_dash\nconcern: output_shape\nmode: enforced\n")},
		"general/rules/notes.md":       {Data: []byte("not a rule\n")},
	}
	rules, err := LoadFS(shipped, "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want 2: %+v", len(rules), rules)
	}
	if rules[0].ID != "comments" || rules[1].Mode != ModeEnforced {
		t.Fatalf("LoadFS did not read the rules it was given: %+v", rules)
	}
	if rules[0].File != "library/dev/go/rules/comments@1.yaml" {
		t.Fatalf("comments says it came from %q, want library/dev/go/rules/comments@1.yaml", rules[0].File)
	}
	if rules[1].File != "library/general/rules/em_dash@1.yaml" {
		t.Fatalf("em_dash says it came from %q, want library/general/rules/em_dash@1.yaml", rules[1].File)
	}
}

func TestLoadFSSkipsARuleADecisionPointReads(t *testing.T) {
	rules, err := LoadFS(fstest.MapFS{
		"general/rules/em_dash@1.yaml":   {Data: []byte("id: em_dash\ndomain: general\nkind: structural\nchecker: em_dash\nconcern: output_shape\n")},
		"general/rules/tool_gate@1.yaml": {Data: []byte("name: tool_gate\ndomain: general\nkind: threshold\nrule_version: 1\n")},
	}, "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if len(rules) != 1 || rules[0].ID != "em_dash" {
		t.Fatalf("rules = %+v, want the structural rule alone", rules)
	}
}

func TestLoadFSNamesTheFileInAnError(t *testing.T) {
	_, err := LoadFS(fstest.MapFS{"general/rules/broken@1.yaml": {Data: []byte("id: broken\ndomain: general\nkind: rumour\nchecker: none\n")}}, "library")
	if err == nil {
		t.Fatal("LoadFS accepted a rule whose kind is not a kind")
	}
	if !strings.HasPrefix(err.Error(), "library/general/rules/broken@1.yaml") {
		t.Fatalf("the error does not start with the file it came from: %v", err)
	}
}

func TestLoadFSNamesTheDirectoryAndTheConventionWhenARuleFailsToParse(t *testing.T) {
	_, err := LoadFS(fstest.MapFS{"tools/shell/rules/broken@1.yaml": {Data: []byte("id: broken\ndomain: general\nkind: rumour\nchecker: none\n")}}, "library")
	if err == nil {
		t.Fatal("LoadFS accepted a rule whose kind is not a kind")
	}
	if !strings.Contains(err.Error(), "library/tools/shell/rules") {
		t.Fatalf("the error does not name the directory the file was read from: %v", err)
	}
	if !strings.Contains(err.Error(), `was read as a rule because its directory is named "rules"`) {
		t.Fatalf("the error does not say the directory's name is why the file was read: %v", err)
	}
}

func TestParseRuleRefusesARuleWithNoDomainByName(t *testing.T) {
	_, err := parseRule([]byte("id: em_dash\nkind: structural\nchecker: em_dash\n"), "library/general/rules/em_dash@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a rule that declares no domain")
	}
	if !strings.HasPrefix(err.Error(), "library/general/rules/em_dash@1.yaml") || !strings.Contains(err.Error(), "no domain") {
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
	off, err := parseRule([]byte("id: test_boundary_cases\ndomain: qa\nkind: structural\nchecker: test_boundary_cases\nconcern: code_rules\nmode: off\n"), "someone-elses-project/rules/test_boundary_cases@1.yaml")
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
		r, err := Load(filepath.Join("..", "..", "library", "qa", "general", "rules", name))
		if err != nil {
			t.Fatalf("Load %s: %v", name, err)
		}
		if r.Kind != KindMeasured || r.Domain != "qa" {
			t.Fatalf("%s is kind %q in domain %q, want %q in qa", name, r.Kind, r.Domain, KindMeasured)
		}
		if r.Measurement != "bench/testquality/flakerun" {
			t.Fatalf("%s measurement = %q, want bench/testquality/flakerun", name, r.Measurement)
		}
		if !strings.HasPrefix(r.Source, "library/qa/references/") {
			t.Fatalf("%s source = %q, want a reference under library/qa/references/", name, r.Source)
		}
		if !strings.Contains(r.Evidence, "2202 test names over 70 packages") {
			t.Fatalf("%s evidence = %q, want the 2026-09-21 sweep it was measured on", name, r.Evidence)
		}
	}
}

func TestParseRuleRefusesAMeasuredRuleThatDeclaresAChecker(t *testing.T) {
	_, err := parseRule([]byte("id: flake_disagreement\ndomain: qa\nkind: measured\nconcern: code_rules\nmeasurement: bench/testquality/flakerun\nchecker: comments\nsource: s\nevidence: e\n"), "library/qa/general/rules/flake_disagreement@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a measured rule carrying a checker")
	}
	if !strings.Contains(err.Error(), "flake_disagreement") || !strings.Contains(err.Error(), "checker") {
		t.Fatalf("the refusal names neither the rule nor the field: %v", err)
	}
}

func TestParseRuleRefusesEnforcedOnARuleNoCheckerCanAct(t *testing.T) {
	for _, one := range []struct {
		kind string
		body string
	}{
		{kind: string(KindMeasured), body: "id: flake_disagreement\ndomain: qa\nkind: measured\nconcern: code_rules\nmeasurement: bench/testquality/flakerun\nsource: s\nevidence: e\nmode: enforced\n"},
		{kind: string(KindHuman), body: "id: quote\ndomain: general\nkind: human\nconcern: task_shaping\ntext: read the turn\nmode: enforced\n"},
	} {
		_, err := parseRule([]byte(one.body), "library/"+one.kind+".yaml")
		if err == nil {
			t.Fatalf("parseRule accepted mode enforced on a %s rule, which has no checker and so can never block", one.kind)
		}
		if !strings.Contains(err.Error(), "enforced") || !strings.Contains(err.Error(), "checker") {
			t.Fatalf("the refusal on a %s rule names neither the mode nor the missing checker: %v", one.kind, err)
		}
	}
}

func TestParseRuleKeepsOffOnARuleNoCheckerCanActBecauseLayerDropsIt(t *testing.T) {
	r, err := parseRule([]byte("id: quote\ndomain: general\nkind: human\nconcern: task_shaping\ntext: read the turn\nmode: off\n"), "project/quote@1.yaml")
	if err != nil {
		t.Fatalf("parseRule refused mode off on a human rule, and off is how a project drops one: %v", err)
	}
	if kept := Layer([]Rule{{ID: "quote", Kind: KindHuman, Mode: ModeShadow}}, []Rule{r}); len(kept) != 0 {
		t.Fatalf("Layer kept %+v after the project turned the rule off", kept)
	}
}

func TestParseRuleRefusesAStructuralRuleThatDeclaresAMeasurement(t *testing.T) {
	_, err := parseRule([]byte("id: comments\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\nmeasurement: bench/testquality/flakerun\n"), "library/dev/go/rules/comments@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a structural rule carrying a measurement")
	}
	if !strings.Contains(err.Error(), "comments") || !strings.Contains(err.Error(), "measurement") {
		t.Fatalf("the refusal names neither the rule nor the field: %v", err)
	}
}

func TestLoadDirWalksEveryDomainAndSkipsTheRulesADecisionPointReads(t *testing.T) {
	rules, err := LoadDir(shippedLibrary)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	loadable, decisionPoint := ruleFilesOnDisk(t, shippedLibrary)
	if len(rules) != len(loadable) {
		t.Fatalf("rules = %d, want the %d rule files on disk: %v", len(rules), len(loadable), loadable)
	}
	if len(decisionPoint) == 0 {
		t.Fatal("no rule file on disk declares the threshold kind, so nothing here exercises the skip")
	}
	for _, skipped := range decisionPoint {
		for _, r := range rules {
			if r.File == skipped {
				t.Fatalf("LoadDir returned %s, which a decision point reads", skipped)
			}
		}
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

func TestAnAddedRuleMovesEveryCountAndBreaksNothingElse(t *testing.T) {
	shipped, err := LoadDir(shippedLibrary)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	grown := t.TempDir()
	if err := os.CopyFS(grown, os.DirFS(shippedLibrary)); err != nil {
		t.Fatalf("copying the shipped library: %v", err)
	}
	if err := os.WriteFile(filepath.Join(grown, "general", "rules", "eleventh@1.yaml"), []byte("id: eleventh\ndomain: general\nkind: structural\nchecker: em_dash\nconcern: output_shape\n"), 0o644); err != nil {
		t.Fatalf("writing the added rule: %v", err)
	}

	grownRules, err := LoadDir(grown)
	if err != nil {
		t.Fatalf("LoadDir on the grown library: %v", err)
	}
	if len(grownRules) != len(shipped)+1 {
		t.Fatalf("the grown library loads %d rules, want the %d shipped ones and the one added", len(grownRules), len(shipped))
	}

	task := Task{Text: "add a table test for the loader", Paths: []string{"internal/rule/load_test.go"}}
	fired := 0
	for _, m := range Index(shipped, task) {
		if m.Fires {
			fired++
		}
	}
	out := &strings.Builder{}
	WriteIndex(out, Index(grownRules, task))
	if !strings.HasPrefix(out.String(), fmt.Sprintf("%d of %d rules fire\n", fired+1, len(shipped)+1)) {
		t.Fatalf("the grown index opens with %q, want %d of %d", strings.SplitN(out.String(), "\n", 2)[0], fired+1, len(shipped)+1)
	}
}
