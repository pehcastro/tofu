package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/question"
)

func toolGateQuestionsPath() string {
	return filepath.Join("..", "..", "..", "catalog", "questions", "tool_gate@1.yaml")
}

func mustLoadToolGateQuestions(t *testing.T) question.Set {
	t.Helper()
	set, err := question.Load(toolGateQuestionsPath())
	if err != nil {
		t.Fatalf("question.Load: %v", err)
	}
	return set
}

func hasFinding(findings []Finding, rule Rule, field string) bool {
	for _, f := range findings {
		if f.Rule == rule && (field == "" || f.Field == field) {
			return true
		}
	}
	return false
}

func TestLintFileOnTheShippedPolicy(t *testing.T) {
	pol, findings, err := LintFile(toolGatePolicyPath())
	if err != nil {
		t.Fatalf("LintFile: %v", err)
	}
	for _, f := range findings {
		t.Errorf("finding: %s", f)
	}
	if pol.Name != "tool_gate" {
		t.Fatalf("pol.Name = %q", pol.Name)
	}
}

func TestAFindingWithNoFieldNamesThePolicyAlone(t *testing.T) {
	finding := Finding{
		Rule:   RuleQuestionMissing,
		Policy: "tool_gate",
		Field:  "risk_question",
		File:   "catalog/policy/tool_gate@1.yaml",
		Detail: "no such question",
	}
	want := "catalog/policy/tool_gate@1.yaml: tool_gate.risk_question: question-not-in-the-set: no such question"
	if got := finding.String(); got != want {
		t.Fatalf("a finding on a field reads %q, want %q", got, want)
	}
	finding.Field = ""
	want = "catalog/policy/tool_gate@1.yaml: tool_gate: question-not-in-the-set: no such question"
	if got := finding.String(); got != want {
		t.Fatalf("a finding on no field reads %q, want %q", got, want)
	}
}

func TestLintFileReportsAQuestionSetItCannotRead(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "policy")
	if err := os.MkdirAll(policyDir, 0o750); err != nil {
		t.Fatalf("making %s: %v", policyDir, err)
	}
	path := filepath.Join(policyDir, "tool_gate@1.yaml")
	writeFile(t, path, strings.Replace(projectToolGatePolicy, "questions_version: 1", "questions_version: 99", 1))
	pol, findings, err := LintFile(path)
	if err == nil {
		t.Fatalf("LintFile linted %s against a question set that is not on disk: %v", pol.Name, findings)
	}
	if findings != nil {
		t.Fatalf("LintFile returned findings it could not have computed: %v", findings)
	}
}

func TestLintCatchesAQuestionThatDoesNotExist(t *testing.T) {
	set := mustLoadToolGateQuestions(t)
	pol := fixturePolicy()
	pol.RiskQuestion = "not_a_real_question"
	findings := Lint(pol, set)
	if !hasFinding(findings, RuleQuestionMissing, "risk_question") {
		t.Fatalf("Lint did not flag the missing question: %v", findings)
	}
}

func TestLintCatchesAQuestionOfTheWrongKind(t *testing.T) {
	set := mustLoadToolGateQuestions(t)
	pol := fixturePolicy()
	pol.RiskQuestion = "approval"
	findings := Lint(pol, set)
	if !hasFinding(findings, RuleQuestionWrongKind, "risk_question") {
		t.Fatalf("Lint did not flag the wrong kind: %v", findings)
	}
}

func TestLintCatchesAVersionMismatch(t *testing.T) {
	set := mustLoadToolGateQuestions(t)
	pol := fixturePolicy()
	pol.QuestionsVersion = 99
	findings := Lint(pol, set)
	if !hasFinding(findings, RuleVersionMismatch, "") {
		t.Fatalf("Lint did not flag the version mismatch: %v", findings)
	}
}
