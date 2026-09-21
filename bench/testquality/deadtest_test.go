package testquality

import "testing"

func TestCountDeadTestsSumsAllThreeRulesWithoutDeduplication(t *testing.T) {
	count, err := CountDeadTests("testdata/fixture")
	if err != nil {
		t.Fatalf("CountDeadTests: %v", err)
	}
	if count.Tautological != 1 {
		t.Fatalf("Tautological = %d, want 1", count.Tautological)
	}
	if count.MockBoundary != 1 {
		t.Fatalf("MockBoundary = %d, want 1", count.MockBoundary)
	}
	if count.NoBoundaryCoverage != 1 {
		t.Fatalf("NoBoundaryCoverage = %d, want 1", count.NoBoundaryCoverage)
	}
	if count.Total != 3 {
		t.Fatalf("Total = %d, want 3", count.Total)
	}
}

func TestCountDeadTestsOnARequireNotNilFixture(t *testing.T) {
	count, err := CountDeadTests("testdata/existential")
	if err != nil {
		t.Fatalf("CountDeadTests: %v", err)
	}
	if count.Tautological != 1 {
		t.Fatalf("Tautological = %d, want 1, because require.NotNil is an existential claim", count.Tautological)
	}
	if count.NoBoundaryCoverage != 1 {
		t.Fatalf("NoBoundaryCoverage = %d, want 1, because the file passes no nil, zero, empty or limit argument", count.NoBoundaryCoverage)
	}
	if count.Total != 2 {
		t.Fatalf("Total = %d, want 2", count.Total)
	}
}
