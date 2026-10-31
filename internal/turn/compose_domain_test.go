package turn

import (
	"slices"
	"strings"
	"testing"

	shipped "tofu/library"

	"tofu/internal/rule"
	"tofu/internal/subagent"
)

func composedRuleIDs(t *testing.T, agent, task string, paths ...string) []string {
	t.Helper()
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	spec := ComposeSpec{Task: task, Paths: paths, Environment: "<env/>", ToolGuidance: "read reads a whole file", Rules: rules}
	if agent != "" {
		found := subagent.Definitions(subagent.Scan{Library: shipped.Files()})
		index := slices.IndexFunc(found.Definitions, func(d subagent.Definition) bool { return d.Name == agent })
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

func TestATestRuleReachesADevWhoseWorkTouchesTests(t *testing.T) {
	testRules := []string{rule.DomainQA + "/e2e_first", rule.DomainQA + "/failure_modes_first", rule.DomainQA + "/no_unit_test_after_code"}
	for _, c := range []struct {
		agent, task string
		paths       []string
		wants       bool
	}{
		{"ts-dev", "add tests for the parser", nil, true},
		{"ts-dev", "rename a field", []string{"src/app.ts"}, false},
		{"ts-dev", "rename a field", []string{"src/__tests__/app.ts"}, true},
		{"ts-dev", "rename a field in src/app.spec.ts", nil, true},
		{"ts-dev", "rename the testing field", nil, false},
		{"qa", "add tests for the parser", nil, true},
		{"qa", "rename a field", []string{"src/app.ts"}, true},
		{"", "add tests for the parser", nil, true},
		{"", "rename a field", []string{"src/app.ts"}, false},
	} {
		got := composedRuleIDs(t, c.agent, c.task, c.paths...)
		for _, id := range testRules {
			if slices.Contains(got, id) != c.wants {
				t.Errorf("%q on %q holding %q: wants %s %v, got %q", c.agent, c.task, c.paths, id, c.wants, got)
			}
		}
		if c.agent != "qa" && slices.Contains(got, rule.DomainQA+"/coverage_split") {
			t.Errorf("%q on %q carries coverage_split, which stays qa only", c.agent, c.task)
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

func TestEveryRuleTheQAAgentDoesNotFireIsHeldBackWithAReason(t *testing.T) {
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	found := subagent.Definitions(subagent.Scan{Library: shipped.Files()})
	at := slices.IndexFunc(found.Definitions, func(d subagent.Definition) bool { return d.Name == "qa" })
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
