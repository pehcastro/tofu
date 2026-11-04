package loop

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	shipped "tofu/library"

	"tofu/internal/agent/subagents"
	rule "tofu/internal/librarian/rules"
)

func composedRuleIDs(t *testing.T, agent, task string, paths ...string) []string {
	t.Helper()
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	spec := ComposeSpec{Task: task, Paths: paths, Environment: "<env/>", ToolGuidance: "read reads a whole file", Rules: rules}
	if agent != "" {
		found := subagents.Definitions(subagents.Scan{Library: shipped.Files()})
		index := slices.IndexFunc(found.Definitions, func(d subagents.Definition) bool { return d.Name == agent })
		if index < 0 {
			t.Fatalf("the library ships no sub-agent named %s: %+v", agent, found.Broken)
		}
		spec.Agent = found.Definitions[index]
	}
	composed, err := Compose(spec)
	if err != nil {
		t.Fatalf("Compose for %q: %v", agent, err)
	}
	domainOf := map[string]string{}
	for _, loaded := range rules {
		domainOf[loaded.ID] = loaded.Domain
	}
	var ids []string
	for _, part := range composed.Parts {
		if part.RuleID != "" {
			ids = append(ids, domainOf[part.RuleID]+"/"+part.RuleID)
		}
	}
	return ids
}

func TestARuleReachesOnlyTheAgentsOfItsDomain(t *testing.T) {
	qaRule := func(id string) bool { return strings.HasPrefix(id, rule.DomainQA+"/") }
	tsRule := func(id string) bool { return strings.Contains(id, "/ts_") }

	orchestrator := composedRuleIDs(t, "", "check the change")
	if slices.ContainsFunc(orchestrator, qaRule) {
		t.Errorf("the orchestrator carries a qa rule: %q", orchestrator)
	}
	qa := composedRuleIDs(t, "qa", "check the change")
	if !slices.Contains(qa, rule.DomainQA+"/coverage_split") || slices.ContainsFunc(qa, tsRule) {
		t.Errorf("qa wants coverage_split and no ts_ rule, got %q", qa)
	}
	tsDev := composedRuleIDs(t, "ts-dev", "check the change")
	if !slices.ContainsFunc(tsDev, tsRule) || slices.ContainsFunc(tsDev, qaRule) {
		t.Errorf("ts-dev wants the ts rules and no qa rule, got %q", tsDev)
	}
}

