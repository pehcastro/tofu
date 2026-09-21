package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func toolGateBatteryInAScratchProject(t *testing.T) battery {
	t.Helper()
	t.Chdir(t.TempDir())
	set, err := resolveCatalog(replayTestRuleRef)
	if err != nil {
		t.Fatalf("resolveCatalog: %v", err)
	}
	return set
}

func TestResolveRuleReadsTheEmbeddedRuleWhenTheProjectHasNoCatalog(t *testing.T) {
	pol, err := resolveRule(replayTestRuleRef, toolGateBatteryInAScratchProject(t))
	if err != nil {
		t.Fatalf("resolveRule in a project with no catalog: %v", err)
	}
	if pol.Name != "tool_gate" || pol.RuleVersion != 3 {
		t.Fatalf("resolved %s@%d, want tool_gate@3", pol.Name, pol.RuleVersion)
	}
	if pol.File != "catalog/general/rules/"+replayTestRuleRef+".yaml" {
		t.Fatalf("rule file = %q, want the one in the binary", pol.File)
	}
}

func TestResolveRulePrefersTheProjectRule(t *testing.T) {
	set := toolGateBatteryInAScratchProject(t)
	path := replayProjectRulePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir catalog/general/rules: %v", err)
	}
	if err := os.WriteFile(path, []byte(replayTestRuleBody), 0o644); err != nil {
		t.Fatalf("writing the project rule: %v", err)
	}
	pol, err := resolveRule(replayTestRuleRef, set)
	if err != nil {
		t.Fatalf("resolveRule: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if want := filepath.Join(wd, path); pol.File != want {
		t.Fatalf("rule file = %q, want the project's %q", pol.File, want)
	}
}

func TestResolveRuleRefusesARuleWithNoVersion(t *testing.T) {
	_, err := resolveRule("tool_gate", toolGateBatteryInAScratchProject(t))
	if err == nil || !strings.Contains(err.Error(), "names no version") {
		t.Fatalf("err = %v, want a refusal naming the missing version", err)
	}
}
