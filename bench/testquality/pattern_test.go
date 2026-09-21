package testquality

import "testing"

func TestPatternMissesAnExistentialComparisonTheJudgmentCatches(t *testing.T) {
	patternFindings, err := PatternFlagsTautologicalTests("testdata/fixture")
	if err != nil {
		t.Fatalf("PatternFlagsTautologicalTests: %v", err)
	}
	if len(patternFindings) != 0 {
		t.Fatalf("pattern flagged %d tests in a fixture where every test contains a comparison operator or an error check, want 0", len(patternFindings))
	}
	judgmentFindings, err := JudgmentFlagsTautologicalTests("testdata/fixture")
	if err != nil {
		t.Fatalf("JudgmentFlagsTautologicalTests: %v", err)
	}
	if len(judgmentFindings) != 1 {
		t.Fatalf("judgment flagged %d tests, want 1 for TestAddReturnsSomething, whose only claim is result == 0", len(judgmentFindings))
	}
}

func TestPatternFlagsATestWithNoComparisonAndNoErrorCheck(t *testing.T) {
	findings, err := PatternFlagsTautologicalTests("testdata/existential")
	if err != nil {
		t.Fatalf("PatternFlagsTautologicalTests: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	if findings[0].Function != "TestExists" {
		t.Fatalf("findings[0].Function = %q, want TestExists", findings[0].Function)
	}
}
