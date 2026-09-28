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
