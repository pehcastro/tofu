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
	set, err := resolveCatalog(replayTestPolicyRef)
	if err != nil {
		t.Fatalf("resolveCatalog: %v", err)
	}
	return set
}

func TestResolvePolicyReadsTheEmbeddedPolicyWhenTheProjectHasNoCatalog(t *testing.T) {
	pol, err := resolvePolicy(replayTestPolicyRef, toolGateBatteryInAScratchProject(t))
	if err != nil {
		t.Fatalf("resolvePolicy in a project with no catalog: %v", err)
	}
	if pol.Name != "tool_gate" || pol.PolicyVersion != 3 {
		t.Fatalf("resolved %s@%d, want tool_gate@3", pol.Name, pol.PolicyVersion)
	}
	if pol.File != "catalog/policy/"+replayTestPolicyRef+".yaml" {
		t.Fatalf("policy file = %q, want the one in the binary", pol.File)
	}
}

func TestResolvePolicyPrefersTheProjectPolicy(t *testing.T) {
	set := toolGateBatteryInAScratchProject(t)
	path := replayProjectPolicyPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir catalog/policy: %v", err)
	}
	if err := os.WriteFile(path, []byte(replayTestPolicyBody), 0o644); err != nil {
		t.Fatalf("writing the project policy: %v", err)
	}
	pol, err := resolvePolicy(replayTestPolicyRef, set)
	if err != nil {
		t.Fatalf("resolvePolicy: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if want := filepath.Join(wd, path); pol.File != want {
		t.Fatalf("policy file = %q, want the project's %q", pol.File, want)
	}
}

func TestResolvePolicyRefusesAPolicyWithNoVersion(t *testing.T) {
	_, err := resolvePolicy("tool_gate", toolGateBatteryInAScratchProject(t))
	if err == nil || !strings.Contains(err.Error(), "names no version") {
		t.Fatalf("err = %v, want a refusal naming the missing version", err)
	}
}
