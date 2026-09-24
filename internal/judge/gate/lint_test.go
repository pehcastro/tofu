package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/question"
)

func toolGateQuestionsPath() string {
	return filepath.Join("..", "..", "..", "library", "questions", "tool_gate@1.yaml")
}

func mustLoadToolGateQuestions(t *testing.T) question.Set {
	t.Helper()
	set, err := question.Load(toolGateQuestionsPath())
	if err != nil {
		t.Fatalf("question.Load: %v", err)
	}
	return set
}

func hasFinding(findings []Finding, check Check, field string) bool {
	for _, f := range findings {
		if f.Check == check && (field == "" || f.Field == field) {
			return true
		}
	}
	return false
}

func TestLintFileOnTheShippedRule(t *testing.T) {
	r, findings, err := LintFile(toolGateRulePath(), filepath.Join("..", "..", "..", "library"))
	if err != nil {
		t.Fatalf("LintFile: %v", err)
	}
	for _, f := range findings {
		t.Errorf("finding: %s", f)
	}
	if r.Name != "tool_gate" {
		t.Fatalf("r.Name = %q", r.Name)
	}
}

func TestAFindingWithNoFieldNamesTheRuleAlone(t *testing.T) {
	finding := Finding{
		Check:  CheckQuestionMissing,
		Rule:   "tool_gate",
		Field:  "risk_question",
		File:   "library/general/rules/tool_gate@1.yaml",
		Detail: "no such question",
	}
	want := "library/general/rules/tool_gate@1.yaml: tool_gate.risk_question: question-not-in-the-set: no such question"
	if got := finding.String(); got != want {
		t.Fatalf("a finding on a field reads %q, want %q", got, want)
	}
	finding.Field = ""
	want = "library/general/rules/tool_gate@1.yaml: tool_gate: question-not-in-the-set: no such question"
	if got := finding.String(); got != want {
		t.Fatalf("a finding on no field reads %q, want %q", got, want)
	}
}

func TestLintFileReportsAQuestionSetItCannotRead(t *testing.T) {
	dir := t.TempDir()
	ruleDir := filepath.Join(dir, "general", "rules")
	if err := os.MkdirAll(ruleDir, 0o750); err != nil {
		t.Fatalf("making %s: %v", ruleDir, err)
	}
	path := filepath.Join(ruleDir, "tool_gate@1.yaml")
	writeFile(t, path, strings.Replace(projectToolGateRule, "questions_version: 1", "questions_version: 99", 1))
	r, findings, err := LintFile(path, dir)
	if err == nil {
		t.Fatalf("LintFile linted %s against a question set that is not on disk: %v", r.Name, findings)
	}
	if findings != nil {
		t.Fatalf("LintFile returned findings it could not have computed: %v", findings)
	}
}

func TestLintCatchesAQuestionThatDoesNotExist(t *testing.T) {
	set := mustLoadToolGateQuestions(t)
	r := fixtureRule()
	r.RiskQuestion = "not_a_real_question"
	findings := Lint(r, set)
	if !hasFinding(findings, CheckQuestionMissing, "risk_question") {
		t.Fatalf("Lint did not flag the missing question: %v", findings)
	}
}

func TestLintCatchesAQuestionOfTheWrongKind(t *testing.T) {
	set := mustLoadToolGateQuestions(t)
	r := fixtureRule()
	r.RiskQuestion = "approval"
	findings := Lint(r, set)
	if !hasFinding(findings, CheckQuestionWrongKind, "risk_question") {
		t.Fatalf("Lint did not flag the wrong kind: %v", findings)
	}
}

func TestLintCatchesAVersionMismatch(t *testing.T) {
	set := mustLoadToolGateQuestions(t)
	r := fixtureRule()
	r.QuestionsVersion = 99
	findings := Lint(r, set)
	if !hasFinding(findings, CheckVersionMismatch, "") {
		t.Fatalf("Lint did not flag the version mismatch: %v", findings)
	}
}

func fixtureRule() Rule {
	return Rule{
		Name:                  "tool_gate",
		RuleVersion:           1,
		Questions:             "tool_gate",
		QuestionsVersion:      1,
		RiskQuestion:          "risk",
		ApprovalQuestion:      "approval",
		UserRequestedQuestion: "user_requested",
		FromUntrustedQuestion: "from_untrusted",
		Thresholds: Thresholds{
			RiskAskAt:            1.5,
			RiskDenyAt:           2.5,
			UserRequestedRelaxAt: 0.85,
			ApprovalRelaxAt:      0.15,
			FromUntrustedBlockAt: 0.5,
		},
	}
}
