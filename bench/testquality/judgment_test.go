package testquality

import "testing"

func TestJudgmentFlagsMockBoundaryOnTheReassignedFunctionVariable(t *testing.T) {
	findings, err := JudgmentFlagsMockBoundary("testdata/fixture")
	if err != nil {
		t.Fatalf("JudgmentFlagsMockBoundary: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1 for the Save reassignment", len(findings))
	}
}

func TestJudgmentFlagsAFileWhereNoTestPassesABoundaryValue(t *testing.T) {
	findings, err := JudgmentFlagsMissingBoundaryCases("testdata/fixture")
	if err != nil {
		t.Fatalf("JudgmentFlagsMissingBoundaryCases: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1 for the file with no nil, zero, empty or limit argument", len(findings))
	}
}
