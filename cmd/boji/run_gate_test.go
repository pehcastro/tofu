package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	catalogpolicy "boji/catalog/policy"
	"boji/internal/judge/ledger"
	"boji/internal/judge/policy"
	"boji/internal/sys"
)

const gateFixtureBuild = "typesafe/jev-1.13-20260917"

func writeGatePolicyFixture(t *testing.T) policy.Thresholds {
	t.Helper()
	catalogDir, err := sys.CatalogDir()
	if err != nil {
		t.Fatalf("catalog dir: %v", err)
	}
	path := filepath.Join(catalogDir, "policy", runGatePoint+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "name: tool_gate\npolicy_version: 3\nquestions: tool_gate\nquestions_version: 3\n" +
		"mode: enforced\nsample_floor: 300\n" +
		"risk_question: risk\napproval_question: approval\n" +
		"user_requested_question: user_requested\nfrom_untrusted_question: from_untrusted\n" +
		"thresholds:\n  risk_ask_at: 1.5\n  risk_deny_at: 2.5\n" +
		"  user_requested_relax_at: 0.85\n  approval_relax_at: 0.15\n  from_untrusted_block_at: 0.5\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the policy fixture: %v", err)
	}
	return policy.Thresholds{
		RiskAskAt:            1.5,
		RiskDenyAt:           2.5,
		UserRequestedRelaxAt: 0.85,
		ApprovalRelaxAt:      0.15,
		FromUntrustedBlockAt: 0.5,
	}
}

func writeGateLockFixture(t *testing.T, build string) policy.Thresholds {
	t.Helper()
	calibDir, err := sys.CalibrationDir()
	if err != nil {
		t.Fatalf("calibration dir: %v", err)
	}
	if err := os.MkdirAll(calibDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := fmt.Sprintf(""+
		"policy: tool_gate\npolicy_version: 3\nquestions: tool_gate\nquestions_version: 3\n"+
		"build: %s\nn_fit: 500\nn_verify: 400\n"+
		"thresholds:\n  risk_ask_at: 1.25\n  risk_deny_at: 2.75\n"+
		"  user_requested_relax_at: 0.9\n  approval_relax_at: 0.2\n  from_untrusted_block_at: 0.4\n", build)
	path := filepath.Join(calibDir, runGatePoint+".lock")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the lock fixture: %v", err)
	}
	return policy.Thresholds{
		RiskAskAt:            1.25,
		RiskDenyAt:           2.75,
		UserRequestedRelaxAt: 0.9,
		ApprovalRelaxAt:      0.2,
		FromUntrustedBlockAt: 0.4,
	}
}

func writeGateLedgerRowFixture(t *testing.T) {
	t.Helper()
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger dir: %v", err)
	}
	row := ledger.Row{Point: "tool_gate", Questions: "tool_gate", Version: 3, Build: gateFixtureBuild}
	if _, err := ledger.NewWriter(dir).Append(row); err != nil {
		t.Fatalf("writing the ledger fixture: %v", err)
	}
}

func gateThresholds(t *testing.T, dir string) policy.Thresholds {
	t.Helper()
	gate, err := newToolGate(dir)
	if err != nil {
		t.Fatalf("building the gate: %v", err)
	}
	if gate.set.Policy == nil {
		t.Fatal("the gate carries no policy")
	}
	return gate.set.Policy.Thresholds
}

func gateThresholdsAsDoctorReadsThem(point doctorPolicy) policy.Thresholds {
	return policy.Thresholds{
		RiskAskAt:            point.Thresholds.RiskAskAt,
		RiskDenyAt:           point.Thresholds.RiskDenyAt,
		UserRequestedRelaxAt: point.Thresholds.UserRequestedRelaxAt,
		ApprovalRelaxAt:      point.Thresholds.ApprovalRelaxAt,
		FromUntrustedBlockAt: point.Thresholds.FromUntrustedBlockAt,
	}
}

func gateScratch(t *testing.T, lockBuild string) (string, policy.Thresholds, policy.Thresholds) {
	t.Helper()
	dir := chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("gate"))
	declared := writeGatePolicyFixture(t)
	writeGateLedgerRowFixture(t)
	pinned := writeGateLockFixture(t, lockBuild)
	return dir, declared, pinned
}

func TestTheGateReadsThePolicyInTheBinaryWhenTheProjectHasNoCatalog(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("gate"))
	shipped, err := policy.LoadFS(catalogpolicy.Files(), runGatePoint+".yaml")
	if err != nil {
		t.Fatalf("policy.LoadFS: %v", err)
	}
	if got := gateThresholds(t, dir); got != shipped.Thresholds {
		t.Fatalf("the gate decides at %+v, want the binary's %+v", got, shipped.Thresholds)
	}
}

func TestDoctorAndTheGateReportThePinnedThresholds(t *testing.T) {
	dir, _, pinned := gateScratch(t, gateFixtureBuild)
	point := policyPointOf(t, runGatePoint)
	if point.Mode != "enforced" || point.ThresholdsFrom != "the lock" {
		t.Fatalf("doctor reads %+v, want it enforced on the lock", point)
	}
	if read := gateThresholdsAsDoctorReadsThem(point); read != pinned {
		t.Fatalf("doctor reports %+v, want the lock's %+v", read, pinned)
	}
	got := gateThresholds(t, dir)
	if got != pinned {
		t.Fatalf("the gate decides at %+v, want the lock's %+v", got, pinned)
	}
	t.Logf("doctor: %+v", point)
	t.Logf("gate:   %s", got)
}

func TestDoctorAndTheGateAgreeAShadowPointKeepsItsOwnThresholds(t *testing.T) {
	dir, declared, pinned := gateScratch(t, "typesafe/jev-1.12-20260901")
	point := policyPointOf(t, runGatePoint)
	want := "lock build typesafe/jev-1.12-20260901 does not match current build " + gateFixtureBuild
	if point.Mode != "shadow" || point.Fallback != want {
		t.Fatalf("doctor reads %+v, want it fallen back to shadow because %q", point, want)
	}
	if read := gateThresholdsAsDoctorReadsThem(point); read != declared {
		t.Fatalf("doctor reports %+v, want the policy's own %+v", read, declared)
	}
	got := gateThresholds(t, dir)
	if got != declared {
		t.Fatalf("the gate decides at %+v, want the policy's own %+v", got, declared)
	}
	if got == pinned {
		t.Fatalf("a shadow point took the lock's thresholds %+v", pinned)
	}
	t.Logf("doctor: %+v", point)
	t.Logf("gate:   %s", got)
	t.Logf("lock, not applied: %s", pinned)
}
