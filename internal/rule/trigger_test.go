package rule

import (
	"fmt"
	"path/filepath"
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
	r, err := Load(filepath.Join("..", "..", "library", "general", "rules", "em_dash@1.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
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

	index := Index(rules, Task{Text: "write the changelog entry", Paths: []string{"internal/rule/trigger_test.go"}})

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

func TestWriteIndexShowsEveryRuleItsVerdictAndTheReason(t *testing.T) {
	rules, err := LoadFS(fstest.MapFS{
		"dev/rules/comments@1.yaml": {Data: []byte("id: comments\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\nscope: internal/**/*.go\n")},
		"dev/rules/release@1.yaml":  {Data: []byte("id: release\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\ncondition:(?i)\\brelease\\b\n")},
	}, "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	out := &strings.Builder{}
	WriteIndex(out, Index(rules, Task{Text: "rename a field", Paths: []string{"internal/rule/trigger.go"}}))
	want := "1 of 2 rules fire\n" +
		"fires comments the scope internal/**/*.go reached internal/rule/trigger.go\n" +
		"      release  the condition (?i)\\brelease\\b matches nothing in the task\n"
	if out.String() != want {
		t.Fatalf("WriteIndex wrote\n%s\nwant\n%s", out, want)
	}
}

func TestTheShippedIndexSaysWhatFiresForATaskAndWhy(t *testing.T) {
	rules, err := LoadDir(shippedLibrary)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	index := Index(rules, Task{Text: "add a table test for the loader", Paths: []string{"internal/rule/load_test.go", "internal/rule/load.go"}})
	out := &strings.Builder{}
	WriteIndex(out, index)
	t.Log("\n" + out.String())

	unconditional := 0
	for i, m := range index {
		declaresNoCondition := rules[i].Trigger.condition == nil
		if m.Fires != declaresNoCondition {
			t.Fatalf("rule %q fires = %v for a task naming a go file and a test file, and it declares a condition = %v: %s", m.RuleID, m.Fires, !declaresNoCondition, m.Why)
		}
		if declaresNoCondition {
			unconditional++
		}
	}
	if !strings.HasPrefix(out.String(), fmt.Sprintf("%d of %d rules fire\n", unconditional, len(rules))) {
		t.Fatalf("the index opens with %q, want %d of %d for a task naming a go file and a test file", strings.SplitN(out.String(), "\n", 2)[0], unconditional, len(rules))
	}
	for _, want := range []string{
		"fires em_dash             always on, the rule declares no trigger",
		"fires comments            the language go reached internal/rule/load_test.go",
		"fires test_assertion      the scope **/*_test.go reached internal/rule/load_test.go",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the index does not carry the line %q", want)
		}
	}

	onProse := Index(rules, Task{Text: "rewrite the changelog entry", Paths: []string{"CHANGELOG.md"}})
	prose := &strings.Builder{}
	WriteIndex(prose, onProse)
	t.Log("\n" + prose.String())

	alwaysOn := 0
	for i, m := range onProse {
		if m.Fires != rules[i].Trigger.AlwaysOn() {
			t.Fatalf("rule %q fires = %v for a task naming one markdown file, and it is always on = %v: %s", m.RuleID, m.Fires, rules[i].Trigger.AlwaysOn(), m.Why)
		}
		if m.Fires {
			alwaysOn++
		}
	}
	if !strings.HasPrefix(prose.String(), fmt.Sprintf("%d of %d rules fire\n", alwaysOn, len(rules))) {
		t.Fatalf("the index opens with %q, want %d of %d for a task naming one markdown file", strings.SplitN(prose.String(), "\n", 2)[0], alwaysOn, len(rules))
	}
	if !strings.Contains(prose.String(), "      comments            no path the task names is go") {
		t.Fatalf("the index does not say why comments was held back:\n%s", prose)
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

	source := Task{Paths: []string{"internal/rule/trigger.go"}}
	test := Task{Paths: []string{"internal/rule/trigger_test.go"}}
	prose := Task{Paths: []string{"library/README.md"}}
	fires := func(id string, task Task) bool {
		r, shipped := scoped[id]
		if !shipped {
			t.Fatalf("rule %q ships without a trigger, want one", id)
		}
		on, _ := r.Trigger.firesFor(task)
		return on
	}
	if !fires("comments", source) || !fires("comments", test) || fires("comments", prose) {
		t.Fatal("the comments rule does not reach exactly the go files")
	}
	goRules := map[string]bool{"comments": true}
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
