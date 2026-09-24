package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/gate"
)

func toolGateBatteryInAScratchProject(t *testing.T) battery {
	t.Helper()
	t.Chdir(t.TempDir())
	set, err := resolveLibrary(replayTestRuleRef, "")
	if err != nil {
		t.Fatalf("resolveLibrary: %v", err)
	}
	return set
}

func TestResolveRuleReadsTheEmbeddedRuleWhenTheProjectHasNoLibrary(t *testing.T) {
	pol, err := resolveRule(replayTestRuleRef, toolGateBatteryInAScratchProject(t))
	if err != nil {
		t.Fatalf("resolveRule in a project with no library: %v", err)
	}
	if pol.Name != "tool_gate" || pol.RuleVersion != 3 {
		t.Fatalf("resolved %s@%d, want tool_gate@3", pol.Name, pol.RuleVersion)
	}
	if pol.File != "library/general/rules/"+replayTestRuleRef+".yaml" {
		t.Fatalf("rule file = %q, want the one in the binary", pol.File)
	}
}

func TestResolveRulePrefersTheProjectRule(t *testing.T) {
	set := toolGateBatteryInAScratchProject(t)
	path := replayProjectRulePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir library/general/rules: %v", err)
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

func TestLoadRulePointReadsTheNamedProjectRatherThanTheWorkingOne(t *testing.T) {
	elsewhere := t.TempDir()
	path := filepath.Join(elsewhere, replayProjectRulePath())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir library/general/rules: %v", err)
	}
	if err := os.WriteFile(path, []byte(replayTestRuleBody), 0o644); err != nil {
		t.Fatalf("writing the project rule: %v", err)
	}
	t.Chdir(t.TempDir())
	pol, origin, err := loadRulePoint(replayTestRuleRef, elsewhere)
	if err != nil {
		t.Fatalf("loadRulePoint: %v", err)
	}
	if origin != gate.OriginProject || pol.File != path {
		t.Fatalf("origin = %q file = %q, want the project rule at %q", origin, pol.File, path)
	}
}

func TestResolveRuleRefusesARuleWithNoVersion(t *testing.T) {
	_, err := resolveRule("tool_gate", toolGateBatteryInAScratchProject(t))
	if err == nil || !strings.Contains(err.Error(), "names no version") {
		t.Fatalf("err = %v, want a refusal naming the missing version", err)
	}
}

const replayTestRuleRef = "tool_gate@3"

const replayTestRuleBody = `name: tool_gate
domain: general
kind: threshold
rule_version: 3
questions: tool_gate
questions_version: 3
notes: fixture for BOJI-033, the project override BOJI-136 replays against

risk_question: risk
approval_question: approval
user_requested_question: user_requested
from_untrusted_question: from_untrusted

thresholds:
  risk_ask_at: 1.5
  risk_deny_at: 2.5
  user_requested_relax_at: 0.85
  approval_relax_at: 0.15
  from_untrusted_block_at: 0.5
`

func replayProjectRulePath() string {
	return filepath.Join("library", "general", "rules", replayTestRuleRef+".yaml")
}
