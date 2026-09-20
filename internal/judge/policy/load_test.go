package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	catalogpolicy "tofu/catalog/policy"
	"tofu/internal/judge/question"
)

func toolGatePolicyPath() string {
	return filepath.Join("..", "..", "..", "catalog", "policy", "tool_gate@1.yaml")
}

const projectToolGatePolicy = `name: tool_gate
policy_version: 1
questions: tool_gate
questions_version: 1
mode: shadow
risk_question: risk
approval_question: approval
user_requested_question: user_requested
from_untrusted_question: from_untrusted

thresholds:
  risk_ask_at: 0.25
`

func shippedQuestionSet(t *testing.T) question.Set {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "catalog", "questions", "tool_gate@1.yaml"))
	if err != nil {
		t.Fatalf("absolute question path: %v", err)
	}
	set, err := question.Load(path)
	if err != nil {
		t.Fatalf("question.Load: %v", err)
	}
	return set
}

func projectPolicyDir(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "catalog", "policy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making %s: %v", dir, err)
	}
	return root, filepath.Join(dir, "tool_gate@1.yaml")
}

func TestLoadPointReadsTheEmbeddedPolicyWhenTheProjectHasNoCatalog(t *testing.T) {
	set := shippedQuestionSet(t)
	t.Chdir(t.TempDir())
	pol, origin, err := LoadPoint(catalogpolicy.Files(), "tool_gate@1", set)
	if err != nil {
		t.Fatalf("LoadPoint: %v", err)
	}
	if origin != OriginBinary {
		t.Fatalf("origin = %q, want %q", origin, OriginBinary)
	}
	if pol.Name != "tool_gate" || pol.Thresholds.RiskAskAt != 1.5 {
		t.Fatalf("name = %q risk_ask_at = %v, want tool_gate and 1.5", pol.Name, pol.Thresholds.RiskAskAt)
	}
	if pol.File != "catalog/policy/tool_gate@1.yaml" {
		t.Fatalf("file = %q, want the embedded name", pol.File)
	}
}

func TestLoadPointPrefersTheProjectPolicyOverTheEmbeddedOne(t *testing.T) {
	set := shippedQuestionSet(t)
	root, path := projectPolicyDir(t)
	writeFile(t, path, projectToolGatePolicy)
	t.Chdir(root)
	pol, origin, err := LoadPoint(catalogpolicy.Files(), "tool_gate@1", set)
	if err != nil {
		t.Fatalf("LoadPoint: %v", err)
	}
	if origin != OriginProject {
		t.Fatalf("origin = %q, want %q", origin, OriginProject)
	}
	if pol.Thresholds.RiskAskAt != 0.25 {
		t.Fatalf("risk_ask_at = %v, want 0.25 from the project policy", pol.Thresholds.RiskAskAt)
	}
	if pol.File != path {
		t.Fatalf("file = %q, want %q", pol.File, path)
	}
}

func TestLoadPointFailsWhenTheProjectPolicyIsUnreadable(t *testing.T) {
	set := shippedQuestionSet(t)
	root, path := projectPolicyDir(t)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("making %s a directory: %v", path, err)
	}
	t.Chdir(root)
	pol, origin, err := LoadPoint(catalogpolicy.Files(), "tool_gate@1", set)
	if err == nil {
		t.Fatalf("LoadPoint fell back to %s with an unreadable project policy: %+v", origin, pol)
	}
	if !strings.Contains(err.Error(), "the project") {
		t.Fatalf("the error does not say the project policy failed: %v", err)
	}
}

func TestLoadPointFailsWhenTheProjectPolicyDoesNotMatchTheQuestions(t *testing.T) {
	set := shippedQuestionSet(t)
	root, path := projectPolicyDir(t)
	writeFile(t, path, strings.Replace(projectToolGatePolicy, "risk_question: risk", "risk_question: danger", 1))
	t.Chdir(root)
	if _, _, err := LoadPoint(catalogpolicy.Files(), "tool_gate@1", set); err == nil {
		t.Fatal("LoadPoint accepted a project policy naming a question the set does not have")
	}
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
