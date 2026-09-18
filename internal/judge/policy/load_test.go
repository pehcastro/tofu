package policy

import (
	"path/filepath"
	"testing"
)

func toolGatePolicyPath() string {
	return filepath.Join("..", "..", "..", "catalog", "policy", "tool_gate@1.yaml")
}

func TestLoadTheShippedPolicy(t *testing.T) {
	pol, err := Load(toolGatePolicyPath())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pol.Name != "tool_gate" || pol.PolicyVersion != 1 {
		t.Fatalf("name = %q version = %d", pol.Name, pol.PolicyVersion)
	}
	if pol.Questions != "tool_gate" || pol.QuestionsVersion != 1 {
		t.Fatalf("questions = %q questions_version = %d", pol.Questions, pol.QuestionsVersion)
	}
	if pol.Thresholds.RiskAskAt != 1.5 || pol.Thresholds.RiskDenyAt != 2.5 {
		t.Fatalf("thresholds = %+v", pol.Thresholds)
	}
	if pol.Thresholds.UserRequestedRelaxAt != 0.85 {
		t.Fatalf("user_requested_relax_at = %v", pol.Thresholds.UserRequestedRelaxAt)
	}
}

func TestLoadRejectsAnUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad@1.yaml")
	writeFile(t, path, "name: bad\nnot_a_field: 1\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unknown field")
	}
}

func TestLoadDefaultsToShadowWhenModeIsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noMode@1.yaml")
	writeFile(t, path, "name: no_mode\npolicy_version: 1\n")
	pol, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pol.Mode != ModeShadow {
		t.Fatalf("mode = %s, want shadow", pol.Mode)
	}
	if pol.ModeDeclared {
		t.Fatalf("ModeDeclared = true, want false, mode was never written")
	}
}

func TestLoadRejectsAnUnknownMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "badMode@1.yaml")
	writeFile(t, path, "name: bad_mode\npolicy_version: 1\nmode: sometimes\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unknown mode value")
	}
}

func TestLoadReadsSampleFloorAndDeclaredMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "declared@1.yaml")
	writeFile(t, path, "name: declared\npolicy_version: 1\nmode: enforced\nsample_floor: 300\n")
	pol, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pol.Mode != ModeEnforced || !pol.ModeDeclared {
		t.Fatalf("mode = %s declared = %v", pol.Mode, pol.ModeDeclared)
	}
	if pol.SampleFloor != 300 {
		t.Fatalf("sample_floor = %d, want 300", pol.SampleFloor)
	}
}
