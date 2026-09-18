package policy

import (
	"path/filepath"
	"testing"

	"boji/internal/judge/question"
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
