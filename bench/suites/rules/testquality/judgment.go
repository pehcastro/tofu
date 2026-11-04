package testquality

import "tofu/internal/librarian/rules"

func judgmentFlags(dir, checkerID string) ([]rules.Finding, error) {
	checker := rules.Builtins()[checkerID]
	return checker.Check(rules.Rule{ID: checkerID, Checker: checkerID}, rules.GoPackage{Dir: dir})
}

func JudgmentFlagsTautologicalTests(dir string) ([]rules.Finding, error) {
	return judgmentFlags(dir, "test_assertion")
}

func JudgmentFlagsMockBoundary(dir string) ([]rules.Finding, error) {
	return judgmentFlags(dir, "test_mock_boundary")
}

func JudgmentFlagsMissingBoundaryCases(dir string) ([]rules.Finding, error) {
	return judgmentFlags(dir, "test_boundary_cases")
}
