package rules

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestParseRuleReadsAConditionAndAScopeAndBothAreOptional(t *testing.T) {
	withBoth, err := parseRule([]byte("id: em_dash\ndomain: general\nkind: structural\nchecker: em_dash\nconcern: code_rules\ncondition: (?i)changelog\nscope: library/**\n"), "library/general/rules/em_dash@1.yaml")
	if err != nil {
		t.Fatalf("parseRule: %v", err)
	}
	if withBoth.Trigger.AlwaysOn() {
		t.Fatalf("a rule declaring a condition and a scope reads as always on: %+v", withBoth.Trigger)
	}
	if withBoth.Trigger.condition.String() != "(?i)changelog" || withBoth.Trigger.scope != "library/**" {
		t.Fatalf("trigger = %+v, want the condition and the scope the file declares", withBoth.Trigger)
	}

	fires, why := withBoth.Trigger.firesFor(Task{Text: "update the CHANGELOG", Paths: []string{"library/general/rules/em_dash@1.yaml"}})
	if !fires {
		t.Fatalf("the rule did not fire for a task matching both parts: %s", why)
	}
	if !strings.Contains(why, "CHANGELOG") || !strings.Contains(why, "library/general/rules/em_dash@1.yaml") {
		t.Fatalf("why = %q, want the matched text and the reached path", why)
	}
}

func TestADeclaredLanguageReachesItsOwnRulesAndNoOther(t *testing.T) {
	typescript := Trigger{languages: []string{"typescript"}}
	scoped := Trigger{languages: []string{"typescript"}, scope: "src/**"}
	golang := Trigger{languages: []string{"go"}}
	cases := []struct {
		name    string
		trigger Trigger
		task    Task
		fires   bool
	}{
		{"declared, no path", typescript, Task{Text: "fix the failing build", Language: "typescript"}, true},
		{"declared another language", golang, Task{Text: "fix the failing build", Language: "typescript"}, false},
		{"a path still reaches its own language", golang, Task{Paths: []string{"main.go"}, Language: "typescript"}, true},
		{"the scope still has to match", scoped, Task{Paths: []string{"docs/x.md"}, Language: "typescript"}, false},
		{"nothing declared", typescript, Task{Text: "fix the failing build"}, false},
	}
	for _, c := range cases {
		if fires, why := c.trigger.firesFor(c.task); fires != c.fires {
			t.Errorf("%s: fires = %v, want %v: %s", c.name, fires, c.fires, why)
		}
	}
}

func TestALanguageListFiresForAnyListedLanguageAndNamesTheOneThatMatched(t *testing.T) {
	r, err := parseRule([]byte("id: frontend\ndomain: dev\nkind: human\nconcern: code_rules\nlanguage: typescript, svelte\ntext: x\n"), "frontend@1.yaml")
	if err != nil {
		t.Fatalf("parseRule: %v", err)
	}
	for file, want := range map[string]string{
		"src/App.svelte": "the language svelte reached src/App.svelte",
		"src/x.ts":       "the language typescript reached src/x.ts",
		"main.go":        "",
	} {
		fires, why := r.Trigger.firesFor(Task{Paths: []string{file}})
		if fires != (want != "") || (fires && why != want) {
			t.Errorf("%s: fires = %v, why %q, want %q", file, fires, why, want)
		}
	}
	if fires, why := r.Trigger.firesFor(Task{Language: "svelte"}); !fires || why != "the language svelte reached the language the sub-agent declares" {
		t.Errorf("a sub-agent declaring svelte: fires = %v, why %q", fires, why)
	}
	if _, err := parseRule([]byte("id: frontend\ndomain: dev\nkind: human\nconcern: code_rules\nlanguage: typescript, sveltee\ntext: x\n"), "frontend@1.yaml"); err == nil || !strings.Contains(err.Error(), "sveltee") {
		t.Errorf("a misspelt language in a list: err = %v, want a refusal naming it", err)
	}
}

