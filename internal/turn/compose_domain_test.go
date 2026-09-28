package turn

import (
	"slices"
	"strings"
	"testing"

	shipped "tofu/library"

	"tofu/internal/rule"
	"tofu/internal/subagent"
)

func composedRuleIDs(t *testing.T, agent string) []string {
	t.Helper()
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	spec := ComposeSpec{Task: "check the change", Environment: "<env/>", ToolGuidance: "read reads a whole file", Rules: rules}
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

	orchestrator := composedRuleIDs(t, "")
	if slices.ContainsFunc(orchestrator, qaRule) {
		t.Errorf("the orchestrator carries a qa rule: %q", orchestrator)
	}
	qa := composedRuleIDs(t, "qa")
	if !slices.Contains(qa, rule.DomainQA+"/coverage_split") || slices.ContainsFunc(qa, tsRule) {
		t.Errorf("qa wants coverage_split and no ts_ rule, got %q", qa)
	}
	tsDev := composedRuleIDs(t, "ts-dev")
	if !slices.ContainsFunc(tsDev, tsRule) || slices.ContainsFunc(tsDev, qaRule) {
		t.Errorf("ts-dev wants the ts rules and no qa rule, got %q", tsDev)
	}
}
