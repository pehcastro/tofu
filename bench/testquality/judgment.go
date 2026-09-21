package testquality

import "tofu/internal/rule"

func judgmentFlags(dir, checkerID string) ([]rule.Finding, error) {
	checker := rule.Builtins()[checkerID]
	return checker.Check(rule.Rule{ID: checkerID, Checker: checkerID}, rule.GoPackage{Dir: dir})
}

func JudgmentFlagsTautologicalTests(dir string) ([]rule.Finding, error) {
	return judgmentFlags(dir, "test_assertion")
}

func JudgmentFlagsMockBoundary(dir string) ([]rule.Finding, error) {
	return judgmentFlags(dir, "test_mock_boundary")
}

func JudgmentFlagsMissingBoundaryCases(dir string) ([]rule.Finding, error) {
	return judgmentFlags(dir, "test_boundary_cases")
}