func TestAScopeFiresForAnOwnedGlobInsideItAndNotForOneOutside(t *testing.T) {
	scoped := Trigger{scope: "src/**"}
	for owned, wants := range map[string]bool{"src/**": true, "src/ui/*.tsx": true, "docs/**": false} {
		if fires, why := scoped.firesFor(Task{Paths: []string{owned}}); fires != wants {
			t.Errorf("owning %s: fires = %v, want %v: %s", owned, fires, wants, why)
		}
	}
}

func TestLanguagesReadsTheFilesInTheFoldersATaskNames(t *testing.T) {
	root := t.TempDir()
	for file, body := range map[string]string{"internal/konst/konst.go": "package konst\n", "docs/guide.md": "# guide\n", "internal/deep/inner/x.rs": "fn main() {}\n"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name  string
		paths []string
		want  []string
	}{
		{"a named folder", []string{"internal/konst"}, []string{"go"}},
		{"a named file reads its folder", []string{"internal/konst/new.go"}, []string{"go"}},
		{"an absolute folder", []string{filepath.ToSlash(filepath.Join(root, "docs"))}, []string{"markdown"}},
		{"two folders, sorted once", []string{"docs/guide.md", "internal/konst", "internal/konst/konst.go"}, []string{"go", "markdown"}},
		{"a folder that does not exist", []string{"missing/dir"}, nil},
		{"a folder holding only folders", []string{"internal/deep"}, nil},
		{"no path", nil, nil},
	} {
		got, err := Languages(root, c.paths)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: Languages = %q, %v, want %q", c.name, got, err, c.want)
		}
	}
}

func TestALoneLeadsLanguagesReachTheLanguageRules(t *testing.T) {
	golang := Trigger{languages: []string{"go"}}
	for _, c := range []struct {
		task  Task
		fires bool
	}{
		{Task{Text: "rename konst.Foo", Languages: []string{"go"}}, true},
		{Task{Text: "rename konst.Foo", Languages: []string{"markdown"}}, false},
		{Task{Text: "rename konst.Foo"}, false},
	} {
		if fires, why := golang.firesFor(c.task); fires != c.fires {
			t.Errorf("%q: fires = %v, want %v: %s", c.task.Languages, fires, c.fires, why)
		}
	}
}

func TestKnownLanguage(t *testing.T) {
	if !KnownLanguage("typescript") || KnownLanguage("typscript") || KnownLanguage("") {
		t.Fatal("KnownLanguage should name typescript and nothing misspelt or empty")
	}
}

func TestParseRuleRefusesAnUnparseableConditionByNameAndByField(t *testing.T) {
	_, err := parseRule([]byte("id: em_dash\ndomain: general\nkind: structural\nchecker: em_dash\nconcern: code_rules\ncondition: (unclosed\n"), "library/general/rules/em_dash@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a condition that is not a regular expression")
	}
	for _, want := range []string{"library/general/rules/em_dash@1.yaml", "em_dash", "condition"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
}

func TestParseRuleRefusesAScopeTheMatcherCannotRead(t *testing.T) {
	_, err := parseRule([]byte("id: em_dash\ndomain: general\nkind: structural\nchecker: em_dash\nconcern: code_rules\nscope: library/[a-z]\n"), "library/general/rules/em_dash@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a scope the ownership matcher cannot read")
	}
	if !strings.Contains(err.Error(), "em_dash") || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("the refusal names neither the rule nor the field: %v", err)
	}
}

func TestAShippedRuleWithNoTriggerIsAlwaysOn(t *testing.T) {
	r := loadShippedRule(t, filepath.Join("..", "..", "..", "library", "general", "rules"), "em_dash")
	if !r.Trigger.AlwaysOn() {
		t.Fatalf("the shipped em_dash rule declares a trigger, and this ticket gives it none: %+v", r.Trigger)
	}
	index := Index([]Rule{r}, Task{})
	if !index[0].Fires {
		t.Fatalf("a rule with no trigger did not fire for a task naming nothing: %+v", index[0])
	}
	if index[0].Why != "always on, the rule declares no trigger" {
		t.Fatalf("why = %q, want the always on wording", index[0].Why)
	}
}