func TestEveryQARuleReachesADevWhoseWorkTouchesTests(t *testing.T) {
	alwaysOn := []string{"coverage_split", "e2e_first", "failure_modes_first", "no_unit_test_after_code"}
	every := slices.Concat(alwaysOn, []string{"flake_disagreement", "skipped_test_budget", "test_assertion", "test_boundary_cases", "test_mock_boundary"})
	slices.Sort(every)
	for _, c := range []struct {
		agent, task string
		paths       []string
		wants       []string
	}{
		{"go-dev", "add an end to end test for resume", nil, every},
		{"go-dev", "rename konst.Foo", nil, nil},
		{"go-dev", "fix the flaky test", []string{"internal/turn/loop_test.go"}, every},
		{"ts-dev", "add tests for the parser", nil, every},
		{"ts-dev", "rename a field", []string{"src/app.ts"}, nil},
		{"ts-dev", "rename a field", []string{"src/__tests__/app.ts"}, every},
		{"ts-dev", "rename a field in src/app.spec.ts", nil, every},
		{"ts-dev", "rename the testing field", nil, nil},
		{"qa", "add tests for the parser", nil, every},
		{"qa", "rename a field", []string{"src/app.ts"}, alwaysOn},
		{"", "add tests for the parser", nil, every},
		{"", "rename a field", []string{"src/app.ts"}, nil},
	} {
		var got []string
		for _, id := range composedRuleIDs(t, c.agent, c.task, c.paths...) {
			if qa, isQA := strings.CutPrefix(id, rule.DomainQA+"/"); isQA {
				got = append(got, qa)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, c.wants) {
			t.Errorf("%q on %q holding %q fires the qa rules %q, want %q", c.agent, c.task, c.paths, got, c.wants)
		}
	}
}

func TestALeadThatPlansReceivesTheQAAndWriterRulesInTheTaskHalf(t *testing.T) {
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	planning := []string{"e2e_first", "failure_modes_first", "test_boundary_cases", "read_first", "design_first"}
	for _, c := range []struct {
		task  string
		plans bool
	}{
		{"write a ticket for the theme picker", true},
		{"Write the Epic for themes,", true},
		{"plan the theme system", true},
		{"write a ticket for internal/turn/loop.go", true},
		{"fix the nil check in internal/turn/loop.go", false},
		{"write tickets/README.md", false},
		{"rename the field in plan.md", false},
	} {
		composed, err := Compose(ComposeSpec{Task: c.task, Environment: "<env/>", ToolGuidance: "read reads a whole file", Rules: rules, Role: rule.RoleOrchestrator})
		if err != nil {
			t.Fatalf("Compose %q: %v", c.task, err)
		}
		if (composed.Task.Verb == rule.VerbPlan) != c.plans {
			t.Errorf("%q: the verb is %q, want plan %v", c.task, composed.Task.Verb, c.plans)
		}
		fired := map[string]PromptPart{}
		for _, part := range composed.Parts {
			if part.RuleID != "" {
				fired[part.RuleID] = part
			}
		}
		for _, id := range planning {
			part, fires := fired[id]
			if fires != c.plans || fires && !part.ByTask {
				t.Errorf("%q: %s fires %v by task %v, want fires %v and by task", c.task, id, fires, part.ByTask, c.plans)
			}
			if strings.Contains(composed.Head(), "from the rule "+id+"]") {
				t.Errorf("%q: %s is in the frozen head", c.task, id)
			}
		}
		if _, fires := fired["sub_agent_boundaries"]; fires {
			t.Errorf("%q: a sub-agent rule reached the lead", c.task)
		}
		if len(fired)+len(composed.HeldBack) != len(rules) {
			t.Errorf("%q: %d fire and %d are held back, of %d loaded", c.task, len(fired), len(composed.HeldBack), len(rules))
		}
		for _, held := range composed.HeldBack {
			if held.Why == "" {
				t.Errorf("%q: %s is held back with no reason", c.task, held.RuleID)
			}
		}
	}
}

func TestALeadAloneGetsTheCodingRulesAndALeadThatSpawnsDoesNot(t *testing.T) {
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	goRules := []string{"comments", "go_wrap_errors", "go_no_blank_error"}
	order := []string{"design_first", "read_first"}
	testOrder := []string{"failure_modes_first", "no_unit_test_after_code"}
	for _, c := range []struct {
		task          string
		alone         bool
		languages     []string
		frameworks    []string
		fire, notFire []string
	}{
		{"fix the nil check in internal/turn/loop.go and add tests", true, nil, nil, slices.Concat(goRules, order, testOrder), nil},
		{"fix the nil check in internal/turn/loop.go and add tests", false, nil, nil, nil, slices.Concat(goRules, order, testOrder)},
		{"rename konst.Foo to konst.Bar in internal/konst", true, []string{"go"}, nil, slices.Concat(goRules, order), testOrder},
		{"rename konst.Foo to konst.Bar in internal/konst", false, []string{"go"}, nil, nil, slices.Concat(goRules, order)},
		{"rename the title in docs/guide", true, []string{"markdown"}, nil, order, goRules},
		{"fix the button in src/app.tsx", true, nil, []string{"react"}, order, []string{"fe_dialog"}},
		{"fix the dialog in src/app.tsx", true, nil, []string{"react"}, []string{"fe_dialog"}, nil},
	} {
		composed, err := Compose(ComposeSpec{Task: c.task, Environment: "<env/>", ToolGuidance: "read reads a whole file", Rules: rules, Role: rule.RoleOrchestrator,
			Alone: c.alone, Languages: c.languages, Frameworks: c.frameworks})
		if err != nil {
			t.Fatalf("Compose %q: %v", c.task, err)
		}
		fired := map[string]bool{}
		for _, part := range composed.Parts {
			fired[part.RuleID] = part.RuleID != ""
		}
		for _, id := range c.fire {
			if !fired[id] {
				t.Errorf("%q alone %v in %q: %s is held back, want it fired", c.task, c.alone, c.languages, id)
			}
		}
		for _, id := range c.notFire {
			if fired[id] {
				t.Errorf("%q alone %v in %q: %s fired, want it held back", c.task, c.alone, c.languages, id)
			}
		}
	}
}

func TestALeadWorksAloneOnOneNamedFolderAndAGoSelectorIsNotAFile(t *testing.T) {
	dir := t.TempDir()
	for _, folder := range []string{"internal/konst", "internal/other"} {
		if err := os.MkdirAll(filepath.Join(dir, folder), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "internal/konst/konst.go"), []byte("package konst\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for task, alone := range map[string]bool{
		"rename konst.Foo to konst.Bar in internal/konst":     true,
		"fix the typo in internal/konst/konst.go":             true,
		"move internal/konst/konst.go into internal/other":    false,
		"make the desk faster":                                false,
		"rename konst.Foo to konst.Bar":                       false,
		"rename konst.Foo in internal/konst with a sub-agent": false,
	} {
		if got := LeadWorksAlone(dir, task, nil); got != alone {
			t.Errorf("%q: alone %v, want %v", task, got, alone)
		}
	}
	for task, paths := range map[string][]string{
		"rename konst.Foo to konst.Bar in internal/konst": {"internal/konst"},
		"fix main.go and README.md and app.tsx":           {"main.go", "README.md", "app.tsx"},
	} {
		if got := TaskNamed(task).Paths; !slices.Equal(got, paths) {
			t.Errorf("%q names %q, want %q", task, got, paths)
		}
	}
}

func TestEveryRuleTheQAAgentDoesNotFireIsHeldBackWithAReason(t *testing.T) {
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	found := subagents.Definitions(subagents.Scan{Library: shipped.Files()})
	at := slices.IndexFunc(found.Definitions, func(d subagents.Definition) bool { return d.Name == "qa" })
	composed, err := Compose(ComposeSpec{Task: "check the change", Environment: "<env/>", ToolGuidance: "g", Rules: rules, Role: rule.RoleSubAgent, Agent: found.Definitions[at]})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	fired := 0
	for _, part := range composed.Parts {
		if part.RuleID != "" {
			fired++
		}
	}
	domainHeld := slices.ContainsFunc(composed.HeldBack, func(m rule.Match) bool { return strings.Contains(m.Why, "a dev rule") })
	if fired+len(composed.HeldBack) != len(rules) || !domainHeld {
		t.Errorf("%d fire and %d are held back of %d loaded, a dev rule held by domain %v", fired, len(composed.HeldBack), len(rules), domainHeld)
	}
}
