package testquality

const DeadTestCountingRule = "a dead test is any occurrence of: a whole test function whose only claims are that a value is nil, zero, empty or of a type (test_assertion); a line inside a test that mocks an internal package function or asserts which internal call happened rather than what the code produced (test_mock_boundary); or a whole test file whose tests pass no nil, zero, empty or limit value anywhere (test_boundary_cases, counted once per file). Occurrences are summed across the three rules without deduplication, so one test flagged by two rules counts twice."

type DeadTestCount struct {
	Tautological       int
	MockBoundary       int
	NoBoundaryCoverage int
	Total              int
}

func CountDeadTests(dir string) (DeadTestCount, error) {
	tautological, err := JudgmentFlagsTautologicalTests(dir)
	if err != nil {
		return DeadTestCount{}, err
	}
	mockBoundary, err := JudgmentFlagsMockBoundary(dir)
	if err != nil {
		return DeadTestCount{}, err
	}
	noBoundary, err := JudgmentFlagsMissingBoundaryCases(dir)
	if err != nil {
		return DeadTestCount{}, err
	}
	count := DeadTestCount{
		Tautological:       len(tautological),
		MockBoundary:       len(mockBoundary),
		NoBoundaryCoverage: len(noBoundary),
	}
	count.Total = count.Tautological + count.MockBoundary + count.NoBoundaryCoverage
	return count, nil
}