func TestIndexFiresEveryRuleThatMatchesAndHoldsBackTheRestWithAReason(t *testing.T) {
	rules, err := LoadFS(fstest.MapFS{
		"dev/rules/go_file@1.yaml":   {Data: []byte("id: go_file\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\nscope: internal/**/*.go\n")},
		"dev/rules/go_test@1.yaml":   {Data: []byte("id: go_test\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\nscope: internal/**/*_test.go\n")},
		"dev/rules/changelog@1.yaml": {Data: []byte("id: changelog\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\ncondition:(?i)\\bchangelog\\b\n")},
		"dev/rules/narrowed@1.yaml":  {Data: []byte("id: narrowed\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\ncondition:(?i)\\bchangelog\\b\nscope: bench/**\n")},
		"dev/rules/em_dash@1.yaml":   {Data: []byte("id: em_dash\ndomain: dev\nkind: structural\nchecker: em_dash\nconcern: output_shape\n")},
	}, "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}

	index := Index(rules, Task{Text: "write the changelog entry", Paths: []string{"internal/librarian/rules/trigger_test.go"}})

	fired := map[string]string{}
	held := map[string]string{}
	for _, m := range index {
		if m.Fires {
			fired[m.RuleID] = m.Why
			continue
		}
		held[m.RuleID] = m.Why
	}
	for _, id := range []string{"go_file", "go_test", "changelog", "em_dash"} {
		if _, both := fired[id]; !both {
			t.Fatalf("rule %q did not fire: %v", id, held[id])
		}
	}
	if len(held) != 1 || !strings.Contains(held["narrowed"], "bench/**") {
		t.Fatalf("held back = %v, want narrowed alone, named by the scope that reached nothing", held)
	}
}

func TestTheShippedIndexSaysWhatFiresForATaskAndWhy(t *testing.T) {
	rules, err := LoadDir(shippedLibrary)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	index := Index(rules, Task{Text: "add a table test for the loader", Paths: []string{"internal/librarian/rules/load_test.go", "internal/librarian/rules/load.go"}})
	why := map[string]string{}
	for i, m := range index {
		why[m.RuleID] = m.Why
		trigger := rules[i].Trigger
		reachesAnUnnamedGoTask := trigger.condition == nil && trigger.verb == VerbNone && (trigger.languages == nil || slices.Contains(trigger.languages, "go")) && trigger.frameworks == nil && trigger.touches == nil && trigger.role == RoleAny
		if m.Fires != reachesAnUnnamedGoTask {
			t.Fatalf("rule %q fires = %v for an unnamed task naming a go file and a test file, and its condition, task and language reach that task = %v: %s", m.RuleID, m.Fires, reachesAnUnnamedGoTask, m.Why)
		}
	}
	for id, want := range map[string]string{
		"em_dash":        "always on, the rule declares no trigger",
		"comments":       "the language go reached internal/librarian/rules/load_test.go",
		"test_assertion": "the scope **/*_test.go reached internal/librarian/rules/load_test.go",
	} {
		if why[id] != want {
			t.Fatalf("the index says %s fires because %q, want %q", id, why[id], want)
		}
	}

	for i, m := range Index(rules, Task{Text: "rewrite the changelog entry", Paths: []string{"CHANGELOG.md"}}) {
		if m.Fires != rules[i].Trigger.AlwaysOn() {
			t.Fatalf("rule %q fires = %v for a task naming one markdown file, and it is always on = %v: %s", m.RuleID, m.Fires, rules[i].Trigger.AlwaysOn(), m.Why)
		}
		if m.RuleID == "comments" && m.Why != "no path the task names is go" {
			t.Fatalf("the index says comments was held back because %q, want no path the task names is go", m.Why)
		}
	}
}

func TestTheShippedGoRulesReachGoFilesAndNothingElse(t *testing.T) {
	rules, err := LoadDir(shippedLibrary)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	scoped := map[string]Rule{}
	for _, r := range rules {
		if !r.Trigger.AlwaysOn() {
			scoped[r.ID] = r
		}
	}

	source := Task{Paths: []string{"internal/librarian/rules/trigger.go"}}
	test := Task{Paths: []string{"internal/librarian/rules/trigger_test.go"}}
	prose := Task{Paths: []string{"library/README.md"}}
	fires := func(id string, task Task) bool {
		r, shipped := scoped[id]
		if !shipped {
			t.Fatalf("rule %q ships without a trigger, want one", id)
		}
		on, _ := r.Trigger.firesFor(task)
		return on
	}
	goRules := map[string]bool{}
	for id, r := range scoped {
		if !slices.Equal(r.Trigger.languages, []string{"go"}) || r.Trigger.touches != nil {
			continue
		}
		if !fires(id, source) || !fires(id, test) || fires(id, prose) {
			t.Fatalf("the go rule %q does not reach exactly the go files", id)
		}
		goRules[id] = true
	}
	if !goRules["comments"] {
		t.Fatal("the comments rule does not ship as a go rule")
	}
	for _, id := range []string{"test_assertion", "test_mock_boundary", "test_boundary_cases", "skipped_test_budget", "flake_disagreement"} {
		if fires(id, source) || !fires(id, test) || fires(id, prose) {
			t.Fatalf("rule %q does not reach exactly the go test files", id)
		}
		goRules[id] = true
	}
	for id, r := range scoped {
		if goRules[id] {
			continue
		}
		for _, task := range []Task{source, test} {
			if on, why := r.Trigger.firesFor(task); on {
				t.Fatalf("rule %q is not one of the go rules and reaches %v: %s", id, task.Paths, why)
			}
		}
	}
}

func TestAQARuleScopedToTestFilesFiresForATestTaskBeforeTheFileIsNamed(t *testing.T) {
	var rules []Rule
	for _, declared := range []string{
		"id: qa_scoped\ndomain: qa\nalso_reaches: work_on_tests\nkind: human\nconcern: code_rules\nscope: \"**/*_test.go\"\ntext: x\n",
		"id: dev_scoped\ndomain: dev\nkind: human\nconcern: code_rules\nscope: \"**/*_test.go\"\ntext: x\n",
		"id: qa_conditioned\ndomain: qa\nalso_reaches: work_on_tests\nkind: human\nconcern: code_rules\ncondition: (?i)flaky\nscope: \"**/*_test.go\"\ntext: x\n",
	} {
		r, err := parseRule([]byte(declared), "x@1.yaml")
		if err != nil {
			t.Fatalf("parseRule: %v", err)
		}
		rules = append(rules, r)
	}
	for _, c := range []struct {
		task  Task
		fires []string
	}{
		{Task{Text: "add an end to end test for resume"}, []string{"qa_scoped"}},
		{Task{Text: "add a cache and a test that drives it", Paths: []string{"internal/tools/code/search/engine/symbols.go"}}, []string{"qa_scoped"}},
		{Task{Text: "rename konst.Foo"}, nil},
		{Task{Text: "rename the testing field", Paths: []string{"internal/konst/konst.go"}}, nil},
		{Task{Text: "fix the flaky one", Paths: []string{"internal/agent/loop/loop_test.go"}}, []string{"qa_scoped", "dev_scoped", "qa_conditioned"}},
	} {
		var fired []string
		for _, m := range IndexReaching(rules, DomainDev, c.task) {
			if m.Fires {
				fired = append(fired, m.RuleID)
			}
		}
		if !slices.Equal(fired, c.fires) {
			t.Errorf("%q holding %q fires %q, want %q", c.task.Text, c.task.Paths, fired, c.fires)
		}
	}
}

func TestThePathPicksTheFrameworkAndJevOrTheTouchesDecidesEachRule(t *testing.T) {
	r, err := parseRule([]byte("id: motion\ndomain: dev\nkind: human\nconcern: code_rules\nframework: gpui-ce\ntouches: (?i)animat|motion\ntext: x\n"), "motion@1.yaml")
	if err != nil {
		t.Fatalf("parseRule: %v", err)
	}
	gpui, panel := []string{"gpui-ce"}, []string{"crates/ui/src/panel.rs"}
	applies := map[string]Judged{"motion": {Applies: true, Why: "jev said 0.9"}}
	refuses := map[string]Judged{"motion": {Why: "jev said 0.1"}}
	for _, c := range []struct {
		task  Task
		fires bool
		why   string
	}{
		{Task{Role: RoleSubAgent, Text: "make crates/ui/src/panel.rs ease in", Paths: panel, Frameworks: gpui}, false,
			"the touches (?i)animat|motion decides, and it matches nothing in the writer's task or paths"},
		{Task{Role: RoleSubAgent, Text: "make crates/ui/src/panel.rs ease in", Paths: panel, Frameworks: gpui, Judged: applies}, true,
			"jev said 0.9 and a Cargo.toml lists gpui-ce"},
		{Task{Role: RoleSubAgent, Text: "make crates/ui/src/panel.rs ease in", Paths: panel, Frameworks: gpui, Judged: refuses}, false, "jev said 0.1"},
		{Task{Role: RoleSubAgent, Text: "make crates/ui/src/panel.rs ease in", Paths: panel, Judged: applies}, false,
			"no Cargo.toml the task reaches lists gpui-ce"},
		{Task{Role: RoleSubAgent, Text: "make the panel ease in", Frameworks: gpui, Judged: applies}, true, "jev said 0.9 and a Cargo.toml lists gpui-ce"},
		{Task{Role: RoleSubAgent, Text: "animate the panel", Frameworks: gpui}, true,
			"the touches (?i)animat|motion decides, and it matched \"animat\" and a Cargo.toml lists gpui-ce"},
		{Task{Role: RoleSubAgent, Text: "tidy the file you hold", Paths: []string{"crates/ui/src/motion.rs"}, Frameworks: gpui}, true,
			"the touches (?i)animat|motion decides, and it matched \"motion\" and a Cargo.toml lists gpui-ce"},
		{Task{Role: RoleSubAgent, Alone: true, Text: "animate the panel in docs/notes.md", Paths: []string{"docs/notes.md"}}, false,
			"no Cargo.toml the task reaches lists gpui-ce"},
		{Task{Role: RoleOrchestrator, Text: "animate the panel", Frameworks: gpui, Judged: applies}, true,
			"jev said 0.9 and a Cargo.toml lists gpui-ce, and a lead that may spawn writes code too, so Jev's judgment of the touches reaches it"},
		{Task{Role: RoleOrchestrator, Text: "animate the panel", Frameworks: gpui}, false,
			"Jev did not judge the touches (?i)animat|motion for a lead that may spawn, and the keyword alone never reaches one"},
		{Task{Role: RoleOrchestrator, Text: "what does the panel do", Frameworks: gpui, Judged: applies}, false,
			"the touches (?i)animat|motion matches nothing in the task or its paths, so Jev is not asked"},
		{Task{Role: RoleOrchestrator, Verb: VerbPlan, Text: "plan the panel", Frameworks: gpui}, true, "a Cargo.toml lists gpui-ce"},
	} {
		got := Index([]Rule{r}, c.task)[0]
		if got.Fires != c.fires || got.Why != c.why {
			t.Errorf("%q with paths %v: fires = %v, why %q, want %v, %q", c.task.Text, c.task.Paths, got.Fires, got.Why, c.fires, c.why)
		}
	}
	for _, c := range []struct {
		task  Task
		asked int
	}{
		{Task{Role: RoleSubAgent, Text: "make crates/ui/src/panel.rs ease in", Paths: panel, Frameworks: gpui}, 1},
		{Task{Role: RoleSubAgent, Text: "make crates/ui/src/panel.rs ease in", Paths: panel}, 0},
		{Task{Role: RoleOrchestrator, Text: "make the panel ease in", Frameworks: gpui}, 0},
		{Task{Role: RoleOrchestrator, Text: "animate the panel", Frameworks: gpui}, 1},
	} {
		if asked := AskedOfTheTask([]Rule{r}, DomainDev, c.task); len(asked) != c.asked {
			t.Errorf("%q as %s in %v asks jev about %d rules, want %d", c.task.Text, c.task.Role, c.task.Frameworks, len(asked), c.asked)
		}
	}
}
